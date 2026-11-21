# HookRelay Authentication & Security Module

A modular, production-ready, fail-closed authentication and security system built for the HookRelay API. This module implements industry-standard cryptographic primitives in Go to handle secure user authentication, multi-factor authorization, and stateless session management.

## 🛡️ Core Security Features

* **Password Hashing (Argon2id):** Utilizes the modern Argon2id key derivation function, outputting hashes in the standardized Password Hashing Competition (PHC) format to defend against GPU-based brute-force and side-channel attacks.
* **TOTP Multi-Factor Authentication:** Standard Time-Based One-Time Passwords (authenticator apps) with a 30-second T±1 drift window to handle minor client clock desynchronization.
* **Cryptographic Replay Guard:** Utilizes Redis (`SETNX`) to enforce strict single-use policies on TOTP codes within their active time window, preventing intercepted code replay.
* **Encrypted Secrets at Rest (AES-GCM):** TOTP base32 secrets are never stored in plaintext. They are encrypted using AES-GCM. An Additional Authenticated Data (AAD) binding using the PostgreSQL User UUID strictly ties the ciphertext to the user's row, rendering database leaks useless.
* **Emergency Recovery Codes:** Generates ten 10-character offline backup codes using a custom, look-alike-free 32-character alphabet. Codes are stored purely as HMAC-SHA256 hashes and consumed via strictly atomic PostgreSQL `UPDATE` transactions to prevent race conditions.
* **Stateless API Sessions (RS256 JWT):** Uses asymmetric RSA key pairs to sign (Private Key) and verify (Public Key) JWTs, preventing algorithm downgrade attacks and enabling secure decentralized verification across microservices.

## 📦 Module Structure

The security primitives are completely isolated into independent adapters:

```text
hookrelay/
├── security/password/   # Argon2id PHC hashing and subtle constant-time verification
├── auth/totp/           # Time-based code generation and drift verification
├── auth/recovery/       # Custom base32 code generation, normalization, and hashing
├── secretbox/           # AES-GCM encryption/decryption with AAD row-binding
├── jwtauth/             # RS256 token generation, key loading, and HTTP middleware
└── cmd/main.go          # Application entry point and route wiring
```

## ⚙️ Prerequisites & Environment Setup

To run this authentication module, ensure you have the following services running:
* **Go** 1.21+
* **PostgreSQL** (for storing users, tenants, and hashed recovery codes)
* **Redis** (for TOTP replay locks)

### Required Environment Variables

You must provide secure, random keys via your environment to bootstrap the cryptography:

```bash
# 32-byte Base64-encoded master key for AES-GCM SecretBox
export SECRETBOX_MASTER_KEY="your-32-byte-base64-string="

# 32-byte Base64-encoded secret key for HMAC-SHA256 Recovery Codes
export RECOVERY_HMAC_KEY="your-32-byte-base64-string="

# PostgreSQL Database URL
export DATABASE_URL="postgres://user:password@localhost:5432/hookrelay?sslmode=disable"

# Redis Connection String
export REDIS_URL="redis://localhost:6379/0"
```

### RSA Keys for JWT

Generate your RS256 key pair for JWT signing/verifying and place them in your configured keys directory:

```bash
# Generate Private Key
openssl genpkey -algorithm RSA -out private.pem -pkeyopt rsa_keygen_bits:2048

# Extract Public Key
openssl rsa -pubout -in private.pem -out public.pem
```

## 🚀 Integration via HTTP Middleware

Protected API routes (like GraphQL endpoints) are secured using the provided HTTP middleware, which strictly verifies the RS256 token signature and extracts the `userID` into the request context.

```go
// Load public key on startup
pubKeyBytes, _ := os.ReadFile("keys/public.pem")
publicKey, _ := jwtauth.LoadPublicKey(pubKeyBytes)

// Protect routes
mux.Handle("/api/graphql", jwtauth.AuthMiddleware(publicKey)(graphqlHandler))
```
