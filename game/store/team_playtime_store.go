// game/store/team_playtime_store.go
package store

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis" // Correct alias for shared Redis constants
	"github.com/redis/go-redis/v9"
)

// TeamPlaytimeStore manages team playtime data exclusively in Redis.
// This store is responsible for maintaining a team's aggregated playtime
// in Redis, which can be used for real-time leaderboards or later synchronized
// with a persistent Team Stats microservice.
type TeamPlaytimeStore struct {
	redisClient *redis.ClusterClient
}

// NewTeamPlaytimeStore creates a new TeamPlaytimeStore instance.
// It requires a connected Redis Cluster client.
func NewTeamPlaytimeStore(redisClient *redis.ClusterClient) *TeamPlaytimeStore {
	return &TeamPlaytimeStore{
		redisClient: redisClient,
	}
}

// SetTeamPlaytime sets a team's total accumulated playtime in Redis.
// This is typically used to initialize a team's playtime or to overwrite it
// (e.g., after loading from a persistent store or a manual adjustment).
func (tps *TeamPlaytimeStore) SetTeamPlaytime(ctx context.Context, teamID string, totalPlaytime float64) error {
	// Construct the Redis key using the predefined constant.
	key := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID)

	// Set the team's total playtime. A TTL of 0 means the key will not expire automatically.
	// This implies that team playtime is considered persistent in Redis until explicitly deleted,
	// or until a periodic sync mechanism updates it from a long-term store.
	err := tps.redisClient.Set(ctx, key, totalPlaytime, 0).Err() // 0 duration for no expiration
	if err != nil {
		return fmt.Errorf("failed to set total playtime for team %s in Redis: %w", teamID, err)
	}

	log.Printf("Successfully set total playtime for team %s to %.2f seconds in Redis.", teamID, totalPlaytime)
	return nil
}

// GetTeamPlaytime retrieves a team's current total playtime from Redis.
// Returns 0.0 and nil if the key does not exist (team has no recorded playtime yet).
func (tps *TeamPlaytimeStore) GetTeamPlaytime(ctx context.Context, teamID string) (float64, error) {
	// Construct the Redis key using the predefined constant.
	key := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID)

	val, err := tps.redisClient.Get(ctx, key).Float64()
	if err == redis.Nil {
		// If the key doesn't exist, it means the team has 0 playtime in the current session/cache.
		return 0.0, nil
	}
	if err != nil {
		return 0.0, fmt.Errorf("failed to retrieve total playtime for team %s from Redis: %w", teamID, err)
	}

	return val, nil
}

// IncrementTeamPlaytime atomically increments a team's total playtime in Redis.
// This is the primary method for updating team playtime during gameplay,
// typically called when a player from that team logs off and their session playtime is calculated.
func (tps *TeamPlaytimeStore) IncrementTeamPlaytime(ctx context.Context, teamID string, additionalPlaytime float64) error {
	// Set a reasonable TTL for in-session playtime keys. This acts as a fallback
	// if the team playtime is not regularly updated or persisted elsewhere.
	// This TTL applies to the key itself, ensuring it doesn't stay forever if not touched.
	playtimeTTL := 6 * time.Hour

	// Construct the Redis key using the predefined constant.
	key := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID)

	// Use IncrByFloat to atomically increment the playtime.
	// This command is safe for concurrent updates.
	currentPlaytime, err := tps.redisClient.IncrByFloat(ctx, key, additionalPlaytime).Result()
	if err != nil {
		return fmt.Errorf("failed to increment playtime for team %s in Redis: %w", teamID, err)
	}

	// After incrementing, refresh the TTL for the key. This ensures that active teams'
	// playtime keys don't expire prematurely if the session is long.
	err = tps.redisClient.Expire(ctx, key, playtimeTTL).Err()
	if err != nil {
		log.Printf("Warning: Failed to refresh TTL for team %s playtime key in Redis: %v", teamID, err)
		// Do not return an error here, as the increment itself was successful.
		// This warning indicates a potential caching issue, not a data integrity one.
	}

	log.Printf("Successfully incremented playtime for team %s by %.2f seconds. New total: %.2f.", teamID, additionalPlaytime, currentPlaytime)
	return nil
}

// DeleteTeamPlaytime removes a team's playtime record from Redis.
// This might be used when a team is disbanded, a game session explicitly ends for a team,
// or during cleanup operations.
func (tps *TeamPlaytimeStore) DeleteTeamPlaytime(ctx context.Context, teamID string) error {
	// Construct the Redis key using the predefined constant.
	key := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID)
	deletedCount, err := tps.redisClient.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to delete playtime record for team %s from Redis: %w", teamID, err)
	}

	if deletedCount > 0 {
		log.Printf("Successfully deleted playtime record for team %s from Redis.", teamID)
	} else {
		log.Printf("No playtime record found for team %s in Redis to delete.", teamID)
	}
	return nil
}

// GetAllTeamPlaytimes retrieves all current team playtime data from Redis.
// This is typically used for periodic synchronization to a persistent Team Stats Microservice
// or for generating comprehensive leaderboards.
// In a Redis Cluster, this operation involves scanning across all master nodes.
func (tps *TeamPlaytimeStore) GetAllTeamPlaytimes(ctx context.Context) (map[string]float64, error) {
	teamPlaytimes := make(map[string]float64)
	var mu sync.Mutex // Protects the 'teamPlaytimes' map during concurrent writes from different cluster nodes.

	// Construct the SCAN pattern using the constant, replacing the teamID placeholder with a wildcard.
	scanPattern := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, "*")

	// Use ForEachMaster to iterate over all master nodes in the Redis Cluster.
	// This ensures that you gather all data across all shards.
	err := tps.redisClient.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		if client == nil {
			log.Printf("Warning: Redis Cluster ForEachMaster provided a nil client; skipping node.")
			return nil // Skip this iteration if the client is unexpectedly nil.
		}

		// Use SCAN to iterate through keys on the current master node.
		// The pattern "team_total_playtime:{*}:" ensures we only get keys matching our format.
		iter := client.Scan(ctx, 0, scanPattern, 0).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()

			// Extract the TeamID from the key (e.g., "team_total_playtime:{teamID}:" -> "teamID").
			// This string manipulation assumes the fixed format of `redisu.TeamTotalPlaytimePrefix`.
			startIdx := strings.Index(key, "{")
			endIdx := strings.Index(key, "}")
			if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
				log.Printf("Warning: Could not parse TeamID from malformed team playtime key: %s. Skipping.", key)
				continue
			}
			teamID := key[startIdx+1 : endIdx] // Extract the TeamID without braces

			// Retrieve the playtime value for the found key.
			val, err := client.Get(ctx, key).Float64()
			if err != nil {
				log.Printf("Warning: Failed to get playtime for team %s (key: %s) from Redis: %v. Skipping.", teamID, key, err)
				continue
			}

			// Safely add the team's playtime to the shared map.
			mu.Lock()
			teamPlaytimes[teamID] = val
			mu.Unlock()
		}
		return iter.Err() // Return any error encountered by the iterator.
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan all team playtime data from Redis cluster: %w", err)
	}

	return teamPlaytimes, nil
}
