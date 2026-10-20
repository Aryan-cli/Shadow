package payment

// Monero subaddress derivation using private spend key + view key (hex).
//
// No seed phrase wordlist needed. Keys are provided directly as 64-char hex strings:
//   XMR_SPEND_KEY  = private spend key  (32 bytes hex, from Feather: Wallet → Keys)
//   XMR_VIEW_KEY   = private view key   (32 bytes hex, optional — derived if absent)
//
// Derivation:
//   b   = private spend key scalar
//   a   = private view key scalar  (= sc_reduce32(Keccak256(b)) if not provided)
//   B   = b*G  (spend public key)
//   A   = a*G  (view public key)
//   subaddress(i):
//     m = Hs("SubAddr\x00" || a_bytes || LE32(account=0) || LE32(i))
//     D = B + m*G
//     C = a*D
//     encoded as Monero base58 with prefix 0x2A
//
// Payment detection via MyMonero-compatible light wallet server (openmonero.co by default).
// Override with XMR_LIGHT_SERVER env var.

import (
        "encoding/binary"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "log"
        "math/big"
        "net/http"
        "os"
        "strings"
        "time"

        "filippo.io/edwards25519"
        "golang.org/x/crypto/sha3"
)

// ── Env helpers ───────────────────────────────────────────────────────────────

func GetXMRSpendKey() string { return strings.TrimSpace(os.Getenv("XMR_SPEND_KEY")) }
func GetXMRViewKey() string  { return strings.TrimSpace(os.Getenv("XMR_VIEW_KEY")) }
func XMREnabled() bool       { return GetXMRSpendKey() != "" }

// XMRSeedEnabled / XMRRPCEnabled kept for compatibility with existing handler code.
func XMRSeedEnabled() bool { return XMREnabled() }
func XMRRPCEnabled() bool  { return XMREnabled() }

// GetXMRSeed is kept for compatibility; returns spend key env string.
func GetXMRSeed() string { return GetXMRSpendKey() }

var xmrHTTPClient = &http.Client{Timeout: 20 * time.Second}

// ── Monero group order l ──────────────────────────────────────────────────────

// l = 2^252 + 27742317777372353535851937790883648493
var xmrL, _ = new(big.Int).SetString(
        "7237005577332262213973186563042994240857116359379907606001950938285454250989", 10)

// scReduce32 reduces a 32-byte little-endian integer mod l, returns a Scalar.
func scReduce32(b []byte) (*edwards25519.Scalar, error) {
        if len(b) != 32 {
                return nil, fmt.Errorf("scReduce32: need 32 bytes, got %d", len(b))
        }
        // Convert LE → big.Int, reduce mod l, convert back to LE
        rev := make([]byte, 32)
        for i, v := range b {
                rev[31-i] = v
        }
        n := new(big.Int).SetBytes(rev)
        n.Mod(n, xmrL)
        be := n.FillBytes(make([]byte, 32))
        le := make([]byte, 32)
        for i, v := range be {
                le[31-i] = v
        }
        return new(edwards25519.Scalar).SetCanonicalBytes(le)
}

// ── Keccak-256 (Monero hash H) ────────────────────────────────────────────────

func xmrKeccak256(data []byte) []byte {
        h := sha3.NewLegacyKeccak256()
        h.Write(data)
        return h.Sum(nil)
}

// Hs(x) = sc_reduce32(Keccak256(x))
func xmrHs(data []byte) (*edwards25519.Scalar, error) {
        return scReduce32(xmrKeccak256(data))
}

// ── Load keys from environment ────────────────────────────────────────────────

// xmrKeys holds the two private scalars and derived public points.
type xmrKeys struct {
        b          *edwards25519.Scalar // private spend key
        a          *edwards25519.Scalar // private view key
        aBytes     []byte               // a as canonical LE bytes (used in Hs input)
        B          *edwards25519.Point  // spend public key = b*G
        primaryAddr string              // primary address (index 0)
}

func loadXMRKeys() (*xmrKeys, error) {
        spendHex := GetXMRSpendKey()
        if spendHex == "" {
                return nil, fmt.Errorf("XMR_SPEND_KEY not set")
        }
        if len(spendHex) != 64 {
                return nil, fmt.Errorf("XMR_SPEND_KEY must be 64 hex chars (32 bytes), got %d", len(spendHex))
        }
        bBytes, err := hex.DecodeString(spendHex)
        if err != nil {
                return nil, fmt.Errorf("XMR_SPEND_KEY invalid hex: %w", err)
        }

        // b scalar: spend keys from Monero wallets are already reduced (< l), use SetCanonicalBytes.
        // If not canonical (edge case), fall through to scReduce32.
        b, err := new(edwards25519.Scalar).SetCanonicalBytes(bBytes)
        if err != nil {
                b, err = scReduce32(bBytes)
                if err != nil {
                        return nil, fmt.Errorf("spend key scalar: %w", err)
                }
        }

        var a *edwards25519.Scalar
        var aBytes []byte
        viewHex := GetXMRViewKey()
        if viewHex != "" {
                if len(viewHex) != 64 {
                        return nil, fmt.Errorf("XMR_VIEW_KEY must be 64 hex chars, got %d", len(viewHex))
                }
                vBytes, err := hex.DecodeString(viewHex)
                if err != nil {
                        return nil, fmt.Errorf("XMR_VIEW_KEY invalid hex: %w", err)
                }
                a, err = new(edwards25519.Scalar).SetCanonicalBytes(vBytes)
                if err != nil {
                        a, err = scReduce32(vBytes)
                        if err != nil {
                                return nil, fmt.Errorf("view key scalar: %w", err)
                        }
                }
                aBytes = vBytes
        } else {
                // Derive view key: a = sc_reduce32(Keccak256(b_bytes))
                derived := xmrKeccak256(bBytes)
                a, err = scReduce32(derived)
                if err != nil {
                        return nil, fmt.Errorf("derive view key: %w", err)
                }
                aBytes = a.Bytes()
        }

        B := new(edwards25519.Point).ScalarBaseMult(b)
        A := new(edwards25519.Point).ScalarBaseMult(a)
        primaryAddr := encodeXMRAddress(0x12, B.Bytes(), A.Bytes())

        return &xmrKeys{b: b, a: a, aBytes: aBytes, B: B, primaryAddr: primaryAddr}, nil
}

// ── Subaddress derivation ─────────────────────────────────────────────────────

// DeriveXMRSubaddress derives the Monero subaddress at account=0, index=i.
// index=0 returns the primary address (prefix 0x12 = 18).
func DeriveXMRSubaddress(_, _ string, index uint32) (string, error) {
        return DeriveXMRSubaddressFromEnv(index)
}

// DeriveXMRSubaddressFromEnv derives the subaddress at index using env-loaded keys.
func DeriveXMRSubaddressFromEnv(index uint32) (string, error) {
        keys, err := loadXMRKeys()
        if err != nil {
                return "", err
        }

        if index == 0 {
                return keys.primaryAddr, nil
        }

        // m = Hs("SubAddr\x00" || a_bytes || LE32(account=0) || LE32(index))
        hsInput := make([]byte, 0, 8+32+4+4)
        hsInput = append(hsInput, []byte("SubAddr\x00")...)
        hsInput = append(hsInput, keys.aBytes...)
        accountLE := make([]byte, 4)
        binary.LittleEndian.PutUint32(accountLE, 0)
        hsInput = append(hsInput, accountLE...)
        indexLE := make([]byte, 4)
        binary.LittleEndian.PutUint32(indexLE, index)
        hsInput = append(hsInput, indexLE...)

        m, err := xmrHs(hsInput)
        if err != nil {
                return "", fmt.Errorf("Hs: %w", err)
        }

        // D = B + m*G
        mG := new(edwards25519.Point).ScalarBaseMult(m)
        D := new(edwards25519.Point).Add(keys.B, mG)

        // C = a*D
        C := new(edwards25519.Point).ScalarMult(keys.a, D)

        return encodeXMRAddress(0x2A, D.Bytes(), C.Bytes()), nil
}

// ── Monero address encoding ───────────────────────────────────────────────────

func encodeXMRAddress(prefix byte, spendPub, viewPub []byte) string {
        data := make([]byte, 65)
        data[0] = prefix
        copy(data[1:33], spendPub)
        copy(data[33:65], viewPub)
        csum := xmrKeccak256(data)
        return xmrBase58Encode(append(data, csum[:4]...))
}

// Monero block-based Base58 (8-byte blocks → 11 chars, last block proportional).
var xmrB58Alpha = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

func xmrBase58Encode(data []byte) string {
        const blockSize = 8
        outSizes := [9]int{0, 2, 3, 5, 6, 7, 9, 10, 11}
        fullBlocks := len(data) / blockSize
        rem := len(data) % blockSize
        result := make([]byte, 0, fullBlocks*11+outSizes[rem])

        encodeBlock := func(block []byte, outLen int) []byte {
                num := new(big.Int).SetBytes(block) // big-endian
                out := make([]byte, outLen)
                for i := outLen - 1; i >= 0; i-- {
                        mod := new(big.Int)
                        num.DivMod(num, big.NewInt(58), mod)
                        out[i] = xmrB58Alpha[mod.Int64()]
                }
                return out
        }

        for i := 0; i < fullBlocks; i++ {
                result = append(result, encodeBlock(data[i*blockSize:(i+1)*blockSize], 11)...)
        }
        if rem > 0 {
                result = append(result, encodeBlock(data[fullBlocks*blockSize:], outSizes[rem])...)
        }
        return string(result)
}

// ── Payment detection via MyMonero-compatible light wallet server ──────────────

// xmrLightServers returns the ordered list of MyMonero-compatible servers to try.
// If XMR_LIGHT_SERVER env var is set, it is tried first before the public fallbacks.
func xmrLightServers() []string {
        defaults := []string{
                "https://openmonero.co",
                "https://mymonero.com",
                "https://node.moneroworld.com:8082",
        }
        if s := strings.TrimRight(os.Getenv("XMR_LIGHT_SERVER"), "/"); s != "" {
                return append([]string{s}, defaults...)
        }
        return defaults
}

// scanXMROnServer tries to fetch total_received from a single light wallet server.
func scanXMROnServer(server, subAddr, viewKeyHex string) (uint64, error) {
        loginBody, _ := json.Marshal(map[string]interface{}{
                "address":           subAddr,
                "view_key":          viewKeyHex,
                "create_account":    true,
                "generated_locally": false,
        })
        loginResp, err := xmrHTTPClient.Post(server+"/login", "application/json",
                strings.NewReader(string(loginBody)))
        if err != nil {
                return 0, fmt.Errorf("login: %w", err)
        }
        loginResp.Body.Close()

        txBody, _ := json.Marshal(map[string]string{
                "address":  subAddr,
                "view_key": viewKeyHex,
        })
        txResp, err := xmrHTTPClient.Post(server+"/get_address_txs", "application/json",
                strings.NewReader(string(txBody)))
        if err != nil {
                return 0, fmt.Errorf("get_address_txs: %w", err)
        }
        defer txResp.Body.Close()

        if txResp.StatusCode < 200 || txResp.StatusCode >= 300 {
                return 0, fmt.Errorf("HTTP %d", txResp.StatusCode)
        }

        var result struct {
                TotalReceived uint64 `json:"total_received"`
        }
        if err := json.NewDecoder(txResp.Body).Decode(&result); err != nil {
                return 0, fmt.Errorf("decode: %w", err)
        }
        return result.TotalReceived, nil
}

// ScanXMRSubaddressDirect checks piconero received at a subaddress (account=0, index)
// by querying MyMonero-compatible light wallet servers with automatic fallback.
func ScanXMRSubaddressDirect(_, _ string, index uint32) (uint64, error) {
        keys, err := loadXMRKeys()
        if err != nil {
                return 0, err
        }

        var subAddr string
        if index == 0 {
                subAddr = keys.primaryAddr
        } else {
                subAddr, err = DeriveXMRSubaddressFromEnv(index)
                if err != nil {
                        return 0, err
                }
        }

        viewKeyHex := hex.EncodeToString(keys.aBytes)
        servers := xmrLightServers()

        var lastErr error
        for _, server := range servers {
                total, err := scanXMROnServer(server, subAddr, viewKeyHex)
                if err != nil {
                        log.Printf("[xmr] light server %s failed: %v — trying next", server, err)
                        lastErr = err
                        continue
                }
                return total, nil
        }
        return 0, fmt.Errorf("all XMR light servers failed; last error: %w", lastErr)
}

// ── Price + conversion ────────────────────────────────────────────────────────

func GetXMRPrice() (float64, error) {
        resp, err := xmrHTTPClient.Get("https://api.coingecko.com/api/v3/simple/price?ids=monero&vs_currencies=usd")
        if err != nil {
                return 0, err
        }
        defer resp.Body.Close()
        var data struct {
                Monero struct {
                        USD float64 `json:"usd"`
                } `json:"monero"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
                return 0, err
        }
        if data.Monero.USD == 0 {
                return 0, fmt.Errorf("zero XMR price returned")
        }
        return data.Monero.USD, nil
}

func USDToXMR(usd, xmrPriceUSD float64) float64 {
        if xmrPriceUSD == 0 {
                return 0
        }
        return float64(uint64(usd/xmrPriceUSD*1e12+0.5)) / 1e12
}

func XMRToPiconero(xmr float64) uint64 { return uint64(xmr * 1e12) }
func PiconeroToXMR(p uint64) float64   { return float64(p) / 1e12 }
