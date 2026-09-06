package redisqueue

import (
	"Hook_Relay2/internal/core/domain"
	"Hook_Relay2/internal/core/ports"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const IngressStream = "stream:events:ingress"
const DlqStream = "stream:events:dlq"

type Client struct {
	client *redis.Client
}

// wireEvent is the JSON shape actually stored in Redis. Kept separate from
// domain.Event so the domain package never has to know JSON exists.
type wireEvent struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	EndpointID string `json:"endpoint_id"`
	Payload    []byte `json:"payload"` // encoding/json base64-encodes []byte automatically
	Status     string `json:"status"`
	Attempts   int    `json:"attempts"`
	CreatedAt  int64  `json:"created_at"` // unix millis — avoids relying on time.Time's default JSON format
}

func toWire(e domain.Event) wireEvent {
	return wireEvent{
		ID:         e.ID,
		TenantID:   e.TenantID,
		EndpointID: e.EndpointID,
		Payload:    e.Payload,
		Status:     string(e.Status),
		Attempts:   e.Attempts,
		CreatedAt:  e.CreatedAt.UnixMilli(),
	}
}
func fromWire(w wireEvent) domain.Event {
	return domain.Event{
		ID:         w.ID,
		TenantID:   w.TenantID,
		EndpointID: w.EndpointID,
		Payload:    w.Payload,
		Status:     domain.EventStatus(w.Status),
		Attempts:   w.Attempts,
		CreatedAt:  time.UnixMilli(w.CreatedAt).UTC(),
	}
}

func NewClient(client *redis.Client) Client {
	return Client{client: client}
}

// A method to create consumer group if it does not exists
func (q Client) EnsureGroup(ctx context.Context, group string) error {

	// we use $ if the history of group is not needed and 0 if need the history
	// .XGroupCreate return a error when the IngressStream (key) does not exists thus we use this which creates a stream of length 0
	err := q.client.XGroupCreateMkStream(ctx, IngressStream, group, "0").Err()
	if err != nil {
		// Handling the already exists case
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			return nil
		}
		return fmt.Errorf("ensure group %q: %w", group, err)
	}

	return nil
}

// How this dunction is called
// queue = redisqueue.NewClient(client)
// this queue becomes q for Publish function
func (q Client) Publish(ctx context.Context, event domain.Event) error {

	wire := toWire(event)

	body, err := json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("marshal event %s: %w", event.ID, err)
	}

	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: IngressStream,
		Values: map[string]interface{}{"event": body},
	}).Err()
}

func (q Client) ReadPending(ctx context.Context, group string, consumer string, count int64) ([]ports.QueuedEvents, error) {
	body, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{IngressStream, ">"}, //reading new, unclaimed messages uses ">" as the special ID.
		Count:    count,
		Block:    2 * time.Second,
	}).Result()

	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pending (group=%s, consumer=%s): %w", group, consumer, err)
	}

	var events []ports.QueuedEvents

	for _, stream := range body {
		for _, msg := range stream.Messages {
			var wire wireEvent
			messageID := msg.ID
			str, ok := msg.Values["event"].(string)
			if !ok {
				log.Printf("skipping message %s: missing or invalid 'event' field", messageID)
				continue
			}
			event := []byte(str)
			err = json.Unmarshal(event, &wire)
			if err != nil {
				log.Printf("skipping malformed message %s: %v", messageID, err)
				continue
			}
			e := fromWire(wire)
			events = append(events, ports.QueuedEvents{MessageID: messageID, Event: e})
		}
	}

	return events, nil
}

func (q Client) Ack(ctx context.Context, group string, messageID string) error {
	err := q.client.XAck(ctx, IngressStream, group, messageID).Err()
	if err != nil {
		return fmt.Errorf("ack message %s (group=%s): %w", messageID, group, err)
	}

	return nil
}

func (q Client) ReclaimStale(ctx context.Context, group string, consumer string, minIdle time.Duration) ([]ports.QueuedEvents, error) {

	//XCLAIM requires you to already know the specific message IDs you want to take over,
	// whereas XAUTOCLAIM automatically scans for and claims idle messages for you in a single step.

	//if there are more pending-and-stale entries than your Count limit,
	// Redis processes them in batches and gives you back a cursor (start) indicating where to resume from on your next call
	// Since we are running reclaim stale every 5 second we can ignore start for simplification so replace start with _
	body, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   IngressStream,
		Group:    group,
		Consumer: consumer,
		MinIdle:  minIdle,
		Start:    "0", //start at the begining of PEL
		Count:    10,  //Max messages to claim in this call
	}).Result()

	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reclaim stale (group=%s, consumer=%s): %w", group, consumer, err)
	}

	var events []ports.QueuedEvents
	for _, msg := range body {
		var wire wireEvent
		messageID := msg.ID
		str, ok := msg.Values["event"].(string)
		if !ok {
			log.Printf("skipping message %s: missing or invalid 'event' field", messageID)
			continue
		}
		data := []byte(str)
		err = json.Unmarshal(data, &wire)
		if err != nil {
			log.Printf("skipping malformed message %s: %v", messageID, err)
			continue
		}

		event := fromWire(wire)
		events = append(events, ports.QueuedEvents{MessageID: messageID, Event: event})
	}

	return events, nil
}

func (q Client) DeadLetter(ctx context.Context, event domain.Event) error {
	wire := toWire(event)

	body, err := json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("marshal event %s for dead-letter: %w", event.ID, err)
	}

	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: DlqStream,
		Values: map[string]interface{}{"event": body},
	}).Err()
}
