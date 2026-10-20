package payment

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"golang.org/x/crypto/pbkdf2"
)

var curveOrder, _ = new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)

func GetHDMnemonic() string {
	return strings.TrimSpace(os.Getenv("BTC_HD_MNEMONIC"))
}

func HDEnabled() bool {
	return GetHDMnemonic() != ""
}

func mnemonicToSeed(mnemonic string) []byte {
	return pbkdf2.Key([]byte(mnemonic), []byte("mnemonic"), 2048, 64, sha512.New)
}

type hdKey struct {
	privKey   []byte
	chainCode []byte
}

func deriveMasterKey(seed []byte) hdKey {
	mac := hmac.New(sha512.New, []byte("Bitcoin seed"))
	mac.Write(seed)
	I := mac.Sum(nil)
	return hdKey{privKey: I[:32], chainCode: I[32:]}
}

func deriveChild(parent hdKey, index uint32) (hdKey, error) {
	var data [37]byte
	if index >= 0x80000000 {
		data[0] = 0x00
		copy(data[1:33], parent.privKey)
	} else {
		privKey, _ := btcec.PrivKeyFromBytes(parent.privKey)
		pub := privKey.PubKey().SerializeCompressed()
		copy(data[:33], pub)
	}
	binary.BigEndian.PutUint32(data[33:], index)

	mac := hmac.New(sha512.New, parent.chainCode)
	mac.Write(data[:])
	I := mac.Sum(nil)
	IL, IR := I[:32], I[32:]

	ilInt := new(big.Int).SetBytes(IL)
	if ilInt.Cmp(curveOrder) >= 0 {
		return hdKey{}, fmt.Errorf("derived key IL >= curve order")
	}
	parentInt := new(big.Int).SetBytes(parent.privKey)
	childInt := new(big.Int).Mod(new(big.Int).Add(ilInt, parentInt), curveOrder)
	if childInt.Sign() == 0 {
		return hdKey{}, fmt.Errorf("derived child key is zero")
	}

	childPriv := make([]byte, 32)
	b := childInt.Bytes()
	copy(childPriv[32-len(b):], b)

	return hdKey{privKey: childPriv, chainCode: IR}, nil
}

// DeriveBTCAddress derives a P2PKH BTC address at BIP44 path m/44'/0'/0'/0/index
func DeriveBTCAddress(mnemonic string, index uint32) (string, error) {
	seed := mnemonicToSeed(mnemonic)
	key := deriveMasterKey(seed)

	path := []uint32{
		44 + 0x80000000,
		0 + 0x80000000,
		0 + 0x80000000,
		0,
		index,
	}

	var err error
	for _, idx := range path {
		key, err = deriveChild(key, idx)
		if err != nil {
			return "", fmt.Errorf("derive at index %d: %w", idx, err)
		}
	}

	privKey, _ := btcec.PrivKeyFromBytes(key.privKey)
	return pubKeyToP2PKH(privKey.PubKey().SerializeCompressed())
}
