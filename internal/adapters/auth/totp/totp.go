package totp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"
)

// generating uri/QR code using 20 byte random secret for third party like google authenticator
func GenerateEnrollment(email string) (secret, uri string, err error) {

	secretBytes := make([]byte, 20)

	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate secret: %w", err)
	}

	// Base32 encoding without padding
	secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secretBytes)

	//URL encode with issuer:AccountName
	// URL pathEscape replace special char to their respective format like @ to %40 or %20 for space
	label := url.PathEscape(fmt.Sprintf("HookRelay:%s", email))

	uri = fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=HookRelay&algorithm=SHA1&digits=6&period=30", label, secret)

	return secret, uri, nil
}

// Generate 6 digit code for a given time
func GenerateCode(base32Secret string, t time.Time) (string, error) {
	//decode the secret back into raw bytes
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(base32Secret)
	if err != nil {
		return "", fmt.Errorf("failed to decode base 32 secret: %w", err)
	}

	// Calculate time step (T) and convert to 8-byte big endian
	timeStep := t.Unix() / 30
	timeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBytes, uint64(timeStep))

	// COmpute HMAC-SHA1
	mac := hmac.New(sha1.New, secretBytes)
	mac.Write(timeBytes)
	hash := mac.Sum(nil)

	// Dynamic Truncation
	// To get the last 4 bits of hash to use as starting offset
	offset := hash[len(hash)-1] & 0x0F // binary of 00001111

	// Extract 4 bytes starting from offset
	truncatedHash := hash[offset : offset+4]

	// Convert those 4 bytes into 32 bit integer, masking most significant bit to avoid negative numbers
	codeInt := binary.BigEndian.Uint32(truncatedHash) & 0x7FFFFFFF // the first bit is 0 rest all 31 bit are 1 as 1st decides + or -

	//APply Modulo 1,000,000 to get 6 digit number	and format with padding 0
	totpCode := codeInt % 1000000
	return fmt.Sprintf("%06d", totpCode), nil
}

// Checking against a 30 second window
func VerifyCode(base32Secret string, submittedCode string, currentTime time.Time) (int64, bool, error) {
	if len(submittedCode) != 6 {
		return 0, false, nil
	}

	// The submitted code should have only digits
	for _, char := range submittedCode {
		if char < '0' || char > '9' {
			return 0, false, nil
		}
	}

	//Define the time window
	timeOffSet := []time.Duration{
		-30 * time.Second,
		0,
		30 * time.Second,
	}

	// check for every time (only for 3 values of time and not every second)
	for _, offset := range timeOffSet {
		targetTime := currentTime.Add(offset)

		expectedCode, err := GenerateCode(base32Secret, targetTime)
		if err != nil {
			return 0, false, err
		}

		if subtle.ConstantTimeCompare([]byte(expectedCode), []byte(submittedCode)) == 1 {
			matchedStep := targetTime.Unix() / 30
			return matchedStep, true, nil
		}
	}

	return 0, false, nil
}

// This guarantees that even if an attacker sends the exact same TOTP code in
// two parallel HTTP requests at the exact same millisecond, Redis will strictly grant
// the lock to the first request and reject the second
func EnforceReplayGuard(ctx context.Context, rdb *redis.Client, userID string, matchedStep int64) (bool, error) {
	// 1. Build the unique key from the user ID and the exact time step
	key := fmt.Sprintf("totp:replay:%s:%d", userID, matchedStep)
	newCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// 2. Execute SET key value NX EX
	// NX: Only set the key if it does Not eXist.
	// EX: Set an expiration (TTL) so Redis cleans it up automatically.
	// 90 seconds covers your T-1 to T+1 drift window.
	locked, err := rdb.SetNX(newCtx, key, "used", 90*time.Second).Result()

	if err != nil {
		// 3. Strict failure: If Redis is unreachable, reject the login entirely
		return false, fmt.Errorf("redis unavailable, rejecting login to fail closed: %w", err)
	}

	// If locked is false, the key already existed, meaning this code was just used
	return locked, nil
}
