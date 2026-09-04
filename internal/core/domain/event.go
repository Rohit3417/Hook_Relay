package domain

import (
	"time"
)

const MaxDeliveryAttempts = 5

type EventStatus string

const (
	StatusPending   EventStatus = "pending"
	StatusDelivered EventStatus = "delivered"
	StatusDead      EventStatus = "dead"
)

type Event struct {
	ID         string
	TenantID   string
	EndpointID string
	Payload    []byte
	Status     EventStatus
	Attempts   int
	CreatedAt  time.Time
}

func NewEvent(ID string, TenantID string, EndPointID string, PayLoad []byte) Event {
	return Event{ID: ID, TenantID: TenantID, EndpointID: EndPointID, Payload: PayLoad, Status: StatusPending, Attempts: 0, CreatedAt: time.Now().UTC()}
}
