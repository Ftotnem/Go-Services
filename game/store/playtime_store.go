// game/store/playtime_store.go
package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis" // Correct alias for shared Redis constants
	"github.com/redis/go-redis/v9"
)

// PlayerPlaytimeStore manages player playtime and delta playtime data exclusively in Redis.
// It acts as a fast, in-memory cache for game session data before it's potentially
// synchronized with a persistent Player microservice.
type PlayerPlaytimeStore struct {
	redisClient *redis.ClusterClient
}

// NewPlayerPlaytimeStore creates a new instance of PlayerPlaytimeStore.
// It requires a connected Redis Cluster client for all operations.
func NewPlayerPlaytimeStore(redisClient *redis.ClusterClient) *PlayerPlaytimeStore {
	return &PlayerPlaytimeStore{
		redisClient: redisClient,
	}
}

// SetPlayerPlaytime sets a player's total accumulated playtime in Redis.
// This is typically used when loading a player's profile or after a major sync.
func (pps *PlayerPlaytimeStore) SetPlayerPlaytime(ctx context.Context, playerUUID string, totalPlaytime float64) error {
	// A TTL for total playtime keys can be useful for caching or eventual consistency cleanup.
	// Adjust this duration based on how often you expect to synchronize with persistent storage.
	playtimeTTL := 6 * time.Hour

	// Construct the Redis key using the predefined constant.
	key := fmt.Sprintf(redisu.PlaytimeKeyPrefix, playerUUID)
	err := pps.redisClient.Set(ctx, key, totalPlaytime, playtimeTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to set total playtime for player %s in Redis: %w", playerUUID, err)
	}

	log.Printf("Successfully set total playtime for player %s to %.2f seconds (TTL: %s).", playerUUID, totalPlaytime, playtimeTTL)
	return nil
}

// GetPlayerPlaytime retrieves a player's current total playtime from Redis.
// Returns 0.0 and nil if the key does not exist (player has no recorded playtime yet).
func (pps *PlayerPlaytimeStore) GetPlayerPlaytime(ctx context.Context, playerUUID string) (float64, error) {
	// Construct the Redis key using the predefined constant.
	key := fmt.Sprintf(redisu.PlaytimeKeyPrefix, playerUUID)

	val, err := pps.redisClient.Get(ctx, key).Float64()
	if err == redis.Nil {
		return 0.0, nil // Player has no recorded playtime yet, or key expired.
	}
	if err != nil {
		return 0.0, fmt.Errorf("failed to retrieve total playtime for player %s from Redis: %w", playerUUID, err)
	}

	return val, nil
}

// IncrementPlayerPlaytime atomically increments a player's total playtime
// and their associated team's total playtime in Redis.
// It uses the `deltaPlaytime` stored under `DeltaPlaytimeKeyPrefix` and CONSUMES it (clears it after use).
func (pps *PlayerPlaytimeStore) IncrementPlayerPlaytime(ctx context.Context, playerUUID string) error {
	// Use the correct package alias for constants when constructing keys.
	deltaKey := fmt.Sprintf(redisu.DeltaPlaytimeKeyPrefix, playerUUID)
	totalPlaytimeKey := fmt.Sprintf(redisu.PlaytimeKeyPrefix, playerUUID)
	playerTeamKey := fmt.Sprintf(redisu.PlayerTeamKeyPrefix, playerUUID) // Key to get player's team ID

	// 1. Fetch the delta playtime value.
	deltaStr, err := pps.redisClient.Get(ctx, deltaKey).Result()
	if err == redis.Nil {
		// No delta playtime found for this player. This is a normal scenario if no recent activity.
		log.Printf("INFO: No delta playtime found for player %s. Skipping playtime increment.", playerUUID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get delta playtime for player %s from Redis: %w", playerUUID, err)
	}

	// 2. Parse the delta playtime string to a float64.
	deltaFloat, err := strconv.ParseFloat(deltaStr, 64)
	if err != nil {
		// Log and return an error if the stored delta value is malformed.
		return fmt.Errorf("failed to parse delta playtime value '%s' for player %s as float: %w", deltaStr, playerUUID, err)
	}

	if deltaFloat <= 0 {
		// If the delta is zero or negative, there's nothing to add.
		// We still log this, but don't perform increments. We should still consume the delta.
		log.Printf("INFO: Delta playtime for player %s is %.2f (non-positive). Consuming delta without increment.", playerUUID, deltaFloat)

		// Clear the delta even if it's non-positive to prevent repeated processing
		err = pps.redisClient.Del(ctx, deltaKey).Err()
		if err != nil {
			log.Printf("WARNING: Failed to clear non-positive delta for player %s: %v", playerUUID, err)
		}
		return nil
	}

	// 3. Get the team ID for the player. This is needed to update team totals.
	teamID, err := pps.redisClient.Get(ctx, playerTeamKey).Result()
	if err == redis.Nil {
		// If no team ID is found, log a warning but proceed with player playtime increment.
		log.Printf("WARNING: Team ID key %s not found for player %s. Player playtime will be incremented, but team playtime will not be updated.", playerTeamKey, playerUUID)

		// Execute player playtime increment atomicall
		pipe := pps.redisClient.Pipeline()
		playerIncrCmd := pipe.IncrByFloat(ctx, totalPlaytimeKey, deltaFloat)

		_, err := pipe.Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to execute player playtime increment for player %s (no team found): %w", playerUUID, err)
		}
		if playerIncrCmd.Err() != nil {
			return fmt.Errorf("player total playtime increment failed for player %s (no team found): %w", playerUUID, playerIncrCmd.Err())
		}
		log.Printf("Incremented total playtime for player %s by %.2f. New total: %.2f. Delta consumed.", playerUUID, deltaFloat, playerIncrCmd.Val())
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to retrieve team ID for player %s from Redis: %w", playerUUID, err)
	}

	// Construct the Redis key for the team's total playtime.
	teamTotalPlaytimeKey := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID)

	// 4. Use a Redis Pipeline for atomic execution of all operations.
	// This ensures that either all increments succeed, or none do.
	// The delta key IS deleted here to consume it after use.
	pipe := pps.redisClient.Pipeline()
	playerIncrCmd := pipe.IncrByFloat(ctx, totalPlaytimeKey, deltaFloat)   // Increment player's total playtime
	teamIncrCmd := pipe.IncrByFloat(ctx, teamTotalPlaytimeKey, deltaFloat) // Increment team's total playtime
	_, err = pipe.Exec(ctx)                                                // Execute the pipeline
	if err != nil {
		return fmt.Errorf("failed to execute playtime increments pipeline for player %s (team %s): %w", playerUUID, teamID, err)
	}

	// Check individual command errors within the pipeline for more granular reporting.
	if playerIncrCmd.Err() != nil {
		return fmt.Errorf("player total playtime increment failed for player %s: %w", playerUUID, playerIncrCmd.Err())
	}
	if teamIncrCmd.Err() != nil {
		return fmt.Errorf("team total playtime increment failed for team %s: %w", teamID, teamIncrCmd.Err())
	}

	log.Printf("Successfully incremented total playtime for player %s by %.2f (new player total: %.2f) and team %s by %.2f (new team total: %.2f). Delta consumed.",
		playerUUID, deltaFloat, playerIncrCmd.Val(), teamID, deltaFloat, teamIncrCmd.Val())

	return nil
}

// GetAllPlayerPlaytimes retrieves all current player total playtime data from Redis.
// This operation can be resource-intensive in large clusters.
func (pps *PlayerPlaytimeStore) GetAllPlayerPlaytimes(ctx context.Context) (map[string]float64, error) {
	playtimes := make(map[string]float64)
	var mu sync.Mutex // Protects map writes from concurrent goroutines across cluster nodes.

	// Construct the SCAN pattern using the constant, replacing the UUID placeholder with a wildcard.
	scanPattern := fmt.Sprintf(redisu.PlaytimeKeyPrefix, "*")

	// Iterate over all master nodes in the Redis Cluster to collect data.
	err := pps.redisClient.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		if client == nil {
			log.Printf("Warning: Redis Cluster ForEachMaster provided a nil client, skipping node.")
			return nil
		}

		iter := client.Scan(ctx, 0, scanPattern, 0).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()

			// Extract the player UUID from the key (e.g., "playtime:{uuid}:" -> "uuid").
			startIdx := strings.Index(key, "{")
			endIdx := strings.Index(key, "}")
			if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
				log.Printf("Warning: Could not parse UUID from malformed playtime key: %s. Skipping.", key)
				continue
			}
			playerUUID := key[startIdx+1 : endIdx] // Extract the UUID without braces

			// Retrieve the playtime value.
			val, err := client.Get(ctx, key).Float64()
			if err != nil {
				log.Printf("Warning: Failed to get playtime for player %s (key: %s) from Redis: %v. Skipping.", playerUUID, key, err)
				continue
			}

			// Safely add the player's playtime to the shared map.
			mu.Lock()
			playtimes[playerUUID] = val
			mu.Unlock()
		}
		return iter.Err() // Return any error from the iterator.
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan all player playtime data from Redis cluster: %w", err)
	}

	return playtimes, nil
}

// SetPlayerDeltaPlaytime stores the latest calculated delta playtime for a player.
// This delta represents the playtime accumulated in the current session since the last update.
func (pps *PlayerPlaytimeStore) SetPlayerDeltaPlaytime(ctx context.Context, playerUUID string, deltaPlaytime float64) error {
	key := fmt.Sprintf(redisu.DeltaPlaytimeKeyPrefix, playerUUID)

	// Set a reasonable TTL for delta playtime. This ensures that old deltas are cleaned up
	// if they are not processed for some reason (e.g., service crash before processing).
	deltaTTL := 24 * time.Hour // Sufficiently long for pending processing.
	err := pps.redisClient.Set(ctx, key, deltaPlaytime, deltaTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to set delta playtime for player %s in Redis: %w", playerUUID, err)
	}

	log.Printf("Delta playtime set for player %s: %.2f seconds (TTL: %s).", playerUUID, deltaPlaytime, deltaTTL)
	return nil
}

// GetPlayerDeltaPlaytime retrieves a player's pending delta playtime from Redis.
// Returns an error if no delta is found.
func (pps *PlayerPlaytimeStore) GetPlayerDeltaPlaytime(ctx context.Context, playerUUID string) (float64, error) {
	key := fmt.Sprintf(redisu.DeltaPlaytimeKeyPrefix, playerUUID)

	val, err := pps.redisClient.Get(ctx, key).Float64()
	if err == redis.Nil {
		// Return a specific error when no delta playtime is found.
		return 0.0, fmt.Errorf("no delta playtime found for player %s: %w", playerUUID, redisu.ErrRedisKeyNotFound)
	}
	if err != nil {
		return 0.0, fmt.Errorf("failed to retrieve delta playtime for player %s from Redis: %w", playerUUID, err)
	}

	return val, nil
}

// SetPlayerTeam assigns a player to a specific team in Redis.
// The team assignment typically doesn't expire unless the player is removed from the team.
func (pps *PlayerPlaytimeStore) SetPlayerTeam(ctx context.Context, playerUUID string, teamID string) error {
	key := fmt.Sprintf(redisu.PlayerTeamKeyPrefix, playerUUID)
	// Set with no expiration (0 duration) as team assignment is usually persistent.
	err := pps.redisClient.Set(ctx, key, teamID, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to set team ID for player %s in Redis: %w", playerUUID, err)
	}
	log.Printf("Player %s assigned to team %s.", playerUUID, teamID)
	return nil
}
