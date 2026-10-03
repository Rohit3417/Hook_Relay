package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Endpoint struct {
	URL    string
	Secret string
}

func NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=require",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pool connection failed : %w", err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("ping Failed : %w", err)
	}

	return pool, nil
}

func GetEndpoint(ctx context.Context, pool *pgxpool.Pool, endpointID string) (Endpoint, error) {

	var temp Endpoint
	err := pool.QueryRow(ctx, "SELECT url, secret from endpoints where id = $1", endpointID).Scan(&temp.URL, &temp.Secret)

	if err == pgx.ErrNoRows {
		return Endpoint{}, fmt.Errorf("endpoint %s not found %w", endpointID, err)
	}
	if err != nil {
		return Endpoint{}, fmt.Errorf("lookup endpoint %s: %w", endpointID, err)
	}

	return temp, nil

}
