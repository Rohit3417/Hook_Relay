package dataloaders

import (
	"context"
	"time"

	"Hook_Relay2/internal/adapters/graphql/graph/model"

	"github.com/graph-gophers/dataloader/v7"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 1. Define the context key and struct to hold loaders
type contextKey string

const LoadersKey contextKey = "dataloaders"

type Loaders struct {
	StatsLoader *dataloader.Loader[string, *model.EndpointStats]
}

// GetStatsBatch returns the batch function required by dataloader.NewBatchedLoader
func GetStatsBatch(pool *pgxpool.Pool) dataloader.BatchFunc[string, *model.EndpointStats] {
	return func(ctx context.Context, endpointIDs []string) []*dataloader.Result[*model.EndpointStats] {
		// 1. Initialize the results array to perfectly match the length of requested IDs
		results := make([]*dataloader.Result[*model.EndpointStats], len(endpointIDs))

		// Map for O(1) lookups to place DB results into the correct array index
		idToIndex := make(map[string]int, len(endpointIDs))

		// 2. Pre-fill defaults. If an endpoint has no deliveries, it won't appear in the SQL GROUP BY.
		for i, id := range endpointIDs {
			idToIndex[id] = i
			results[i] = &dataloader.Result[*model.EndpointStats]{
				Data: &model.EndpointStats{
					TotalDeliveries: 0,
					SuccessRate:     0.0,
					AvgLatencyMs:    0.0,
					LastDeliveryAt:  nil, // Nullable in GraphQL
				},
			}
		}

		query := `
			SELECT 
				endpoint_id,
				COUNT(*) as total_deliveries,
				COALESCE(AVG(CASE WHEN success THEN 1.0 ELSE 0.0 END), 0.0) as success_rate,
				COALESCE(AVG(duration_ms), 0.0) as avg_latency_ms,
				MAX(created_at) as last_delivery_at
			FROM delivery_attempts
			WHERE endpoint_id = ANY($1)
			GROUP BY endpoint_id
		`

		rows, err := pool.Query(ctx, query, endpointIDs)
		if err != nil {
			// If the overall query fails, apply the error to every requested endpoint
			for i := range results {
				results[i].Error = err
			}
			return results
		}
		defer rows.Close()

		// 3. Map the aggregated database rows back to their corresponding endpoints
		for rows.Next() {
			var epID string
			var total int
			var rate float64
			var latency float64
			var lastAt time.Time

			if err := rows.Scan(&epID, &total, &rate, &latency, &lastAt); err != nil {
				continue
			}

			if index, ok := idToIndex[epID]; ok {
				results[index].Data = &model.EndpointStats{
					TotalDeliveries: int32(total), // GraphQL Int generates as int32
					SuccessRate:     rate,
					AvgLatencyMs:    latency,
					LastDeliveryAt:  &lastAt,
				}
			}
		}

		return results
	}
}
