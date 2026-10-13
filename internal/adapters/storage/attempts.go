package storage

import (
	"context"
	"fmt"

	"Hook_Relay2/internal/core/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RecordAttempt(ctx context.Context, pool *pgxpool.Pool, attempt domain.DeliveryAttempt) error {

	var statusCode *int //For storing null value if present in status_code and err msg
	var errMsg *string
	if attempt.HTTPStatus != 0 {
		statusCode = &attempt.HTTPStatus
	}
	if attempt.ErrMsg != "" {
		errMsg = &attempt.ErrMsg
	}

	_, err := pool.Exec(ctx, "INSERT INTO delivery_attempts (event_id, endpoint_id, tenant_id, attempt_number, status_code, success, error_message, duration_ms) values ($1, $2, $3, $4, $5, $6, $7, $8)", attempt.EventID, attempt.EndpointID, attempt.TenantID, attempt.AttemptNo, statusCode, attempt.Success, errMsg, attempt.Duration.Milliseconds())
	if err != nil {
		return fmt.Errorf("failed to insert delivery attempt (event: %s, endpoint: %s, attempt: %d): %w",
			attempt.EventID, attempt.EndpointID, attempt.AttemptNo, err)
	}

	return nil
}

func CountAttempts(ctx context.Context, pool *pgxpool.Pool, eventID string) (int, error) {
	var count int

	err := pool.QueryRow(ctx, "SELECT COUNT(*) from delivery_attempts WHERE event_id = $1", eventID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count delivery attempts for event %s: %w", eventID, err)
	}

	return count, nil
}
