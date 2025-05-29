package store

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis"

	"github.com/redis/go-redis/v9"
)

// TeamPlaytimeStore handles team playtime operations exclusively in Redis.
// This store is responsible for maintaining a team's current session playtime
// in Redis, which will later be periodically synchronized with a Team Stats microservice.
type TeamPlaytimeStore struct {
	redisClient *redis.ClusterClient
}

// NewTeamPlaytimeStore creates a new TeamPlaytimeStore instance.
// It no longer requires MongoDB client details.
func NewTeamPlaytimeStore(redisClient *redis.ClusterClient) *TeamPlaytimeStore {
	return &TeamPlaytimeStore{
		redisClient: redisClient,
	}
}

// SetTeamPlaytime sets a team's current total playtime in Redis.
// This is typically used to initialize or overwrite the playtime for a session.
func (tps *TeamPlaytimeStore) SetTeamPlaytime(ctx context.Context, teamID string, totalPlaytime float64) error {
	// Using a relatively short TTL for in-session playtime,
	// as the authoritative source will be the Team Stats Microservice.
	// Adjust TTL as needed based on your game's session duration and sync frequency.

	key := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID) // <-- Using the constant here
	err := tps.redisClient.Set(ctx, key, totalPlaytime, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to set playtime for team %s in Redis: %w", teamID, err)
	}

	log.Printf("Set playtime for team %s in Redis: %.2f seconds", teamID, totalPlaytime)
	return nil
}

// GetTeamPlaytime retrieves a team's current total playtime from Redis.
func (tps *TeamPlaytimeStore) GetTeamPlaytime(ctx context.Context, teamID string) (float64, error) {
	key := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID) // <-- Using the constant here

	val, err := tps.redisClient.Get(ctx, key).Float64()
	if err == redis.Nil {
		// If the key doesn't exist, it means the team has 0 playtime in the current session
		return 0.0, nil
	}
	if err != nil {
		return 0.0, fmt.Errorf("failed to get playtime for team %s from Redis: %w", teamID, err)
	}

	return val, nil
}

// IncrementTeamPlaytime increments a team's total playtime in Redis.
// This is the primary method for updating team playtime during gameplay.
func (tps *TeamPlaytimeStore) IncrementTeamPlaytime(ctx context.Context, teamID string, additionalPlaytime float64) error {
	playtimeTTL := 6 * time.Hour

	key := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID) // <-- Using the constant here

	currentPlaytime, err := tps.redisClient.IncrByFloat(ctx, key, additionalPlaytime).Result()
	if err != nil {
		return fmt.Errorf("failed to increment playtime for team %s in Redis: %w", teamID, err)
	}

	// Also refresh the TTL so the key doesn't expire prematurely if the session is long
	err = tps.redisClient.Expire(ctx, key, playtimeTTL).Err()
	if err != nil {
		log.Printf("Warning: Failed to refresh TTL for team %s playtime: %v", teamID, err)
		// Don't return error here, as the increment itself was successful.
	}

	log.Printf("Incremented playtime for team %s in Redis by %.2f. New total: %.2f", teamID, additionalPlaytime, currentPlaytime)
	return nil
}

// DeleteTeamPlaytime removes a team's playtime record from Redis.
// This might be used when a team is disbanded or a game session ends.
func (tps *TeamPlaytimeStore) DeleteTeamPlaytime(ctx context.Context, teamID string) error {
	key := fmt.Sprintf(redisu.TeamTotalPlaytimePrefix, teamID) // <-- Using the constant here
	deletedCount, err := tps.redisClient.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to delete playtime for team %s from Redis: %w", teamID, err)
	}

	if deletedCount > 0 {
		log.Printf("Deleted playtime record for team %s from Redis", teamID)
	} else {
		log.Printf("No playtime record found for team %s in Redis to delete", teamID)
	}
	return nil
}

// GetAllTeamPlaytimes retrieves all current team playtime data from Redis.
// This is useful for periodic synchronization to a Team Stats Microservice.
func (tps *TeamPlaytimeStore) GetAllTeamPlaytimes(ctx context.Context) (map[string]float64, error) {
	teamPlaytimes := make(map[string]float64)
	var mu sync.Mutex // Mutex to protect 'teamPlaytimes' map during concurrent access

	// Construct the scan pattern using the constant
	scanPattern := strings.Replace(redisu.TeamTotalPlaytimePrefix, "%s", "*", 1) // <-- Using the constant here

	// Use ForEachMaster to scan all master nodes in a Redis Cluster.
	// This ensures you gather all data across all shards.
	err := tps.redisClient.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		if client == nil {
			log.Printf("Warning: ForEachMaster provided nil client, skipping node")
			return nil
		}

		iter := client.Scan(ctx, 0, scanPattern, 0).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()

			// Extract TeamID from key: "team_total_playtime:{teamID}:"
			start := strings.Index(key, "{")
			end := strings.Index(key, "}")
			if start == -1 || end == -1 || end <= start {
				log.Printf("Warning: Could not parse TeamID from team playtime key: %s", key)
				continue
			}
			teamID := key[start+1 : end]

			val, err := client.Get(ctx, key).Float64()
			if err != nil {
				log.Printf("Warning: Failed to get playtime for %s from Redis: %v", teamID, err)
				continue
			}

			mu.Lock()
			teamPlaytimes[teamID] = val
			mu.Unlock()
		}
		return iter.Err() // Return any error from iterator
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan team playtime redisu from Redis: %w", err)
	}

	return teamPlaytimes, nil
}
