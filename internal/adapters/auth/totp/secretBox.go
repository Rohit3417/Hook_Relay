package totp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// NOTE : the aad will conttain the userid in bytes so that we encrypt the secret along with userid so no 2 users will have the same secret

// Encrypting the secret to secure TOTP secrte for database storeage
// So if database is leaked the stolen columns are useless

// The key is not in database
func EncryptSecret(plainTextSecret string, masterKey []byte, aad []byte) (string, error) {
	if len(masterKey) != 32 {
		return "", fmt.Errorf("invalid encryption key: must be exactly 32 bytes")
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// Create a 12-byte nonce
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// append the ciphertext directly to nonce
	cipherText := aesGCM.Seal(nonce, nonce, []byte(plainTextSecret), aad)

	return base64.StdEncoding.EncodeToString(cipherText), nil
}

// Retriving the plain Secret from database
func DecryptSecret(encryptedBase64 string, masterKey []byte, aad []byte) (string, error) {
	if len(masterKey) != 32 {
		return "", fmt.Errorf("invalid master key: must be exactly 32 bytes")
	}

	cipherText, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(cipherText) < nonceSize {
		return "", fmt.Errorf("cipher text too short")
	}

	// split the nonce and cipher text
	nonce, encryptedMessage := cipherText[:nonceSize], cipherText[nonceSize:]

	// Open decrypt and authenticate message
	plainTextBytes, err := aesGCM.Open(nil, nonce, encryptedMessage, aad)
	if err != nil {
		return "", err
	}

	return string(plainTextBytes), nil
}
