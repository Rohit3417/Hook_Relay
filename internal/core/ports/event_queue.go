package ports

import (
	"Hook_Relay2/internal/core/domain"
	"context"
	"time"
)

// Interface
type EventQueue interface {

	//Publishing tells our worker (slow work) to start working on whatever function it is and to our customer sends 202 ok
	// if no error occured
	Publish(ctx context.Context, event domain.Event) error

	// Read new pending events that needs to be processed
	//Redis needs to know who the group and consumer is so that it does not assign same work to 2 workers
	ReadPending(ctx context.Context, group string, consumer string, count int64) ([]QueuedEvents, error)

	//Ack acknowlegdes successful processing of message, removing it from consumers group's pending entries list(PEL)
	Ack(ctx context.Context, group string, messageID string) error

	//ReclaimStale reassigns messages that have been pending
	//if any work is idle for too long, it means the worker must have been crashed, so redis reassigns it.
	// Consumer is redis asking who's going to take the ownership
	ReclaimStale(ctx context.Context, group string, consumer string, minIdle time.Duration) ([]QueuedEvents, error)

	//Once an event has exhausted MaxDeliveryAttempts reached, it should not stay in the main flow
	DeadLetter(ctx context.Context, event domain.Event) error
}

// QueuedEvents wraps domain.Event with specific message ID that is needed by the Ack, ReclaimState
type QueuedEvents struct {
	MessageID string
	Event     domain.Event
}
