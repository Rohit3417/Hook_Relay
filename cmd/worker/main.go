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

	startupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

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
	for {
		events, err := queue.ReadPending(loopCtx, consumerGroup, consumerName, batchSize)
		if err != nil {
			log.Printf("read pending failed: %v", err)
			time.Sleep(1 * time.Second)
		}

		for _, event := range events {
			handle(loopCtx, queue, consumerGroup, event)
		}
	}

}
