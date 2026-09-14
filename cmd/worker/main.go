package main

import (
	"Hook_Relay2/internal/adapters/redisqueue"
	"Hook_Relay2/internal/adapters/storage"
	"Hook_Relay2/internal/core/ports"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const (
	consumerGroup = "dispatcher_group"
	batchSize     = 10
)

func handle(ctx context.Context, queue redisqueue.Client, group string, event ports.QueuedEvents) {

	// The failure condition or the condition for which we increase attempts count :
	// Without an actual outbound HTTP call to a destination server,
	// nothing in your system can genuinely fail during delivery — there's no destination to reject the request,
	// no network error, no timeout.
	err := queue.Ack(ctx, group, event.MessageID)
	if err != nil {
		log.Printf("error occured in acknowledging: %v\n", err)
		return
	}
	log.Println("Hello")
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
				handle(loopCtx, queue, consumerGroup, event)
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
			handle(loopCtx, queue, consumerGroup, event)
		}

	}

}
