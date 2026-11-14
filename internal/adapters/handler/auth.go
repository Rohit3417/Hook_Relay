package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	jwtauth "Hook_Relay2/internal/adapters/auth/jwtAuth"
	"Hook_Relay2/internal/adapters/auth/password"
	"Hook_Relay2/internal/adapters/auth/recovery"
	"Hook_Relay2/internal/adapters/auth/totp"
)

type signupRequest struct {
	TenantName string `json:"tenant_name"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token    string `json:"token"`
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	APIKey   string `json:"api_key,omitempty"`
}

type verify2FARequest struct {
	Code string `json:"code"`
}

// Helper: Generates a random API key and its SHA-256 hash
func generateAPIKey() (plainKey string, keyHash string, err error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	plainKey = "hr_live_" + hex.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(plainKey))
	keyHash = hex.EncodeToString(hash[:])
	return plainKey, keyHash, nil
}

// POST /api/v1/auth/signup
func (h *Handler) Signup(w http.ResponseWriter, r *http.Request) {

	apiKey, keyHash, err := generateAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate api key")
		return
	}

	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" || req.TenantName == "" {
		writeError(w, http.StatusBadRequest, "tenant_name, email, and password are required")
		return
	}

	// 1. Hash password with Argon2id
	passHash, err := password.Hash(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	// 2. Insert tenant
	var tenantID string
	err = h.pool.QueryRow(r.Context(),
		"INSERT INTO tenants (name, api_key_hash) VALUES ($1, $2) RETURNING id",
		req.TenantName, keyHash,
	).Scan(&tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create tenant")
		return
	}
	// Cache key in Redis immediately: apikey:<hash> -> tenantID
	_ = h.client.Set(r.Context(), "apikey:"+keyHash, tenantID, 24*time.Hour).Err()

	// 3. Insert user linked to tenant
	var userID string
	err = h.pool.QueryRow(r.Context(),
		"INSERT INTO users (tenant_id, email, password_hash) VALUES ($1, $2, $3) RETURNING id",
		tenantID, req.Email, passHash,
	).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}

	// 4. Issue RS256 JWT
	token, err := jwtauth.GenerateToken(userID, tenantID, h.privateKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{
		Token:    token,
		TenantID: tenantID,
		UserID:   userID,
		APIKey:   apiKey,
	})
}

// POST /api/v1/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 1. Look up user by email
	var userID, tenantID, passHash string
	err := h.pool.QueryRow(r.Context(),
		"SELECT id, tenant_id, password_hash FROM users WHERE email = $1",
		req.Email,
	).Scan(&userID, &tenantID, &passHash)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// 2. Verify Argon2id password
	match, err := password.Verify(req.Password, passHash)
	if err != nil || !match {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// 3. Issue RS256 JWT
	token, err := jwtauth.GenerateToken(userID, tenantID, h.privateKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		Token:    token,
		TenantID: tenantID,
		UserID:   userID,
	})
}

// 1. POST /api/v1/auth/2fa/setup (Requires Bearer JWT)
func (h *Handler) Setup2FA(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from authenticated JWT context
	userID, ok := jwtauth.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// Fetch user's email
	var email string
	err := h.pool.QueryRow(r.Context(), "SELECT email FROM users WHERE id = $1", userID).Scan(&email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "user not found")
		return
	}
	// Generate base32 secret and QR URI
	plainSecret, uri, err := totp.GenerateEnrollment(email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate 2FA secret")
		return
	}
	// Encrypt secret with AES-GCM using userID as AAD
	encryptedSecret, err := totp.EncryptSecret(plainSecret, h.totpMasterKey, []byte(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt 2FA secret")
		return
	}
	// Save encrypted secret in DB (totp_enabled remains false until verified)
	_, err = h.pool.Exec(r.Context(),
		"UPDATE users SET totp_secret_encrypted = $1 WHERE id = $2",
		encryptedSecret, userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store 2FA secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret":      plainSecret,
		"otpauth_url": uri,
	})
}

// 2. POST /api/v1/auth/2fa/enable (Requires Bearer JWT + 6-digit code)
func (h *Handler) Enable2FA(w http.ResponseWriter, r *http.Request) {
	userID, ok := jwtauth.GetUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req verify2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Code) != 6 {
		writeError(w, http.StatusBadRequest, "a 6-digit code is required")
		return
	}
	// Fetch encrypted secret from DB
	var encryptedSecret string
	err := h.pool.QueryRow(r.Context(),
		"SELECT totp_secret_encrypted FROM users WHERE id = $1",
		userID,
	).Scan(&encryptedSecret)
	if err != nil || encryptedSecret == "" {
		writeError(w, http.StatusBadRequest, "run 2fa/setup first")
		return
	}
	// Decrypt secret
	plainSecret, err := totp.DecryptSecret(encryptedSecret, h.totpMasterKey, []byte(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt secret")
		return
	}
	// Verify code (±30s window)
	matchedStep, valid, err := totp.VerifyCode(plainSecret, req.Code, time.Now())
	if err != nil || !valid {
		writeError(w, http.StatusBadRequest, "invalid 2FA code")
		return
	}
	// Check Redis replay guard
	allowed, err := totp.EnforceReplayGuard(r.Context(), h.client, userID, matchedStep)
	if err != nil || !allowed {
		writeError(w, http.StatusBadRequest, "code already used, wait for next code")
		return
	}
	// Generate 8 recovery codes
	plainCodes, hashedCodes, err := recovery.Generate(8, h.totpMasterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate recovery codes")
		return
	}
	// Save recovery codes and activate 2FA in a single transaction
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), "UPDATE users SET totp_enabled = true WHERE id = $1", userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate 2FA")
		return
	}
	for _, hash := range hashedCodes {
		_, _ = tx.Exec(r.Context(),
			"INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2)",
			userID, hash,
		)
	}
	_ = tx.Commit(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "2fa_enabled",
		"recovery_codes": plainCodes,
	})
}
