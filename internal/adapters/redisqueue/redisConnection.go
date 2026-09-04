package redisqueue

import (
	"context"
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"
)

func NewRedisConnection(ctx context.Context) (*redis.Client, error) {
	opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		return nil, fmt.Errorf("invalid redis url")
	}

	client := redis.NewClient(opt)

	err = client.Ping(ctx).Err()
	if err != nil {
		return nil, fmt.Errorf("redis connection failed : %w", err)
	}

	return client, nil
}
