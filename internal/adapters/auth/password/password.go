package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonVer     = 19
	iterations   = uint32(3)
	memoryKiB    = uint32(64 * 1024)
	parallelism  = uint8(2)
	keyLen       = uint32(32)
	maxMemory    = uint32(128 * 1024)
	maxIteration = uint32(10)
	maxThreads   = uint8(10)
	maxPass      = 128
)

func Hash(password string) (string, error) {

	if len(password) > maxPass {
		return "", fmt.Errorf("passwrod too long")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	// 16-byte salt, 3 iteration, 64MiB memory, 2 threads (1 or 2 CPU cores can work), 32 byte output size
	key := argon2.IDKey([]byte(password), salt, iterations, memoryKiB, parallelism, keyLen)

	saltStr := base64.RawStdEncoding.EncodeToString(salt)
	keyStr := base64.RawStdEncoding.EncodeToString(key)

	// v argon2id version, m = memory limit, t = iteration, p = parallelism/threads, base64 encoded salt and key
	passHash := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argonVer, memoryKiB, iterations, parallelism, saltStr, keyStr)

	return passHash, nil
}

// the encoded string is fetched from our database not user input so we directly compare
func Verify(password string, encoded string) (bool, error) {

	if len(password) > maxPass {
		return false, nil
	}
	//Breakdown the encoded string exactly we get 6 paths
	part := strings.Split(encoded, "$")
	/*
		part[t0] = ""
		part[1] = "argon2id"
		part[2] = "v=19"
		part[3] = "m=.., t=.., p=.."
		part[4] = salt
		part[5] = key
	*/

	if len(part) != 6 {
		return false, fmt.Errorf("invalid hash format")
	}
	if part[1] != "argon2id" || part[2] != fmt.Sprintf("v=%d", argonVer) {
		return false, fmt.Errorf("incompatible algorithm: %s", part[1])
	}

	//extract m, t, p
	var memory, iteration uint32
	var threads uint8
	_, err := fmt.Sscanf(part[3], "m=%d,t=%d,p=%d", &memory, &iteration, &threads)
	if err != nil {
		return false, fmt.Errorf("failed to parse paramters; %v", err)
	}
	if iteration == 0 || threads == 0 || memory > maxMemory || threads > maxThreads || iteration > maxIteration {
		return false, fmt.Errorf("invalid parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(part[4])
	if err != nil {
		return false, fmt.Errorf("failed to decode")
	}
	key, err := base64.RawStdEncoding.DecodeString(part[5])
	if err != nil {
		return false, fmt.Errorf("failed to decode")
	}

	if len(salt) < 8 || len(key) < 16 {
		return false, fmt.Errorf("invalid parameters")
	}

	newKey := argon2.IDKey([]byte(password), salt, iteration, memory, threads, uint32(len(key)))

	if subtle.ConstantTimeCompare(key, newKey) == 1 {
		return true, nil
	} else {
		return false, nil
	}

}
