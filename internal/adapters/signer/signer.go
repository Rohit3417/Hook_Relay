package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func Sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	currTime := time.Now().Unix()

	// build the string to hash explicitly and safely, with a clear separator, without touching the original payload
	mac.Write([]byte(strconv.FormatInt(currTime, 10)))
	// Our separator for timestamp and payload as mentioned in guide
	mac.Write([]byte("."))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("t=%d,v1=%s", currTime, signature)
}

func Verify(payload []byte, header string, secret string) error {
	parts := strings.Split(header, ",")
	if len(parts) != 2 {
		return errors.New("invalid header format")
	}
	timePart := parts[0]
	sigPart := parts[1]

	timeVals := strings.SplitN(timePart, "=", 2)
	if len(timeVals) != 2 {
		return errors.New("invalid timestamp format")
	}
	timeStr := timeVals[1]

	sigVals := strings.SplitN(sigPart, "=", 2)
	if len(sigVals) != 2 {
		return errors.New("invalid signature format")
	}
	receivedHash := sigVals[1]

	timestamp, err := strconv.ParseInt(timeStr, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing timestamp: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	expectedHash := hex.EncodeToString(mac.Sum(nil))

	if hmac.Equal([]byte(expectedHash), []byte(receivedHash)) {
		return nil
	}

	return errors.New("signature mismatch")
}
