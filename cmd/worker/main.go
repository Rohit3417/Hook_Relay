package main

import (
	"Hook_Relay2/internal/adapters/circuitBreaker"
	"Hook_Relay2/internal/adapters/redisqueue"
	"Hook_Relay2/internal/adapters/signer"
	"Hook_Relay2/internal/adapters/ssrf"
	"Hook_Relay2/internal/adapters/storage"
	"Hook_Relay2/internal/core/domain"
	"Hook_Relay2/internal/core/ports"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

const (
	consumerGroup    = "dispatcher_group"
	batchSize        = 10
	failureThreshold = 50
	cooldown         = 100 * time.Second
)

// Helper function
func deadLetterAndack(ctx context.Context, queue redisqueue.Client, group string, event ports.QueuedEvents) {
	err2 := queue.DeadLetter(ctx, event.Event)
	if err2 != nil {
		log.Printf("Failed to dead-letter event %s: %v", event.Event.ID, err2)
		return // Return without ACKing so ReclaimStale can try the DLQ route again
	}
	ackErr := queue.Ack(ctx, group, event.MessageID)
	if ackErr != nil {
		log.Printf("Failed to ack after dead-lettering %s: %v", event.MessageID, ackErr)
	}

}

// Helper function
func recordFailureAndMaybeDeadLetter(ctx context.Context, queue redisqueue.Client, group string, event ports.QueuedEvents, breaker *circuitBreaker.Breaker, currentAttempt int) {
	breaker.RecordResult(event.Event.EndpointID, false)
	if currentAttempt >= domain.MaxDeliveryAttempts {
		event.Event.Status = domain.StatusDead
		deadLetterAndack(ctx, queue, group, event)
	}
}

func handle(ctx context.Context, queue redisqueue.Client, group string, event ports.QueuedEvents, secureClient *http.Client, breaker *circuitBreaker.Breaker, pool *pgxpool.Pool) {

	// The failure condition or the condition for which we increase attempts count :
	// Without an actual outbound HTTP call to a destination server,
	// nothing in your system can genuinely fail during delivery — there's no destination to reject the request,
	// no network error, no timeout.

	// Endpoint contains the url and secret to create Http request and IsActive state to check
	Endpoint, err := storage.GetEndpoint(ctx, pool, event.Event.EndpointID)
	if errors.Is(err, pgx.ErrNoRows) {
		event.Event.Status = domain.StatusDead
		log.Printf("Endpoint %s permanently missing, sending to DLQ", event.Event.EndpointID)
		deadLetterAndack(ctx, queue, group, event)
		return
	} else if err != nil {
		log.Printf("Database error fetching endpoint %s, waiting for retry: %v", event.Event.EndpointID, err)
		return
	}

	if !Endpoint.IsActive {
		log.Println("endpoint is not active")
		return
	}

	attemptCount, err := storage.CountAttempts(ctx, pool, event.Event.ID)
	attemptCount++
	if err != nil {
		log.Println(err)
		return
	}

	// Checking the state of server
	if !breaker.Allow(event.Event.EndpointID) {
		log.Println("currently in opened state")
		return
	}

	//Signing function use
	signedValue := signer.Sign(event.Event.Payload, Endpoint.Secret)
	bodyReader := bytes.NewReader(event.Event.Payload)
	req, reqErr := http.NewRequestWithContext(ctx, "POST", Endpoint.URL, bodyReader)
	if reqErr != nil {
		log.Printf("Failed to create HTTP request for event %s: %v", event.Event.ID, reqErr)
		return
	}

	// Setting custom header for security
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("WebHook-Signature", signedValue)

	start := time.Now()
	// use secureClient to execute
	response, reqErr := secureClient.Do(req)
	latency := time.Since(start)
	// Building the Delivery attempt struct
	var deliveryInfo domain.DeliveryAttempt
	deliveryInfo.EventID = event.Event.ID
	deliveryInfo.EndpointID = event.Event.EndpointID
	deliveryInfo.TenantID = Endpoint.TenantID
	deliveryInfo.AttemptNo = attemptCount
	deliveryInfo.Duration = latency
	if reqErr != nil {
		deliveryInfo.ErrMsg = err.Error()
		deliveryInfo.Success = false
		deliveryInfo.HTTPStatus = 0
		_ = storage.RecordAttempt(ctx, pool, deliveryInfo)
		log.Printf("delivery request failed for event %s: %v", event.Event.ID, err)
		recordFailureAndMaybeDeadLetter(ctx, queue, group, event, breaker, attemptCount)
		return
	}
	defer response.Body.Close() // To avoid leakage of TCP connection
	status := response.StatusCode

	deliveryInfo.HTTPStatus = status
	switch status / 100 {
	case 2:
		deliveryInfo.Success = true
		deliveryInfo.ErrMsg = ""
	case 4:
		deliveryInfo.Success = false
		deliveryInfo.ErrMsg = "a short message, e.g. “HTTP 400 Bad Request”"
	case 5:
		deliveryInfo.Success = false
		deliveryInfo.ErrMsg = "a short message, e.g. “HTTP 503 Service Unavailable”"
	default:
		deliveryInfo.Success = false
		deliveryInfo.ErrMsg = "a short message, e.g. “unexpected HTTP 302”"
	}

	// Once recording
	err = storage.RecordAttempt(ctx, pool, deliveryInfo)
	if err != nil {
		log.Printf("failed to record attempt for event %s: %v", event.Event.ID, err)
	}
	switch status / 100 {
	case 4:
		//the endpoint responded, so it's reachable — the payload itself is what's rejected,
		// not the destination being down) and immediately dead-lettering (retrying won't fix a malformed request)
		event.Event.Status = domain.StatusDead
		event.Event.Attempts = attemptCount
		breaker.RecordResult(event.Event.EndpointID, true)
		deadLetterAndack(ctx, queue, group, event)
		return
	case 5:
		recordFailureAndMaybeDeadLetter(ctx, queue, group, event, breaker, attemptCount)
		return

	case 2:
		breaker.RecordResult(event.Event.EndpointID, true)
		ackErr := queue.Ack(ctx, group, event.MessageID)
		if ackErr != nil {
			log.Printf("Failed to ack after dead-lettering %s: %v", event.MessageID, ackErr)
			return
		}
		log.Printf("delivered event %s to endpoint %s (status %d)", event.Event.ID, event.Event.EndpointID, status)
		return

	default: // Case of 1xx or 3xx (1xx is They notify the client that the initial part of an HTTP request has been received and that processing is continuing, 3xx )
		log.Printf("unexpected status %d for event %s", status, event.Event.ID)
		recordFailureAndMaybeDeadLetter(ctx, queue, group, event, breaker, attemptCount)
		return
	}

}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("No .env found")
	}

	startupCtx := context.Background()

	pool, err := storage.NewPool(startupCtx)
	if err != nil {
		log.Fatalf("%v", err)
		return
	}
	defer pool.Close()

	client, err := redisqueue.NewRedisConnection(startupCtx)
	if err != nil {
		log.Fatalf("%v", err)
		return
	}
	defer client.Close()

	secureClient := ssrf.NewSecureClient()
	// secureClient := &http.Client{Timeout: 10 * time.Second} // for testing local case as ssrf will block my requests only
	breaker := circuitBreaker.New(failureThreshold, cooldown)

	log.Println("worker started")
	consumerName := os.Getenv("WORKER_NAME")
	if consumerName == "" {
		consumerName = fmt.Sprintf("worker-%d", os.Getpid())
	}

	queue := redisqueue.NewClient(client)
	if err := queue.EnsureGroup(startupCtx, consumerGroup); err != nil {
		log.Println(err)
		return
	}

	//We cannot set timeout for this ctx as we require it in ReadPending function which runs forever
	loopCtx := context.Background()

	go func() {
		for {
			// only reclaim things that have been stuck for a genuinely long time (30s)
			events, err := queue.ReclaimStale(loopCtx, consumerGroup, consumerName, 30*time.Second)
			if err != nil {
				log.Printf("error occured: %v\n", err)
				time.Sleep(1 * time.Second)
				continue
			}

			for _, event := range events {
				handle(loopCtx, queue, consumerGroup, event, secureClient, breaker, pool)
			}

			// Check every 5 second for stale events
			time.Sleep(5 * time.Second)
		}
	}()

	for {

		// The ReadPending auto takes 2 second before calling on again
		//This controls how long one blocking read waits before returning empty
		events, err := queue.ReadPending(loopCtx, consumerGroup, consumerName, batchSize)
		if err != nil {
			log.Printf("read pending failed: %v", err)

			time.Sleep(1 * time.Second)
			continue
		}

		for _, event := range events {
			handle(loopCtx, queue, consumerGroup, event, secureClient, breaker, pool)
		}

	}

}
