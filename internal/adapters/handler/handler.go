package handler

import (
	"Hook_Relay2/internal/adapters/storage"
	"Hook_Relay2/internal/core/domain"
	"Hook_Relay2/internal/core/ports"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	pool       *pgxpool.Pool
	client     *redis.Client
	queue      ports.EventQueue
	privateKey *rsa.PrivateKey
}

type publishRequest struct {
	EndpointID string          `json:"endpoint_id"`
	Payload    json.RawMessage `json:"payload"`
}

type publishResponse struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
}

func NewHandler(pool *pgxpool.Pool, client *redis.Client, queue ports.EventQueue, privateKey *rsa.PrivateKey) *Handler {
	return &Handler{pool: pool, client: client, queue: queue, privateKey: privateKey}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{
		"error": msg,
	})
}

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"error":  "postgres unreachable",
		})
		return
	}

	if err := h.client.Ping(ctx).Err(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"error":  "redis unreachable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	// 1. Extract API key from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		writeError(w, http.StatusUnauthorized, "missing Authorization header")
		return
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		writeError(w, http.StatusUnauthorized, "invalid header format, expected: Bearer <api_key>")
		return
	}
	plainKey := parts[1]

	// 2. Hash the key with SHA-256
	keyHash := hex.EncodeToString(sha256.New().Sum([]byte(plainKey)))
	redisKey := "apikey:" + keyHash

	// 3. Lookup tenant_id in Redis (fast path)
	ctx := r.Context()
	tenantID, err := h.client.Get(ctx, redisKey).Result()
	if err != nil {
		// Cache miss: check Postgres
		err = h.pool.QueryRow(ctx, "SELECT id FROM tenants WHERE api_key_hash = $1", keyHash).Scan(&tenantID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		// Cache for future requests
		_ = h.client.Set(ctx, redisKey, tenantID, 1*time.Hour).Err()
	}

	// 4. Parse JSON body
	var req publishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.EndpointID == "" || len(req.Payload) == 0 {
		writeError(w, http.StatusBadRequest, "endpoint_id and payload are required")
		return
	}

	// 5. Verify the endpoint belongs to this tenant
	var endpointExists bool
	err = h.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM endpoints WHERE id = $1 AND tenant_id = $2)",
		req.EndpointID, tenantID,
	).Scan(&endpointExists)
	if err != nil || !endpointExists {
		writeError(w, http.StatusNotFound, "endpoint not found for this tenant")
		return
	}

	// 6. Persist event in Postgres
	id := uuid.New().String()
	event := domain.NewEvent(id, tenantID, req.EndpointID, []byte(req.Payload))

	if err := storage.SaveEvent(ctx, h.pool, event); err != nil {
		log.Printf("failed to persist event %s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to persist event")
		return
	}

	// 7. Push to Redis Stream
	if err := h.queue.Publish(ctx, event); err != nil {
		log.Printf("publish failed for tenant=%s: %v", tenantID, err)
		writeError(w, http.StatusInternalServerError, "failed to queue event")
		return
	}

	writeJSON(w, http.StatusAccepted, publishResponse{
		EventID: id,
		Status:  "accepted",
	})
}
