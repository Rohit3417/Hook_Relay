package handler

import (
	"Hook_Relay2/internal/core/domain"
	"Hook_Relay2/internal/core/ports"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	pool   *pgxpool.Pool
	client *redis.Client
	queue  ports.EventQueue
}

type publishRequest struct {
	TenantID   string
	EndpointID string
	Payload    []byte
}

type publishResponse struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
}

func NewHandler(pool *pgxpool.Pool, client *redis.Client, queue ports.EventQueue) *Handler {
	return &Handler{pool: pool, client: client, queue: queue}
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
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed: use POST")
		return
	}

	var req publishRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.TenantID == "" || req.EndpointID == "" {
		writeError(w, http.StatusBadRequest, "tenant_id and endpoint_id are required")
		return
	}

	id := uuid.New().String()
	event := domain.NewEvent(id, req.TenantID, req.EndpointID, req.Payload)

	err = h.queue.Publish(r.Context(), event)
	if err != nil {
		log.Printf("publish failed for tenant=%s endpoint=%s: %v", req.TenantID, req.EndpointID, err)
		writeError(w, http.StatusInternalServerError, "failed to queue event")
		return
	}

	writeJSON(w, http.StatusAccepted, publishResponse{
		EventID: id,
		Status:  "accepted",
	})
}
