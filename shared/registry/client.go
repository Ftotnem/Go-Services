package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	// You'll need to import the redis client package from the go-redis library here
	// because RegistryClient uses redis.ClusterClient type.
	"github.com/redis/go-redis/v9"
)

// GetActiveServices is now part of a separate client to read the registry,
// making the ServiceRegistrar purely for self-registration.
// This allows other services (like Gate-Proxy) to query the registry.
type RegistryClient struct {
	redisClient    *redis.ClusterClient // This type comes from github.com/redis/go-redis/v9
	serviceTimeout time.Duration
}

// NewRegistryClient takes an already initialized *redis.ClusterClient.
func NewRegistryClient(redisClient *redis.ClusterClient, serviceTimeout time.Duration) *RegistryClient {
	return &RegistryClient{
		redisClient:    redisClient,
		serviceTimeout: serviceTimeout,
	}
}

// GetActiveServices retrieves a map of active service instances for a given service type.
// The map key is the instance ID, and the value is the ServiceInfo.
// This function filters out services whose LastSeen timestamp is older than the ServiceTimeout.
func (rc *RegistryClient) GetActiveServices(ctx context.Context, serviceType string) (map[string]ServiceInfo, error) {
	key := fmt.Sprintf("%s%s", RedisRegistryHashPrefix, serviceType) // Use the same prefix as registrar
	results, err := rc.redisClient.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get all services of type %s from Redis: %w", serviceType, err)
	}

	activeServices := make(map[string]ServiceInfo)
	currentTime := time.Now()

	for instanceID, infoJSON := range results {
		var info ServiceInfo
		if err := json.Unmarshal([]byte(infoJSON), &info); err != nil {
			log.Printf("WARNING: RegistryClient: Failed to unmarshal ServiceInfo for ID %s (type %s): %v", instanceID, serviceType, err)
			continue // Skip malformed entries, they'll be cleaned up by cleanupLoop in registrar
		}
		lastSeenTime := time.UnixMilli(info.LastSeen)
		// Consider service active if its last heartbeat was within the ServiceTimeout
		if currentTime.Sub(lastSeenTime) <= rc.serviceTimeout {
			activeServices[instanceID] = info
		}
	}
	return activeServices, nil
}
