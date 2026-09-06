// Throwaway test — NOT part of the real app.
// Verifies that EnsureGroup's "already exists" handling actually works,
// by calling it twice against a real Redis and checking the second call
// returns nil, not an error.
//
// Run with: go run ensuregroup_check.go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

const IngressStream = "stream:events:ingress"

// Client mirrors your redisqueue.Client — copied here standalone so this
// test has zero dependency on your actual module/import path.
type Client struct {
	client *redis.Client
}

func NewClient(c *redis.Client) Client {
	return Client{client: c}
}

// EnsureGroup — paste your real implementation here to test the exact
// code you're shipping, not a reimplementation of it.
func (q Client) EnsureGroup(ctx context.Context, group string) error {
	err := q.client.XGroupCreateMkStream(ctx, IngressStream, group, "0").Err()
	if err != nil {
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			return nil
		}
		return fmt.Errorf("ensure group : %w", err)
	}
	return nil
}

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis not reachable: %v", err)
	}

	// Clean slate so this test is repeatable regardless of prior runs.
	rdb.Del(ctx, IngressStream)

	q := NewClient(rdb)
	const group = "test_group"

	fmt.Println("=== Call 1: group does not exist yet ===")
	if err := q.EnsureGroup(ctx, group); err != nil {
		log.Fatalf("FAIL: first call should succeed, got error: %v", err)
	}
	fmt.Println("  -> nil error, as expected (group created)")

	fmt.Println("\n=== Call 2: group already exists ===")
	err := q.EnsureGroup(ctx, group)
	if err != nil {
		fmt.Printf("  -> FAIL: expected nil, got error: %v\n", err)
		fmt.Println("     This means your \"already exists\" string check did NOT match.")
		fmt.Println("     Print the raw error text below to see what go-redis actually returned:")
		rawErr := rdb.XGroupCreateMkStream(ctx, IngressStream, group, "0").Err()
		fmt.Printf("     raw error: %q\n", rawErr.Error())
		return
	}
	fmt.Println("  -> nil error, as expected (already-exists case correctly handled)")

	fmt.Println("\n=== PASS: EnsureGroup is idempotent ===")
}
