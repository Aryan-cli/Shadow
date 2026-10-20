package payment

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// ── Key Generation ───────────────────────────────────────────────────────────

func GenerateBTCAddress() (address, wif string, err error) {
	privKey, err := btcec.NewPrivateKey()
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}
	pubKey := privKey.PubKey()
	address, err = pubKeyToP2PKH(pubKey.SerializeCompressed())
	if err != nil {
		return "", "", fmt.Errorf("derive address: %w", err)
	}
	wif = encodeWIF(privKey.Serialize())
	return address, wif, nil
}

func pubKeyToP2PKH(pubKeyBytes []byte) (string, error) {
	h := sha256.Sum256(pubKeyBytes)
	ripe := ripemd160Hash(h[:])
	payload := append([]byte{0x00}, ripe...) // mainnet version byte
	checksum := doubleSHA256(payload)[:4]
	full := append(payload, checksum...)
	return base58Encode(full), nil
}

func encodeWIF(privKeyBytes []byte) string {
	payload := append([]byte{0x80}, privKeyBytes...) // mainnet
	payload = append(payload, 0x01)                  // compressed flag
	checksum := doubleSHA256(payload)[:4]
	full := append(payload, checksum...)
	return base58Encode(full)
}

func decodeWIF(wifStr string) ([]byte, error) {
	decoded := base58Decode(wifStr)
	if len(decoded) < 37 {
		return nil, fmt.Errorf("invalid WIF length")
	}
	payload := decoded[:len(decoded)-4]
	checksum := decoded[len(decoded)-4:]
	computed := doubleSHA256(payload)[:4]
	if !bytes.Equal(checksum, computed) {
		return nil, fmt.Errorf("invalid WIF checksum")
	}
	raw := payload[1:] // strip version byte
	if len(raw) == 33 && raw[32] == 0x01 {
		raw = raw[:32]
	}
	return raw, nil
}

// ── BTC Price ────────────────────────────────────────────────────────────────

type coinGeckoResp struct {
	Bitcoin struct {
		USD float64 `json:"usd"`
	} `json:"bitcoin"`
}

func GetBTCPrice() (float64, error) {
	resp, err := httpClient.Get("https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var data coinGeckoResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	if data.Bitcoin.USD == 0 {
		return 0, fmt.Errorf("zero price returned")
	}
	return data.Bitcoin.USD, nil
}

func USDToBTC(usd, btcPriceUSD float64) float64 {
	if btcPriceUSD == 0 {
		return 0
	}
	btc := usd / btcPriceUSD
	factor := 1e8
	return float64(int64(btc*factor+0.5)) / factor
}

func SatoshisToBTC(sat int64) float64 { return float64(sat) / 1e8 }
func BTCToSatoshis(btc float64) int64 { return int64(btc * 1e8) }

// ── Balance Check ────────────────────────────────────────────────────────────

type bcAddrResp struct {
	TotalReceived int64 `json:"total_received"`
	FinalBalance  int64 `json:"final_balance"`
}

func GetAddressReceivedSatoshis(address string) (int64, error) {
	url := "https://api.blockcypher.com/v1/btc/main/addrs/" + address + "/balance"
	resp, err := httpClient.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return 0, nil
	}
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("blockcypher status %d", resp.StatusCode)
	}
	var data bcAddrResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	total := data.TotalReceived
	if data.FinalBalance > total {
		total = data.FinalBalance
	}
	return total, nil
}

// ── Transaction Sending ──────────────────────────────────────────────────────

func SendAllBTC(wifKey, fromAddress, destAddress string) (string, error) {
	if destAddress == "" {
		return "", fmt.Errorf("destination address is empty")
	}

	balance, err := GetAddressReceivedSatoshis(fromAddress)
	if err != nil {
		return "", fmt.Errorf("check balance: %w", err)
	}
	if balance <= 0 {
		return "", nil
	}

	privKeyBytes, err := decodeWIF(wifKey)
	if err != nil {
		return "", fmt.Errorf("decode wif: %w", err)
	}
	privKey, pubKey := btcec.PrivKeyFromBytes(privKeyBytes)
	pubKeyHex := hex.EncodeToString(pubKey.SerializeCompressed())

	// Step 1: Create transaction skeleton (value=0 means send max/sweep)
	reqBody := map[string]interface{}{
		"inputs":  []map[string]interface{}{{"addresses": []string{fromAddress}}},
		"outputs": []map[string]interface{}{{"addresses": []string{destAddress}, "value": 0}},
	}
	reqJSON, _ := json.Marshal(reqBody)

	resp, err := httpClient.Post(
		"https://api.blockcypher.com/v1/btc/main/txs/new?includeToSignTx=true",
		"application/json",
		bytes.NewReader(reqJSON),
	)
	if err != nil {
		return "", fmt.Errorf("new tx request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var newTxResp struct {
		Tx     json.RawMessage `json:"tx"`
		ToSign []string        `json:"tosign"`
		Errors []struct {
			Error string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &newTxResp); err != nil {
		return "", fmt.Errorf("parse new tx: %w", err)
	}
	if len(newTxResp.Errors) > 0 {
		return "", fmt.Errorf("blockcypher: %s", newTxResp.Errors[0].Error)
	}
	if len(newTxResp.ToSign) == 0 {
		return "", fmt.Errorf("no tosign data")
	}

	// Step 2: Sign each hash
	signatures := make([]string, len(newTxResp.ToSign))
	pubkeys := make([]string, len(newTxResp.ToSign))
	for i, tsHex := range newTxResp.ToSign {
		hashBytes, err := hex.DecodeString(tsHex)
		if err != nil {
			return "", fmt.Errorf("decode tosign[%d]: %w", i, err)
		}
		sig := ecdsa.Sign(privKey, hashBytes)
		signatures[i] = hex.EncodeToString(sig.Serialize())
		pubkeys[i] = pubKeyHex
	}

	// Step 3: Broadcast
	sendReq := map[string]interface{}{
		"tx":         newTxResp.Tx,
		"tosign":     newTxResp.ToSign,
		"signatures": signatures,
		"pubkeys":    pubkeys,
	}
	sendJSON, _ := json.Marshal(sendReq)

	sendResp, err := httpClient.Post(
		"https://api.blockcypher.com/v1/btc/main/txs/send",
		"application/json",
		bytes.NewReader(sendJSON),
	)
	if err != nil {
		return "", fmt.Errorf("send tx: %w", err)
	}
	defer sendResp.Body.Close()
	sendBody, _ := io.ReadAll(sendResp.Body)

	var result struct {
		Tx struct {
			Hash string `json:"hash"`
		} `json:"tx"`
		Errors []struct {
			Error string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(sendBody, &result); err != nil {
		return "", fmt.Errorf("parse send resp: %w", err)
	}
	if len(result.Errors) > 0 {
		return "", fmt.Errorf("blockcypher send: %s", result.Errors[0].Error)
	}
	return result.Tx.Hash, nil
}

// ── Crypto helpers ───────────────────────────────────────────────────────────

func doubleSHA256(data []byte) []byte {
	h1 := sha256.Sum256(data)
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

func ripemd160Hash(data []byte) []byte { return ripemd160Compute(data) }

// ── Base58 ───────────────────────────────────────────────────────────────────

const b58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base58Encode(input []byte) string {
	n := new(big.Int).SetBytes(input)
	base := big.NewInt(58)
	mod := new(big.Int)
	var result []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		result = append(result, b58Alphabet[mod.Int64()])
	}
	for _, b := range input {
		if b != 0x00 {
			break
		}
		result = append(result, b58Alphabet[0])
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

func base58Decode(input string) []byte {
	n := big.NewInt(0)
	base := big.NewInt(58)
	for _, c := range input {
		idx := strings.IndexRune(b58Alphabet, c)
		if idx < 0 {
			return nil
		}
		n.Mul(n, base)
		n.Add(n, big.NewInt(int64(idx)))
	}
	decoded := n.Bytes()
	leadingZeros := 0
	for _, c := range input {
		if c == rune(b58Alphabet[0]) {
			leadingZeros++
		} else {
			break
		}
	}
	result := make([]byte, leadingZeros+len(decoded))
	copy(result[leadingZeros:], decoded)
	return result
}

// ── RIPEMD-160 (pure Go) ─────────────────────────────────────────────────────

func ripemd160Compute(msg []byte) []byte {
	h0, h1, h2, h3, h4 := uint32(0x67452301), uint32(0xEFCDAB89), uint32(0x98BADCFE), uint32(0x10325476), uint32(0xC3D2E1F0)

	msgLen := len(msg)
	bitLen := uint64(msgLen) * 8
	msg = append(msg, 0x80)
	for len(msg)%64 != 56 {
		msg = append(msg, 0x00)
	}
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, bitLen)
	msg = append(msg, buf...)

	rl := [80]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 7, 4, 13, 1, 10, 6, 15, 3, 12, 0, 9, 5, 2, 14, 11, 8, 3, 10, 14, 4, 9, 15, 8, 1, 2, 7, 0, 6, 13, 11, 5, 12, 1, 9, 11, 10, 0, 8, 12, 4, 13, 3, 7, 15, 14, 5, 6, 2, 4, 0, 5, 9, 7, 12, 2, 10, 14, 1, 3, 8, 11, 6, 15, 13}
	rr := [80]int{5, 14, 7, 0, 9, 2, 11, 4, 13, 6, 15, 8, 1, 10, 3, 12, 6, 11, 3, 7, 0, 13, 5, 10, 14, 15, 8, 12, 4, 9, 1, 2, 15, 5, 1, 3, 7, 14, 6, 9, 11, 8, 12, 2, 10, 0, 4, 13, 8, 6, 4, 1, 3, 11, 15, 0, 5, 12, 2, 13, 9, 7, 10, 14, 12, 15, 10, 4, 1, 5, 8, 7, 6, 2, 13, 14, 0, 3, 9, 11}
	sl := [80]uint{11, 14, 15, 12, 5, 8, 7, 9, 11, 13, 14, 15, 6, 7, 9, 8, 7, 6, 8, 13, 11, 9, 7, 15, 7, 12, 15, 9, 11, 7, 13, 12, 11, 13, 6, 7, 14, 9, 13, 15, 14, 8, 13, 6, 5, 12, 7, 5, 11, 12, 14, 15, 14, 15, 9, 8, 9, 14, 5, 6, 8, 6, 5, 12, 9, 15, 5, 11, 6, 8, 13, 12, 5, 12, 13, 14, 11, 8, 5, 6}
	sr := [80]uint{8, 9, 9, 11, 13, 15, 15, 5, 7, 7, 8, 11, 14, 14, 12, 6, 9, 13, 15, 7, 12, 8, 9, 11, 7, 7, 12, 7, 6, 15, 13, 11, 9, 7, 15, 11, 8, 6, 6, 14, 12, 13, 5, 14, 13, 13, 7, 5, 15, 5, 8, 11, 14, 14, 6, 14, 6, 9, 12, 9, 12, 5, 15, 8, 8, 5, 12, 9, 12, 5, 14, 6, 8, 13, 6, 5, 15, 13, 11, 11}
	kl := [5]uint32{0x00000000, 0x5A827999, 0x6ED9EBA1, 0x8F1BBCDC, 0xA953FD4E}
	kr := [5]uint32{0x50A28BE6, 0x5C4DD124, 0x6D703EF3, 0x7A6D76E9, 0x00000000}

	rol := func(x uint32, n uint) uint32 { return (x << n) | (x >> (32 - n)) }

	for i := 0; i < len(msg); i += 64 {
		var X [16]uint32
		for j := 0; j < 16; j++ {
			X[j] = binary.LittleEndian.Uint32(msg[i+j*4:])
		}
		al, bl, cl, dl, el := h0, h1, h2, h3, h4
		ar, br, cr, dr, er := h0, h1, h2, h3, h4

		for j := 0; j < 80; j++ {
			round := j / 16
			var fl, fr uint32
			switch round {
			case 0:
				fl = bl ^ cl ^ dl
				fr = (br & dr) | (^dr & cr)
			case 1:
				fl = (bl & cl) | (^bl & dl)
				fr = (br | ^cr) ^ dr
			case 2:
				fl = (bl | ^cl) ^ dl
				fr = (br & cr) | (^br & dr)
			case 3:
				fl = (bl & dl) | (cl & ^dl)
				fr = br | (^dr & cr)
			case 4:
				fl = bl ^ (cl | ^dl)
				fr = br ^ cr ^ dr
			}
			T := rol(al+fl+X[rl[j]]+kl[round], sl[j]) + el
			al, el, dl, cl, bl = el, dl, rol(cl, 10), bl, T
			T = rol(ar+fr+X[rr[j]]+kr[round], sr[j]) + er
			ar, er, dr, cr, br = er, dr, rol(cr, 10), br, T
		}
		T := h1 + cl + dr
		h1 = h2 + dl + er
		h2 = h3 + el + ar
		h3 = h4 + al + br
		h4 = h0 + bl + cr
		h0 = T
	}

	out := make([]byte, 20)
	binary.LittleEndian.PutUint32(out[0:], h0)
	binary.LittleEndian.PutUint32(out[4:], h1)
	binary.LittleEndian.PutUint32(out[8:], h2)
	binary.LittleEndian.PutUint32(out[12:], h3)
	binary.LittleEndian.PutUint32(out[16:], h4)
	return out
}
