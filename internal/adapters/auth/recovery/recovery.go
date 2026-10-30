package recovery

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Generate creates recovery codes and their HMAC-SHA256 hashes
func Generate(n int, secretKey []byte) ([]string, []string, error) {
	if n < 1 || len(secretKey) < 32 {
		return nil, nil, errors.New("invalid arguments")
	}

	plainCodes := make([]string, 0, n)
	hashedCodes := make([]string, 0, n)

	for i := 0; i < n; i++ {
		randomBytes := make([]byte, 10)
		if _, err := rand.Read(randomBytes); err != nil {
			return nil, nil, err
		}

		var codeBuilder strings.Builder
		for j := 0; j < 10; j++ {
			char := alphabet[randomBytes[j]&31]
			codeBuilder.WriteByte(char)

			if j == 4 {
				codeBuilder.WriteByte('-')
			}
		}

		plainCode := codeBuilder.String()
		hash, err := Hash(plainCode, secretKey)
		if err != nil {
			return nil, nil, err
		}

		plainCodes = append(plainCodes, plainCode)
		hashedCodes = append(hashedCodes, hash)
	}

	return plainCodes, hashedCodes, nil
}

// Hash normalizes the code and returns its HMAC-SHA256 hex string
func Hash(code string, key []byte) (string, error) {
	if len(key) < 32 {
		return "", errors.New("secret key must be at least 32 bytes")
	}

	normalized := Normalize(code)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// Normalize simply converts to uppercase and removes spaces/dashes
func Normalize(input string) string {
	input = strings.ToUpper(input)
	input = strings.ReplaceAll(input, "-", "")
	input = strings.ReplaceAll(input, " ", "")
	return input
}
