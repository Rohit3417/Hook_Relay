package storage

import (
	"Hook_Relay2/internal/core/domain"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SaveEvent persists an ingested event into the Postgres events table.
func SaveEvent(ctx context.Context, pool *pgxpool.Pool, event domain.Event) error {
	query := `
		INSERT INTO events (id, tenant_id, endpoint_id, payload, status, attempts, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := pool.Exec(ctx, query,
		event.ID,
		event.TenantID,
		event.EndpointID,
		event.Payload,
		string(event.Status),
		event.Attempts,
		event.CreatedAt,
		event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save event %s: %w", event.ID, err)
	}

	return nil
}

// UpdateEventStatus updates status, attempts, and updated_at timestamp for an event.
func UpdateEventStatus(ctx context.Context, pool *pgxpool.Pool, eventID string, status domain.EventStatus, attempts int) error {
	query := `
		UPDATE events
		SET status = $1, attempts = $2, updated_at = $3
		WHERE id = $4
	`
	_, err := pool.Exec(ctx, query, string(status), attempts, time.Now().UTC(), eventID)
	if err != nil {
		return fmt.Errorf("failed to update event %s status to %s: %w", eventID, status, err)
	}

	return nil
}
