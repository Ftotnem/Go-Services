// game/store/online_status_store.go
package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// OnlinePlayersStore handles online player status and session management in Redis
type OnlinePlayersStore struct {
	client    *redis.ClusterClient
	onlineTTL time.Duration
}

// NewOnlinePlayersStore creates a new OnlinePlayersStore instance
func NewOnlinePlayersStore(client *redis.ClusterClient, onlineTTL time.Duration) *OnlinePlayersStore {
	return &OnlinePlayersStore{
		client:    client,
		onlineTTL: onlineTTL,
	}
}

// SetPlayerOnline marks a player as online with session start time
func (ops *OnlinePlayersStore) SetPlayerOnline(ctx context.Context, playerUUID string, sessionStartTime time.Time) error {
	key := fmt.Sprintf("online:{%s}:", playerUUID)

	// Store the session start timestamp as the value
	startTimestamp := sessionStartTime.Unix()
	err := ops.client.Set(ctx, key, startTimestamp, ops.onlineTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to set player %s online: %w", playerUUID, err)
	}

	log.Printf("Player %s marked online at %v", playerUUID, sessionStartTime)
	return nil
}

// GetPlayerOnlineTime retrieves when a player went online
func (ops *OnlinePlayersStore) GetPlayerOnlineTime(ctx context.Context, playerUUID string) (time.Time, error) {
	key := fmt.Sprintf("online:{%s}:", playerUUID)

	val, err := ops.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return time.Time{}, fmt.Errorf("player %s is not marked as online", playerUUID)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get online time for %s: %w", playerUUID, err)
	}

	timestamp, parseErr := strconv.ParseInt(val, 10, 64)
	if parseErr != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp for player %s: %s", playerUUID, val)
	}

	return time.Unix(timestamp, 0), nil
}

// IsPlayerOnline checks if a player is currently marked as online
func (ops *OnlinePlayersStore) IsPlayerOnline(ctx context.Context, playerUUID string) (bool, error) {
	key := fmt.Sprintf("online:{%s}:", playerUUID)
	exists, err := ops.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check online status for %s: %w", playerUUID, err)
	}
	return exists == 1, nil
}

// RemovePlayerOnline removes a player's online status
func (ops *OnlinePlayersStore) RemovePlayerOnline(ctx context.Context, playerUUID string) error {
	key := fmt.Sprintf("online:{%s}:", playerUUID)
	deletedCount, err := ops.client.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to remove online status for %s: %w", playerUUID, err)
	}

	if deletedCount > 0 {
		log.Printf("Player %s removed from online status", playerUUID)
	} else {
		log.Printf("Player %s was not marked as online", playerUUID)
	}

	return nil
}

// GetPlayerDeltaPlaytime retrieves a player's delta playtime from their last session
func (ops *OnlinePlayersStore) GetPlayerDeltaPlaytime(ctx context.Context, playerUUID string) (float64, error) {
	key := fmt.Sprintf("deltatime:{%s}:", playerUUID)

	val, err := ops.client.Get(ctx, key).Float64()
	if err == redis.Nil {
		return 1.0, fmt.Errorf("no delta playtime found for player %s", playerUUID)
	}
	if err != nil {
		return 0.0, fmt.Errorf("failed to get delta playtime for %s: %w", playerUUID, err)
	}

	return val, nil
}

// GetAllOnlinePlayers retrieves all currently online players with their session start times
func (ops *OnlinePlayersStore) GetAllOnlinePlayers(ctx context.Context) (map[string]time.Time, error) {
	onlinePlayers := make(map[string]time.Time)
	var mu sync.Mutex

	err := ops.client.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		if client == nil {
			log.Printf("Warning: ForEachMaster provided nil client, skipping node")
			return nil
		}

		iter := client.Scan(ctx, 0, "online:{*}:", 0).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()

			// Extract UUID from key: "online:{uuid}:"
			start := strings.Index(key, "{")
			end := strings.Index(key, "}")
			if start == -1 || end == -1 || end <= start {
				log.Printf("Warning: Could not parse UUID from online key: %s", key)
				continue
			}
			uuid := key[start+1 : end]

			// Get the session start time
			val, err := client.Get(ctx, key).Result()
			if err != nil {
				log.Printf("Warning: Failed to get session time for %s: %v", uuid, err)
				continue
			}

			timestamp, parseErr := strconv.ParseInt(val, 10, 64)
			if parseErr != nil {
				log.Printf("Warning: Invalid timestamp for %s: %s", uuid, val)
				continue
			}

			sessionStart := time.Unix(timestamp, 0)

			mu.Lock()
			onlinePlayers[uuid] = sessionStart
			mu.Unlock()
		}

		return iter.Err()
	})

	if err != nil {
		return nil, fmt.Errorf("error scanning online players: %w", err)
	}

	return onlinePlayers, nil
}

// GetOnlinePlayerCount returns the total number of online players
func (ops *OnlinePlayersStore) GetOnlinePlayerCount(ctx context.Context) (int, error) {
	onlinePlayers, err := ops.GetAllOnlinePlayers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get online player count: %w", err)
	}
	return len(onlinePlayers), nil
}

// GetPlayerSessionDuration calculates how long a player has been online
func (ops *OnlinePlayersStore) GetPlayerSessionDuration(ctx context.Context, playerUUID string) (time.Duration, error) {
	sessionStart, err := ops.GetPlayerOnlineTime(ctx, playerUUID)
	if err != nil {
		return 0, err
	}

	duration := time.Since(sessionStart)
	return duration, nil
}

// RefreshPlayerOnlineStatus extends the TTL for a player's online status
func (ops *OnlinePlayersStore) RefreshPlayerOnlineStatus(ctx context.Context, playerUUID string) error {
	key := fmt.Sprintf("online:{%s}:", playerUUID)

	// Check if player is online first
	exists, err := ops.client.Exists(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to check online status for %s: %w", playerUUID, err)
	}
	if exists == 0 {
		return fmt.Errorf("player %s is not marked as online", playerUUID)
	}

	// Extend the TTL
	success, err := ops.client.Expire(ctx, key, ops.onlineTTL).Result()
	if err != nil {
		return fmt.Errorf("failed to refresh TTL for %s: %w", playerUUID, err)
	}
	if !success {
		return fmt.Errorf("failed to extend TTL for %s (key may have expired)", playerUUID)
	}

	return nil
}

// CleanupExpiredSessions removes any expired online sessions (manual cleanup)
func (ops *OnlinePlayersStore) CleanupExpiredSessions(ctx context.Context) (int, error) {
	onlinePlayers, err := ops.GetAllOnlinePlayers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get online players for cleanup: %w", err)
	}

	var expiredCount int
	cutoffTime := time.Now().Add(-ops.onlineTTL)

	for uuid, sessionStart := range onlinePlayers {
		if sessionStart.Before(cutoffTime) {
			if err := ops.RemovePlayerOnline(ctx, uuid); err != nil {
				log.Printf("Warning: Failed to cleanup expired session for %s: %v", uuid, err)
			} else {
				expiredCount++
				log.Printf("Cleaned up expired session for player %s", uuid)
			}
		}
	}

	return expiredCount, nil
}
