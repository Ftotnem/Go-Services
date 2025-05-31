// game/store/online_status_store.go
package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings" // Still needed for parsing UUID from scanned keys
	"sync"
	"time"

	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis" // Alias for Redis constants
	"github.com/redis/go-redis/v9"
)

// OnlinePlayersStore manages the online status and session details of players in Redis.
// It uses Redis's TTL (Time To Live) feature to automatically expire online status keys
// after a defined duration, effectively acting as a heartbeat mechanism.
type OnlinePlayersStore struct {
	client    *redis.ClusterClient
	onlineTTL time.Duration // The duration after which an online status key expires if not refreshed.
}

// NewOnlinePlayersStore creates and returns a new OnlinePlayersStore instance.
// It requires a connected Redis Cluster client and a time-to-live duration for online status.
func NewOnlinePlayersStore(client *redis.ClusterClient, onlineTTL time.Duration) *OnlinePlayersStore {
	return &OnlinePlayersStore{
		client:    client,
		onlineTTL: onlineTTL,
	}
}

// SetPlayerOnline marks a player as online in Redis and stores their session start time.
// The key will automatically expire after `ops.onlineTTL` unless refreshed.
func (ops *OnlinePlayersStore) SetPlayerOnline(ctx context.Context, playerUUID string, sessionStartTime time.Time) error {
	// Construct the Redis key using the predefined constant for consistency.
	key := fmt.Sprintf(redisu.OnlineKeyPrefix, playerUUID)

	// Store the session start timestamp (Unix seconds) as the value.
	startTimestamp := sessionStartTime.Unix()
	err := ops.client.Set(ctx, key, startTimestamp, ops.onlineTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to set player %s online status in Redis: %w", playerUUID, err)
	}

	log.Printf("Player %s marked online with session start time: %v (TTL: %s)", playerUUID, sessionStartTime, ops.onlineTTL)
	return nil
}

// GetPlayerOnlineTime retrieves the recorded session start time for an online player.
// Returns a zero Time and an error if the player is not marked as online or if the data is invalid.
func (ops *OnlinePlayersStore) GetPlayerOnlineTime(ctx context.Context, playerUUID string) (time.Time, error) {
	key := fmt.Sprintf(redisu.OnlineKeyPrefix, playerUUID)

	val, err := ops.client.Get(ctx, key).Result()
	if err == redis.Nil {
		// Specific error for "not found" scenario.
		return time.Time{}, fmt.Errorf("player %s is not currently marked as online: %w", playerUUID, redisu.ErrRedisKeyNotFound)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to retrieve online time for player %s from Redis: %w", playerUUID, err)
	}

	// Parse the Unix timestamp string back to int64.
	timestamp, parseErr := strconv.ParseInt(val, 10, 64)
	if parseErr != nil {
		return time.Time{}, fmt.Errorf("invalid session start timestamp '%s' for player %s in Redis: %w", val, playerUUID, parseErr)
	}

	return time.Unix(timestamp, 0), nil // Convert Unix timestamp to Go time.Time
}

// IsPlayerOnline checks if a player's online status key currently exists in Redis.
// This is a quick check without retrieving the session start time.
func (ops *OnlinePlayersStore) IsPlayerOnline(ctx context.Context, playerUUID string) (bool, error) {
	key := fmt.Sprintf(redisu.OnlineKeyPrefix, playerUUID)
	exists, err := ops.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check online existence for player %s in Redis: %w", playerUUID, err)
	}
	return exists == 1, nil // exists == 1 means the key exists
}

// RemovePlayerOnline explicitly deletes a player's online status key from Redis.
// This is called when a player logs off or their session explicitly ends.
func (ops *OnlinePlayersStore) RemovePlayerOnline(ctx context.Context, playerUUID string) error {
	key := fmt.Sprintf(redisu.OnlineKeyPrefix, playerUUID)
	deletedCount, err := ops.client.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to remove online status key for player %s from Redis: %w", playerUUID, err)
	}

	if deletedCount > 0 {
		log.Printf("Player %s's online status removed from Redis.", playerUUID)
	} else {
		log.Printf("Attempted to remove online status for player %s, but they were not marked as online.", playerUUID)
	}

	return nil
}

// GetAllOnlinePlayers retrieves a map of all currently online players and their session start times.
// In a Redis Cluster, this involves iterating over all master nodes.
func (ops *OnlinePlayersStore) GetAllOnlinePlayers(ctx context.Context) (map[string]time.Time, error) {
	onlinePlayers := make(map[string]time.Time)
	var mu sync.Mutex // Mutex to protect concurrent map writes from different cluster nodes

	// ForEachMaster iterates over all master nodes in the Redis Cluster.
	err := ops.client.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		if client == nil {
			log.Printf("Warning: Redis Cluster ForEachMaster provided a nil client, skipping node.")
			return nil // Skip this iteration if client is nil
		}

		// Use SCAN to iterate through keys on the current master node.
		// The pattern "online:{*}:" ensures we only get keys matching our online status format.
		iter := client.Scan(ctx, 0, fmt.Sprintf(redisu.OnlineKeyPrefix, "*"), 0).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()

			// Extract the player UUID from the key (e.g., "online:{uuid}:" -> "uuid").
			// This string manipulation assumes the fixed format of `redisu.OnlineKeyPrefix`.
			startIdx := strings.Index(key, "{")
			endIdx := strings.Index(key, "}")
			if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
				log.Printf("Warning: Could not parse UUID from malformed online key: %s. Skipping.", key)
				continue
			}
			playerUUID := key[startIdx+1 : endIdx] // Extract the UUID without braces

			// Retrieve the session start timestamp for the found key.
			val, err := client.Get(ctx, key).Result()
			if err != nil {
				log.Printf("Warning: Failed to get session start time for player %s (key: %s): %v. Skipping.", playerUUID, key, err)
				continue
			}

			// Parse the timestamp string to a time.Time object.
			timestamp, parseErr := strconv.ParseInt(val, 10, 64)
			if parseErr != nil {
				log.Printf("Warning: Invalid timestamp '%s' for player %s (key: %s). Skipping.", val, playerUUID, key)
				continue
			}
			sessionStart := time.Unix(timestamp, 0)

			// Safely add to the shared map.
			mu.Lock()
			onlinePlayers[playerUUID] = sessionStart
			mu.Unlock()
		}

		return iter.Err() // Return any error from the iterator
	})

	if err != nil {
		return nil, fmt.Errorf("error during scan of online players across Redis masters: %w", err)
	}

	return onlinePlayers, nil
}

// GetOnlinePlayerCount returns the total number of players currently marked as online.
// This is less efficient than a direct Redis COUNT if available for key patterns,
// but accurate as it counts active sessions.
func (ops *OnlinePlayersStore) GetOnlinePlayerCount(ctx context.Context) (int, error) {
	onlinePlayers, err := ops.GetAllOnlinePlayers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve all online players to count: %w", err)
	}
	return len(onlinePlayers), nil
}

// GetPlayerSessionDuration calculates the elapsed time since a player went online.
// Returns a duration of 0 and an error if the player is not online.
func (ops *OnlinePlayersStore) GetPlayerSessionDuration(ctx context.Context, playerUUID string) (time.Duration, error) {
	sessionStart, err := ops.GetPlayerOnlineTime(ctx, playerUUID)
	if err != nil {
		// Propagate the specific "not online" error or other retrieval errors.
		return 0, err
	}

	duration := time.Since(sessionStart) // Calculate duration from session start to now.
	return duration, nil
}

// RefreshPlayerOnlineStatus extends the TTL (Time To Live) for a player's online status key.
// This acts as a "heartbeat" to keep a player marked as online.
func (ops *OnlinePlayersStore) RefreshPlayerOnlineStatus(ctx context.Context, playerUUID string) error {
	key := fmt.Sprintf(redisu.OnlineKeyPrefix, playerUUID)

	// Use Redis's EXPIRE command to set the new TTL.
	// This command only works if the key already exists.
	success, err := ops.client.Expire(ctx, key, ops.onlineTTL).Result()
	if err != nil {
		return fmt.Errorf("failed to refresh online status TTL for player %s in Redis: %w", playerUUID, err)
	}
	if !success {
		// If Expire returns false, it means the key did not exist (e.g., already expired or never set).
		return fmt.Errorf("could not refresh online status for player %s, key might not exist or already expired", playerUUID)
	}

	log.Printf("Online status TTL for player %s refreshed to %s.", playerUUID, ops.onlineTTL)
	return nil
}

// CleanupExpiredSessions manually scans for and removes online sessions that have already expired.
// While Redis's TTL automatically handles expiration, this can be used for explicit cleanup
// or to verify consistency if needed.
func (ops *OnlinePlayersStore) CleanupExpiredSessions(ctx context.Context) (int, error) {
	// Note: For most Redis setups, relying on TTL is sufficient. This manual cleanup
	// might be redundant if your `onlineTTL` is properly configured and clients
	// consistently refresh status. However, it can act as a safety net.
	onlinePlayers, err := ops.GetAllOnlinePlayers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve all online players for manual cleanup: %w", err)
	}

	var expiredCount int
	// Define a cutoff time; sessions started before this time *should* have expired if TTL worked.
	// This approach is more for identifying stale data that *should* have been collected by Redis TTL.
	// A more direct approach for manual cleanup would be to check the key's actual TTL,
	// but `GetAllOnlinePlayers` already filters out expired keys (as they wouldn't be returned by GET).
	// Therefore, this method, as written, checks if the *start time* of the session
	// is older than `onlineTTL` ago, assuming the `onlineTTL` is the maximum
	// expected session duration *without* refreshes.
	cutoffTime := time.Now().Add(-ops.onlineTTL)

	for uuid, sessionStart := range onlinePlayers {
		// If the session's start time plus the TTL is in the past, it's logically expired.
		// However, Redis already removed it via TTL. This loop effectively cleans up
		// potential inconsistencies if a key somehow lost its TTL or if `GetAllOnlinePlayers`
		// returns a key that Redis *thinks* is still active but whose TTL has expired.
		// The `GetPlayerOnlineTime` and `IsPlayerOnline` methods are more reliable for current status.
		// This method's main utility might be for diagnostic purposes.
		if sessionStart.Before(cutoffTime) { // If the session started before the cutoff
			if err := ops.RemovePlayerOnline(ctx, uuid); err != nil {
				log.Printf("Warning: Failed to cleanup logically expired session for player %s: %v", uuid, err)
			} else {
				expiredCount++
				log.Printf("Cleaned up logically expired session for player %s.", uuid)
			}
		}
	}

	return expiredCount, nil
}
