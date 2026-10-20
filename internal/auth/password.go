package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultParams = &argon2Params{
	memory:      64 * 1024,
	iterations:  3,
	parallelism: 2,
	saltLength:  16,
	keyLength:   32,
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, defaultParams.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey(
		[]byte(password),
		salt,
		defaultParams.iterations,
		defaultParams.memory,
		defaultParams.parallelism,
		defaultParams.keyLength,
	)
	b64salt := base64.RawStdEncoding.EncodeToString(salt)
	b64hash := base64.RawStdEncoding.EncodeToString(hash)
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		defaultParams.memory,
		defaultParams.iterations,
		defaultParams.parallelism,
		b64salt,
		b64hash,
	)
	return encoded, nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	vals := strings.Split(encoded, "$")
	if len(vals) != 6 {
		return false, errors.New("invalid hash format")
	}
	var version int
	if _, err := fmt.Sscanf(vals[2], "v=%d", &version); err != nil {
		return false, err
	}
	if version != argon2.Version {
		return false, errors.New("incompatible argon2 version")
	}
	p := &argon2Params{}
	if _, err := fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(vals[4])
	if err != nil {
		return false, err
	}
	decodedHash, err := base64.RawStdEncoding.DecodeString(vals[5])
	if err != nil {
		return false, err
	}
	p.keyLength = uint32(len(decodedHash))
	comparisonHash := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)
	return subtle.ConstantTimeCompare(decodedHash, comparisonHash) == 1, nil
}
