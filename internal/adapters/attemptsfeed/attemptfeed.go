package attemptsfeed

import (
	"Hook_Relay2/internal/core/domain"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const publishTimeout = 2 * time.Second

type Message struct {
	ID         string `json:"id"` // delivery attempt id
	EventID    string `json:"event_id"`
	EndpointID string `json:"endpoint_id"`
	TenantID   string `json:"tenant_id"`
	AttemptNo  int    `json:"attempt_no"`
	Success    bool   `json:"success"`
	StatusCode int    `json:"status_code"` // 0 means no response
	ErrMsg     string `json:"error_message"`
	DurationMs int64  `json:"duration_ms"`
	CreatedAt  int64  `json:"created_at"`
}

func EndpointChannel(endpointID string) string {
	return "live:attempts:endpoint:" + endpointID
}

func TenantChannel(tenantID string) string {
	return "live:attempts:tenant:" + tenantID
}

// NewMessage maps a domain attempt, plus the id and timestamp the database
// generated (returned by storage.RecordAttempt), into a wire Message.
func NewMessage(a domain.DeliveryAttempt, id string, createdAt time.Time) Message {
	return Message{
		ID:         id,
		EventID:    a.EventID,
		EndpointID: a.EndpointID,
		TenantID:   a.TenantID,
		AttemptNo:  a.AttemptNo,
		Success:    a.Success,
		StatusCode: a.HTTPStatus,
		ErrMsg:     a.ErrMsg,
		DurationMs: a.Duration.Milliseconds(),
		CreatedAt:  createdAt.UnixMilli(),
	}
}

type Publisher struct {
	client *redis.Client
}

func New(client *redis.Client) *Publisher {
	return &Publisher{client: client}
}

func (p *Publisher) Publish(ctx context.Context, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal attempt message %s: %w", msg.ID, err)
	}

	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()

	pipe := p.client.Pipeline()
	pipe.Publish(ctx, EndpointChannel(msg.EndpointID), data)
	pipe.Publish(ctx, TenantChannel(msg.TenantID), data)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("publish attempt %s (endpoint %s, tenant %s): %w", msg.ID, msg.EndpointID, msg.TenantID, err)
	}

	return nil
}
