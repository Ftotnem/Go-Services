package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis"
	"github.com/redis/go-redis/v9"
)

// PlayerPlaytimeStore handles player playtime operations exclusively in Redis.
// This store is responsible for maintaining a player's current session playtime
// in Redis, which will later be periodically synchronized with a Player microservice.
type PlayerPlaytimeStore struct {
	redisClient *redis.ClusterClient
}

// NewPlayerPlaytimeStore creates a new PlayerPlaytimeStore instance.
func NewPlayerPlaytimeStore(redisClient *redis.ClusterClient) *PlayerPlaytimeStore {
	return &PlayerPlaytimeStore{
		redisClient: redisClient,
	}
}

// SetPlayerPlaytime sets a player's current total playtime in Redis.
func (pps *PlayerPlaytimeStore) SetPlayerPlaytime(ctx context.Context, playerUUID string, totalPlaytime float64) error {
	playtimeTTL := 6 * time.Hour // Example TTL, adjust as needed

	key := fmt.Sprintf(redisu.PlaytimeKeyPrefix, playerUUID) // <-- Using the constant here
	err := pps.redisClient.Set(ctx, key, totalPlaytime, playtimeTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to set playtime for %s in Redis: %w", playerUUID, err)
	}

	log.Printf("Set playtime for player %s in Redis: %.2f seconds", playerUUID, totalPlaytime)
	return nil
}

// GetPlayerPlaytime retrieves a player's current total playtime from Redis.
func (pps *PlayerPlaytimeStore) GetPlayerPlaytime(ctx context.Context, playerUUID string) (float64, error) {
	key := fmt.Sprintf(redisu.PlaytimeKeyPrefix, playerUUID) // <-- Using the constant here

	val, err := pps.redisClient.Get(ctx, key).Float64()
	if err == redis.Nil {
		return 0.0, nil
	}
	if err != nil {
		return 0.0, fmt.Errorf("failed to get playtime for %s from Redis: %w", playerUUID, err)
	}

	return val, nil
}

// IncrementPlayerPlaytime increments a player's total playtime and their team's total playtime in Redis.
// It consumes the delta playtime stored in Redis under DeltaPlaytimeKeyPrefix and then clears it.
func (pps *PlayerPlaytimeStore) IncrementPlayerPlaytime(ctx context.Context, uuid string) error {
	// Use the correct package alias for constants
	deltaKey := fmt.Sprintf(redisu.DeltaPlaytimeKeyPrefix, uuid)
	totalPlaytimeKey := fmt.Sprintf(redisu.PlaytimeKeyPrefix, uuid)
	playerTeamKey := fmt.Sprintf(redisu.PlayerTeamKeyPrefix, uuid)

	// 1. Get the delta value from Redis.
	deltaStr, err := pps.redisClient.Get(ctx, deltaKey).Result()
	if err == redis.Nil {
		// No delta playtime found for this player, nothing to increment.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get delta playtime for %s: %w", uuid, err)
	}

	// 2. Convert the string delta value to a float64.
	deltaFloat, err := strconv.ParseFloat(deltaStr, 64)
	if err != nil {
		return fmt.Errorf("failed to parse delta playtime '%s' for %s as float: %w", deltaStr, uuid, err)
	}

	if deltaFloat <= 0 {
		// If delta is 0 or negative, don't increment. Just clear the key.
		log.Printf("INFO: Delta playtime for %s is %.2f (<=0). Clearing delta key.", uuid, deltaFloat)
		_, err := pps.redisClient.Del(ctx, deltaKey).Result()
		if err != nil {
			log.Printf("WARNING: Failed to delete delta playtime key %s for %s: %v", deltaKey, uuid, err)
		}
		return nil
	}

	// 3. Get the team ID for the player.
	teamID, err := pps.redisClient.Get(ctx, playerTeamKey).Result()
	if err == redis.Nil {
		log.Printf("WARN: Team ID key %s not found for player %s. Cannot increment team playtime. Player playtime will still be incremented.", playerTeamKey, uuid)
		// If no team, we still want to increment player playtime.
		// Use a simple IncrByFloat for player, and then delete the delta key.
		pipe := pps.redisClient.Pipeline()
		playerIncr := pipe.IncrByFloat(ctx, totalPlaytimeKey, deltaFloat)
		pipe.Del(ctx, deltaKey) // Clear the delta key

		_, err := pipe.Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to execute player playtime increment and delta delete for %s: %w", uuid, err)
		}
		if playerIncr.Err() != nil {
			return fmt.Errorf("player playtime increment failed for %s (no team found): %w", uuid, playerIncr.Err())
		}
		log.Printf("Incremented playtime for player %s by %.2f (no team update). New total: %.2f", uuid, deltaFloat, playerIncr.Val())
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get team ID for player %s: %w", uuid, err)
	}

	teamTotalPlaytimeKey := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID)

	// 4. Use a Redis Pipeline for atomic execution of both increments and delta key deletion.
	pipe := pps.redisClient.Pipeline()
	playerIncr := pipe.IncrByFloat(ctx, totalPlaytimeKey, deltaFloat)
	teamIncr := pipe.IncrByFloat(ctx, teamTotalPlaytimeKey, deltaFloat)
	pipe.Del(ctx, deltaKey) // Crucial: Delete the delta key after applying its value

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute playtime increments in pipeline for %s (team %s): %w", uuid, teamID, err)
	}

	// Although pipe.Exec aggregates, checking individual command errors can provide more specific context.
	if playerIncr.Err() != nil {
		return fmt.Errorf("player playtime increment failed for %s: %w", uuid, playerIncr.Err())
	}
	if teamIncr.Err() != nil {
		return fmt.Errorf("team playtime increment failed for team %s: %w", teamID, teamIncr.Err())
	}

	log.Printf("Incremented playtime for player %s by %.2f. New total for player: %.2f. New total for team %s: %.2f",
		uuid, deltaFloat, playerIncr.Val(), teamID, teamIncr.Val())

	return nil
}

// GetAllPlayerPlaytimes retrieves all current player playtime data from Redis.
func (pps *PlayerPlaytimeStore) GetAllPlayerPlaytimes(ctx context.Context) (map[string]float64, error) {
	playtimes := make(map[string]float64)
	var mu sync.Mutex

	// Construct the scan pattern using the constant
	scanPattern := strings.Replace(redisu.PlaytimeKeyPrefix, "%s", "*", 1) // <-- Using the constant here

	err := pps.redisClient.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		if client == nil {
			log.Printf("Warning: ForEachMaster provided nil client, skipping node")
			return nil
		}

		iter := client.Scan(ctx, 0, scanPattern, 0).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()

			// Extract UUID from key: "playtime:{uuid}:"
			start := strings.Index(key, "{")
			end := strings.Index(key, "}")
			if start == -1 || end == -1 || end <= start {
				log.Printf("Warning: Could not parse UUID from playtime key: %s", key)
				continue
			}
			uuid := key[start+1 : end]

			val, err := client.Get(ctx, key).Float64()
			if err != nil {
				log.Printf("Warning: Failed to get playtime for %s from Redis: %v", uuid, err)
				continue
			}

			mu.Lock()
			playtimes[uuid] = val
			mu.Unlock()
		}
		return iter.Err()
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan playtime redisu from Redis: %w", err)
	}

	return playtimes, nil
}

// SetPlayerDeltaPlaytime stores the delta playtime for a player's last session
func (pps *PlayerPlaytimeStore) SetPlayerDeltaPlaytime(ctx context.Context, playerUUID string, deltaPlaytime float64) error {
	key := fmt.Sprintf(redisu.DeltaPlaytimeKeyPrefix, playerUUID)

	// Set with a reasonable TTL (e.g., 24 hours) for delta playtime
	deltaTTL := 24 * time.Hour
	err := pps.redisClient.Set(ctx, key, deltaPlaytime, deltaTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to set delta playtime for %s: %w", playerUUID, err)
	}

	log.Printf("Delta playtime set for %s: %.2f seconds", playerUUID, deltaPlaytime)
	return nil
}

// GetPlayerDeltaPlaytime retrieves a player's delta playtime from their last session
func (pps *PlayerPlaytimeStore) GetPlayerDeltaPlaytime(ctx context.Context, playerUUID string) (float64, error) {
	key := fmt.Sprintf(redisu.DeltaPlaytimeKeyPrefix, playerUUID)

	val, err := pps.redisClient.Get(ctx, key).Float64()
	if err == redis.Nil {
		return 1.0, fmt.Errorf("no delta playtime found for player %s", playerUUID)
	}
	if err != nil {
		return 1.0, fmt.Errorf("failed to get delta playtime for %s: %w", playerUUID, err)
	}

	return val, nil
}

// SetPlayerTeam sets a player's assigned team in Redis.
func (pps *PlayerPlaytimeStore) SetPlayerTeam(ctx context.Context, uuid string, teamID string) error {
	key := fmt.Sprintf(redisu.PlayerTeamKeyPrefix, uuid)
	return pps.redisClient.Set(ctx, key, teamID, 0).Err()
}
