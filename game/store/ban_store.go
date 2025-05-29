// game/store/ban_store.go
package store

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// BanStore handles player ban operations in Redis
type BanStore struct {
	client *redis.ClusterClient
}

// NewBanStore creates a new BanStore instance
func NewBanStore(client *redis.ClusterClient) *BanStore {
	return &BanStore{
		client: client,
	}
}

// BanPlayer bans a player with an optional expiration time
func (bs *BanStore) BanPlayer(ctx context.Context, playerUUID string, expiresAt *time.Time, reason string) error {
	key := fmt.Sprintf("banned:{%s}:", playerUUID)

	var banExpiresAt int64
	var duration time.Duration

	if expiresAt != nil {
		// Temporary ban
		banExpiresAt = expiresAt.Unix()
		duration = time.Until(*expiresAt)
		if duration < 0 {
			duration = 1 * time.Millisecond // Already expired, but set minimal duration
		}
	} else {
		// Permanent ban
		banExpiresAt = 0
		duration = 0 // No expiration
	}

	// Store the ban with expiration timestamp as value
	err := bs.client.Set(ctx, key, banExpiresAt, duration).Err()
	if err != nil {
		return fmt.Errorf("failed to ban player %s: %w", playerUUID, err)
	}

	// Optionally store ban reason in a separate key
	if reason != "" {
		reasonKey := fmt.Sprintf("ban_reason:{%s}:", playerUUID)
		reasonDuration := duration
		if duration == 0 {
			reasonDuration = 0 // Permanent
		}
		if err := bs.client.Set(ctx, reasonKey, reason, reasonDuration).Err(); err != nil {
			log.Printf("Warning: Failed to store ban reason for %s: %v", playerUUID, err)
		}
	}

	if expiresAt != nil {
		log.Printf("Player %s banned until %v. Reason: %s", playerUUID, *expiresAt, reason)
	} else {
		log.Printf("Player %s permanently banned. Reason: %s", playerUUID, reason)
	}

	return nil
}

// UnbanPlayer removes a ban from a player
func (bs *BanStore) UnbanPlayer(ctx context.Context, playerUUID string) error {
	banKey := fmt.Sprintf("banned:{%s}:", playerUUID)
	reasonKey := fmt.Sprintf("ban_reason:{%s}:", playerUUID)

	// Delete both ban and reason keys
	deletedCount, err := bs.client.Del(ctx, banKey, reasonKey).Result()
	if err != nil {
		return fmt.Errorf("failed to unban player %s: %w", playerUUID, err)
	}

	if deletedCount > 0 {
		log.Printf("Player %s has been unbanned (%d keys deleted)", playerUUID, deletedCount)
	} else {
		log.Printf("Player %s was not banned (no ban keys found)", playerUUID)
	}

	return nil
}

// IsPlayerBanned checks if a player is currently banned
func (bs *BanStore) IsPlayerBanned(ctx context.Context, playerUUID string) (bool, error) {
	key := fmt.Sprintf("banned:{%s}:", playerUUID)
	val, err := bs.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil // Not banned
	}
	if err != nil {
		return false, fmt.Errorf("failed to check ban status for %s: %w", playerUUID, err)
	}

	// Parse the expiration timestamp
	expiresAt, parseErr := strconv.ParseInt(val, 10, 64)
	if parseErr != nil {
		log.Printf("Warning: Ban status for %s has invalid timestamp: %s. Treating as not banned.", playerUUID, val)
		return false, nil
	}

	// Check if temporary ban has expired
	if expiresAt > 0 && time.Now().Unix() >= expiresAt {
		// Ban has expired, clean it up asynchronously
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := bs.UnbanPlayer(cleanupCtx, playerUUID); err != nil {
				log.Printf("Error cleaning up expired ban for %s: %v", playerUUID, err)
			}
		}()
		return false, nil
	}

	return true, nil
}

// GetBanInfo retrieves ban information for a player
func (bs *BanStore) GetBanInfo(ctx context.Context, playerUUID string) (*BanInfo, error) {
	banKey := fmt.Sprintf("banned:{%s}:", playerUUID)
	reasonKey := fmt.Sprintf("ban_reason:{%s}:", playerUUID)

	// Use pipeline to get both ban timestamp and reason
	pipe := bs.client.Pipeline()
	banCmd := pipe.Get(ctx, banKey)
	reasonCmd := pipe.Get(ctx, reasonKey)
	pipe.Exec(ctx)

	// Check if player is banned
	banVal, banErr := banCmd.Result()
	if banErr == redis.Nil {
		return nil, nil // Not banned
	}
	if banErr != nil {
		return nil, fmt.Errorf("failed to get ban info for %s: %w", playerUUID, banErr)
	}

	// Parse expiration timestamp
	expiresAt, parseErr := strconv.ParseInt(banVal, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("invalid ban timestamp for %s: %s", playerUUID, banVal)
	}

	// Get ban reason (optional)
	reason, reasonErr := reasonCmd.Result()
	if reasonErr == redis.Nil {
		reason = "No reason provided"
	} else if reasonErr != nil {
		log.Printf("Warning: Could not retrieve ban reason for %s: %v", playerUUID, reasonErr)
		reason = "Unknown reason"
	}

	banInfo := &BanInfo{
		PlayerUUID:  playerUUID,
		Reason:      reason,
		IsPermanent: expiresAt == 0,
	}

	if expiresAt > 0 {
		expireTime := time.Unix(expiresAt, 0)
		banInfo.ExpiresAt = &expireTime
		banInfo.IsActive = time.Now().Before(expireTime)
	} else {
		banInfo.IsActive = true // Permanent ban
	}

	return banInfo, nil
}

// BanInfo represents ban information for a player
type BanInfo struct {
	PlayerUUID  string     `json:"player_uuid"`
	Reason      string     `json:"reason"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	IsPermanent bool       `json:"is_permanent"`
	IsActive    bool       `json:"is_active"`
}

// GetAllBannedPlayers retrieves all currently banned players
func (bs *BanStore) GetAllBannedPlayers(ctx context.Context) (map[string]*BanInfo, error) {
	bannedPlayers := make(map[string]*BanInfo)

	// Scan for all banned keys
	iter := bs.client.Scan(ctx, 0, "banned:{*}:", 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()

		// Extract UUID from key
		start := len("banned:{")
		end := len(key) - 2 // Remove "}:"
		if start >= end {
			log.Printf("Warning: Could not parse UUID from ban key: %s", key)
			continue
		}
		uuid := key[start:end]

		// Get ban info for this player
		banInfo, err := bs.GetBanInfo(ctx, uuid)
		if err != nil {
			log.Printf("Warning: Failed to get ban info for %s: %v", uuid, err)
			continue
		}

		if banInfo != nil && banInfo.IsActive {
			bannedPlayers[uuid] = banInfo
		}
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan banned players: %w", err)
	}

	return bannedPlayers, nil
}
