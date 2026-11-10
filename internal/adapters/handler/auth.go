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
