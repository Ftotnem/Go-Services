// shared/redis/client.go
package redis

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedisClusterClient creates and returns a new configured Redis Cluster client.
// This function can be used by any service or shared component that needs to
// connect to the Redis Cluster.
func NewRedisClusterClient(addrs []string) (*redis.ClusterClient, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no Redis addresses provided")
	}

	rdb := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:        addrs,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  6 * time.Second,
		PoolSize:     10, // Adjust pool size as needed for your workload
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ping to ensure connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis cluster at %v: %w", addrs, err)
	}
	log.Println("Successfully connected to Redis cluster.")
	return rdb, nil
}
