package totp

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/url"
)

// generating uri/QR code using 20 byte random secret for third party like google authenticator
func GenerateEnrollment(email string) (string, string, error) {

	secretBytes := make([]byte, 20)

	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate secret")
	}

	// Base32 encoding without padding
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secretBytes)

	//URL encode with issuer:AccountName
	// URL pathEscape replace special char to their respective format like @ to %40 or %20 for space
	label := url.PathEscape(fmt.Sprintf("HookRelay:%s", email))

	uri := fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=HookRelay", label, secret)

	return secret, uri, nil

}
