package domain

import "time"

// 0 means no response and empty means no error
type DeliveryAttempt struct {
	EventID    string
	EndpointID string
	TenantID   string
	AttemptNo  int
	HTTPStatus int
	Success    bool
	ErrMsg     string
	Duration   time.Duration
}
