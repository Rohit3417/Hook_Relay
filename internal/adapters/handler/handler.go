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
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	pool          *pgxpool.Pool
	client        *redis.Client
	queue         ports.EventQueue
	privateKey    *rsa.PrivateKey
	totpMasterKey []byte
}

type publishRequest struct {
	EndpointID string          `json:"endpoint_id"`
	Payload    json.RawMessage `json:"payload"`
}

type publishResponse struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
}

const (
	TierStarter           = "STARTER"
	TierGrowth            = "GROWTH"
	TierEnterprise        = "ENTERPRISE"
	QuotaStarter    int64 = 1000    // 1,000 events/month
	QuotaGrowth     int64 = 100000  // 100,000 events/month
	QuotaEnterprise int64 = 1000000 // 1,000,000 events/month (or -1 for unlimited)
)

// Helper: Returns quota limit based on tier
func GetTierQuota(tier string) int64 {
	switch strings.ToUpper(tier) {
	case TierGrowth:
		return QuotaGrowth
	case TierEnterprise:
		return QuotaEnterprise
	default:
		return QuotaStarter
	}
}

func NewHandler(pool *pgxpool.Pool, client *redis.Client, queue ports.EventQueue, privateKey *rsa.PrivateKey, totpKey []byte) *Handler {
	return &Handler{pool: pool, client: client, queue: queue, privateKey: privateKey, totpMasterKey: totpKey}
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
	// CORRECT (hashes plainKey with SHA-256)
	plainKey = strings.TrimSpace(plainKey)
	hash := sha256.Sum256([]byte(plainKey))
	keyHash := hex.EncodeToString(hash[:])
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
	}

	// Cache for future requests
	_ = h.client.Set(ctx, redisKey, tenantID, 1*time.Hour).Err()
	// 1. Fetch tenant's tier from Redis cache (or Postgres on cache miss)
	tierCacheKey := "tenant_tier:" + tenantID
	tier, err := h.client.Get(ctx, tierCacheKey).Result()
	if err != nil {
		_ = h.pool.QueryRow(ctx, "SELECT tier FROM tenants WHERE id = $1", tenantID).Scan(&tier)
		if tier == "" {
			tier = TierStarter
		}
		_ = h.client.Set(ctx, tierCacheKey, tier, 24*time.Hour).Err()
	}
	// 2. Monthly bucket key: "metering:<tenantID>:YYYYMM"
	monthKey := "metering:" + tenantID + ":" + time.Now().UTC().Format("200601")
	// 3. Check usage against the tenant's tier quota
	currentUsage, _ := h.client.Get(ctx, monthKey).Int64()
	quota := GetTierQuota(tier)
	if quota != -1 && currentUsage >= quota {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("%s quota exceeded (%d/%d events); please upgrade your plan", tier, currentUsage, quota))
		return
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

	// 8. Event is safely accepted -> Increment counters now:
	// A) Monthly counter for quota/billing
	_ = h.client.Incr(ctx, monthKey).Err()
	// Set TTL to 35 days so old months auto-clean from Redis
	_ = h.client.Expire(ctx, monthKey, 35*24*time.Hour).Err()

	// 1. Current hour bucket key: "metering:events:YYYYMMDDHH"
	hourBucket := time.Now().UTC().Format("2006010215")
	redisUsageKey := "metering:" + hourBucket

	// 2. Increment this tenant's counter by 1
	_ = h.client.HIncrBy(ctx, redisUsageKey, tenantID, 1).Err()

	writeJSON(w, http.StatusAccepted, publishResponse{
		EventID: id,
		Status:  "accepted",
	})

}
