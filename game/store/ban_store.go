// game/store/ban_store.go
package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis" // Alias for Redis constants
	"github.com/redis/go-redis/v9"
)

// BanInfo represents the details of a player's ban.
type BanInfo struct {
	PlayerUUID  string     `json:"player_uuid"`
	Reason      string     `json:"reason"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	IsPermanent bool       `json:"is_permanent"`
	IsActive    bool       `json:"is_active"` // Indicates if the ban is currently in effect
}

// BanStore handles player ban operations using Redis.
// It manages ban status and reasons for individual players.
type BanStore struct {
	client *redis.ClusterClient
}

// NewBanStore creates a new BanStore instance.
// It requires a connected Redis Cluster client.
func NewBanStore(client *redis.ClusterClient) *BanStore {
	return &BanStore{
		client: client,
	}
}

// BanPlayer applies a ban to a player.
// A ban can be temporary (with an expiration time) or permanent.
func (bs *BanStore) BanPlayer(ctx context.Context, playerUUID string, expiresAt *time.Time, reason string) error {
	// Construct the Redis key using the predefined constant for consistency.
	banKey := fmt.Sprintf(redisu.BannedKeyPrefix, playerUUID)
	reasonKey := fmt.Sprintf("ban_reason:%s", playerUUID) // Using a similar pattern for reason key

	var banExpiresAtUnix int64
	var duration time.Duration

	if expiresAt != nil {
		// Calculate duration for temporary ban.
		banExpiresAtUnix = expiresAt.Unix()
		duration = time.Until(*expiresAt)
		if duration < 0 {
			// If the expiration is in the past, set a minimal duration to ensure the key is set briefly
			// before Redis's TTL mechanism removes it. This handles cases where BanPlayer is called
			// with an already-expired timestamp.
			duration = 1 * time.Millisecond
		}
	} else {
		// For a permanent ban, set expiration to 0 (no TTL) and timestamp to 0.
		banExpiresAtUnix = 0
		duration = 0 // A duration of 0 means no expiration in Redis Set command.
	}

	// Store the ban status: key -> playerUUID, value -> Unix timestamp of expiration (0 for permanent).
	err := bs.client.Set(ctx, banKey, banExpiresAtUnix, duration).Err()
	if err != nil {
		return fmt.Errorf("failed to set ban status for player %s in Redis: %w", playerUUID, err)
	}

	// Store the ban reason if provided. Its TTL will match the ban status.
	if reason != "" {
		reasonDuration := duration // Reason should expire with the ban itself
		if err := bs.client.Set(ctx, reasonKey, reason, reasonDuration).Err(); err != nil {
			log.Printf("Warning: Could not store ban reason for player %s: %v", playerUUID, err)
		}
	}

	if expiresAt != nil {
		log.Printf("Player %s temporarily banned until %v. Reason: %s", playerUUID, *expiresAt, reason)
	} else {
		log.Printf("Player %s permanently banned. Reason: %s", playerUUID, reason)
	}

	return nil
}

// UnbanPlayer removes a ban from a player by deleting the relevant Redis keys.
func (bs *BanStore) UnbanPlayer(ctx context.Context, playerUUID string) error {
	banKey := fmt.Sprintf(redisu.BannedKeyPrefix, playerUUID)
	reasonKey := fmt.Sprintf("ban_reason:%s", playerUUID)

	// Atomically delete both the ban status and ban reason keys.
	deletedCount, err := bs.client.Del(ctx, banKey, reasonKey).Result()
	if err != nil {
		return fmt.Errorf("failed to delete ban keys for player %s: %w", playerUUID, err)
	}

	if deletedCount > 0 {
		log.Printf("Player %s has been unbanned (%d ban-related keys removed).", playerUUID, deletedCount)
	} else {
		log.Printf("Player %s was not actively banned (no ban keys found to delete).", playerUUID)
	}

	return nil
}

// IsPlayerBanned checks if a player is currently banned.
// It also handles automatic cleanup of expired temporary bans.
func (bs *BanStore) IsPlayerBanned(ctx context.Context, playerUUID string) (bool, error) {
	key := fmt.Sprintf(redisu.BannedKeyPrefix, playerUUID)
	val, err := bs.client.Get(ctx, key).Result()

	if err == redis.Nil {
		return false, nil // Player is not banned (key doesn't exist)
	}
	if err != nil {
		return false, fmt.Errorf("failed to retrieve ban status for player %s from Redis: %w", playerUUID, err)
	}

	// Parse the expiration Unix timestamp from the Redis value.
	expiresAtUnix, parseErr := strconv.ParseInt(val, 10, 64)
	if parseErr != nil {
		// Log a warning if the stored value is malformed and treat as not banned.
		log.Printf("Warning: Ban record for player %s contains an invalid expiration timestamp '%s'. Treating as not banned.", playerUUID, val)
		return false, nil
	}

	// If it's a temporary ban (expiresAtUnix > 0) and it has passed, the ban is expired.
	if expiresAtUnix > 0 && time.Now().Unix() >= expiresAtUnix {
		// The ban has expired. Asynchronously clean up the keys to prevent stale data.
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := bs.UnbanPlayer(cleanupCtx, playerUUID); err != nil {
				log.Printf("Error cleaning up expired ban for player %s: %v", playerUUID, err)
			}
		}()
		return false, nil // Ban expired, so player is no longer considered banned.
	}

	// If expiresAtUnix is 0, it's a permanent ban. Otherwise, it's an active temporary ban.
	return true, nil
}

// GetBanInfo retrieves detailed ban information for a player.
// Returns nil, nil if the player is not banned.
func (bs *BanStore) GetBanInfo(ctx context.Context, playerUUID string) (*BanInfo, error) {
	banKey := fmt.Sprintf(redisu.BannedKeyPrefix, playerUUID)
	reasonKey := fmt.Sprintf("ban_reason:%s", playerUUID)

	// Use a Redis pipeline to fetch both the ban status and reason concurrently.
	pipe := bs.client.Pipeline()
	banCmd := pipe.Get(ctx, banKey)
	reasonCmd := pipe.Get(ctx, reasonKey)
	_, err := pipe.Exec(ctx) // Execute the pipeline commands
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to execute Redis pipeline for ban info for player %s: %w", playerUUID, err)
	}

	// Get the ban expiration timestamp.
	banVal, banErr := banCmd.Result()
	if banErr == redis.Nil {
		return nil, nil // Player is not banned.
	}
	if banErr != nil {
		return nil, fmt.Errorf("failed to get ban expiration for player %s from Redis: %w", playerUUID, banErr)
	}

	// Parse the expiration timestamp.
	expiresAtUnix, parseErr := strconv.ParseInt(banVal, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("invalid ban expiration timestamp stored for player %s: '%s'", playerUUID, banVal)
	}

	// Get the ban reason. Handle cases where the reason key might not exist.
	reason, reasonErr := reasonCmd.Result()
	if reasonErr == redis.Nil {
		reason = "No reason provided" // Default if reason key is missing
	} else if reasonErr != nil {
		log.Printf("Warning: Could not retrieve ban reason for player %s: %v", playerUUID, reasonErr)
		reason = "Unknown reason" // Fallback for other errors
	}

	banInfo := &BanInfo{
		PlayerUUID:  playerUUID,
		Reason:      reason,
		IsPermanent: expiresAtUnix == 0, // Permanent if expiration timestamp is 0
	}

	if expiresAtUnix > 0 {
		// For temporary bans, set the actual expiration time and check if it's active.
		expireTime := time.Unix(expiresAtUnix, 0)
		banInfo.ExpiresAt = &expireTime
		banInfo.IsActive = time.Now().Before(expireTime) // Ban is active if current time is before expiration
	} else {
		// Permanent bans are always active.
		banInfo.IsActive = true
	}

	// If the ban is found but it's expired, return nil to signify no active ban.
	// This also triggers an asynchronous cleanup, similar to IsPlayerBanned.
	if !banInfo.IsActive {
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := bs.UnbanPlayer(cleanupCtx, playerUUID); err != nil {
				log.Printf("Error cleaning up expired ban for player %s after GetBanInfo: %v", playerUUID, err)
			}
		}()
		return nil, nil // No active ban found
	}

	return banInfo, nil
}

// GetAllBannedPlayers retrieves information for all currently active banned players.
// It scans Redis for all ban keys and fetches their details.
func (bs *BanStore) GetAllBannedPlayers(ctx context.Context) (map[string]*BanInfo, error) {
	bannedPlayers := make(map[string]*BanInfo)

	// Scan for all keys that match the banned player key pattern.
	// We use '*' within the curly braces for cluster-friendly scanning (hash tag).
	iter := bs.client.Scan(ctx, 0, fmt.Sprintf(redisu.BannedKeyPrefix, "*"), 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()

		// Extract the player UUID from the Redis key.
		// Example key: banned:{uuid}:
		// We need to parse "uuid" from it.
		// The `redisu.BannedKeyPrefix` is "banned:{%s}:", so we extract what's between `{` and `}:`.
		// A more robust way to extract UUID might involve regular expressions or careful string manipulation
		// if the pattern gets more complex, but for this fixed pattern, slicing is efficient.
		// Let's ensure the slice is safe.
		const prefixLen = len("banned:{")
		const suffixLen = len("}:")
		if len(key) > prefixLen+suffixLen { // Ensure key is long enough to contain a UUID
			uuid := key[prefixLen : len(key)-suffixLen]

			// Get detailed ban information for the extracted UUID.
			// GetBanInfo will automatically handle expired bans and clean them up.
			banInfo, err := bs.GetBanInfo(ctx, uuid)
			if err != nil {
				log.Printf("Warning: Failed to retrieve ban info for player %s during full scan: %v", uuid, err)
				continue
			}

			// Add to the map only if the ban is active.
			if banInfo != nil && banInfo.IsActive {
				bannedPlayers[uuid] = banInfo
			}
		} else {
			log.Printf("Warning: Skipped invalid ban key format during scan: %s", key)
		}
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate through banned player keys in Redis: %w", err)
	}

	return bannedPlayers, nil
}
