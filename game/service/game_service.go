// game/service/game_service.go
package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Ftotnem/GO-SERVICES/game/store"
	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis"
	playerserviceclient "github.com/Ftotnem/GO-SERVICES/shared/service" // This is your gRPC/HTTP client for Player Service
	"github.com/redis/go-redis/v9"
)

// GameService holds references to the data stores and other dependencies needed
// for game-related business logic. This service now primarily interacts with Redis
// for real-time, in-session data, and delegates long-term persistence
// to other microservices (e.g., Player Service, Team Stats Service) via periodic updates.
type GameService struct {
	PlayerPlaytimeStore *store.PlayerPlaytimeStore // For managing player playtime in Redis
	OnlinePlayersStore  *store.OnlinePlayersStore  // For managing online status and delta playtime in Redis
	TeamPlaytimeStore   *store.TeamPlaytimeStore   // For managing team total playtimes in Redis
	BanStore            *store.BanStore            // For managing player bans in Redis
	RedisClient         *redis.ClusterClient       // Direct Redis client for player team lookup
	PlayerServiceClient *playerserviceclient.PlayerServiceClient
}

// NewGameService is the constructor for GameService.
func NewGameService(
	playerPlaytimeStore *store.PlayerPlaytimeStore,
	onlinePlayersStore *store.OnlinePlayersStore,
	teamPlaytimeStore *store.TeamPlaytimeStore,
	banStore *store.BanStore,
	redisClient *redis.ClusterClient,
	playerServiceClient *playerserviceclient.PlayerServiceClient,
) *GameService {
	return &GameService{
		PlayerPlaytimeStore: playerPlaytimeStore,
		OnlinePlayersStore:  onlinePlayersStore,
		TeamPlaytimeStore:   teamPlaytimeStore,
		BanStore:            banStore,
		RedisClient:         redisClient,
		PlayerServiceClient: playerServiceClient,
	}
}

// PlayerOnline marks a player as online, loads their profile, and initializes Redis data.
func (gs *GameService) PlayerOnline(ctx context.Context, playerUUID string) error {
	// 1. Check if player is banned
	isBanned, err := gs.BanStore.IsPlayerBanned(ctx, playerUUID)
	if err != nil {
		return fmt.Errorf("failed to check ban status for player %s: %w", playerUUID, err)
	}
	if isBanned {
		return fmt.Errorf("player %s is currently banned and cannot go online", playerUUID)
	}

	// 2. Load player profile from Player Service (MongoDB)
	playerProfile, err := gs.PlayerServiceClient.GetPlayerProfile(ctx, playerUUID)
	if err != nil {
		log.Printf("Warning: Could not fetch player profile for %s from Player Service: %v. Initializing with default values.", playerUUID, err)
		// If profile not found or error, initialize with default values
		// total playtime 0.0, delta playtime 1.0 (as per requirement), no team initially in Redis
		if err = gs.PlayerPlaytimeStore.SetPlayerPlaytime(ctx, playerUUID, 0.0); err != nil {
			return fmt.Errorf("failed to initialize total playtime for %s: %w", playerUUID, err)
		}
		if err = gs.PlayerPlaytimeStore.SetPlayerDeltaPlaytime(ctx, playerUUID, 1.0); err != nil {
			return fmt.Errorf("failed to initialize delta playtime for %s: %w", playerUUID, err)
		}
		// No team key set if profile not found
	} else {
		// Profile found, set values from DB
		if err = gs.PlayerPlaytimeStore.SetPlayerPlaytime(ctx, playerUUID, playerProfile.TotalPlaytime); err != nil {
			return fmt.Errorf("failed to set total playtime for %s from profile: %w", playerUUID, err)
		}
		// Delta playtime is always 1.0 on going online, according to previous logic
		if err = gs.PlayerPlaytimeStore.SetPlayerDeltaPlaytime(ctx, playerUUID, 1.0); err != nil {
			return fmt.Errorf("failed to set delta playtime for %s: %w", playerUUID, err)
		}
		// Set player's team in Redis for quick lookup for team playtime updates
		if playerProfile.Team != "" {
			playerTeamKey := fmt.Sprintf(redisu.PlayerTeamKeyPrefix, playerUUID)
			if err = gs.RedisClient.Set(ctx, playerTeamKey, playerProfile.Team, 0).Err(); err != nil { // No expiry, it's tied to player identity
				log.Printf("Warning: Failed to set team ID for player %s in Redis: %v", playerUUID, err)
			}
		}
	}

	// 3. Mark player online in Redis (store session start time and set TTL)
	err = gs.OnlinePlayersStore.SetPlayerOnline(ctx, playerUUID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set player %s online in Redis: %w", playerUUID, err)
	}
	log.Printf("Service: Player %s marked online and data loaded/initialized.", playerUUID)

	return nil
}

// PlayerOffline marks a player as offline, retrieves their final accumulated playtime from Redis,
// persists it to the Player Service (MongoDB), and then cleans up all player-specific keys in Redis.
func (gs *GameService) PlayerOffline(ctx context.Context, playerUUID string) error {
	log.Printf("Service: Handling player %s going offline.", playerUUID)

	// 1. Retrieve the player's final total playtime from Redis.
	// This `totalPlaytime` should already be updated by the game's tick/increment logic.
	finalTotalPlaytime, err := gs.PlayerPlaytimeStore.GetPlayerPlaytime(ctx, playerUUID)
	if err != nil {
		if _, ok := err.(interface{ IsNil() bool }); ok && err.(interface{ IsNil() bool }).IsNil() { // Check for redis.Nil or custom ErrRedisKeyNotFound
			log.Printf("INFO: Player %s had no recorded playtime in Redis (key non-existent or expired). Persisting 0.0 playtime.", playerUUID)
			finalTotalPlaytime = 0.0 // Default to 0 if no playtime record found
		} else {
			// This is a more critical error (e.g., network issue, Redis corruption)
			return fmt.Errorf("failed to retrieve final total playtime for player %s from Redis: %w", playerUUID, err)
		}
	} else {
		log.Printf("Service: Player %s final total playtime from Redis: %.2f seconds.", playerUUID, finalTotalPlaytime)
	}

	// 2. Persist the final accumulated total playtime to the Player Service (MongoDB).
	// This is the authoritative save operation.
	err = gs.PlayerServiceClient.UpdatePlayerPlaytime(ctx, playerUUID, finalTotalPlaytime)
	if err != nil {
		// Log the error but continue with Redis cleanup. Persistence should ideally
		// have a robust retry/dead-letter queue mechanism for critical data.
		log.Printf("ERROR: Failed to persist player %s total playtime (%.2f) to Player Service (MongoDB): %v", playerUUID, finalTotalPlaytime, err)
		// Optionally, decide if this error should block further cleanup or be retried.
		// For now, we'll continue cleanup to free up Redis resources.
	} else {
		log.Printf("Service: Player %s total playtime (%.2f) successfully persisted to Player Service (MongoDB).", playerUUID, finalTotalPlaytime)
	}

	// 3. Clean up all player-specific keys in Redis.
	// These keys will be re-set when the player comes online next.
	keysToDelete := []string{
		fmt.Sprintf(redisu.OnlineKeyPrefix, playerUUID),        // Marks player online status
		fmt.Sprintf(redisu.PlaytimeKeyPrefix, playerUUID),      // Player's total accumulated playtime in Redis cache
		fmt.Sprintf(redisu.DeltaPlaytimeKeyPrefix, playerUUID), // Player's current session delta playtime
		fmt.Sprintf(redisu.PlayerTeamKeyPrefix, playerUUID),    // Player's assigned team ID
		// Add any other player-specific keys that should be ephemeral per session
	}

	// Use a pipeline for atomic deletion of multiple keys if they are in the same slot,
	// or simply `Del` them if they might be in different slots (Redis Cluster handles this).
	// In Redis Cluster, `DEL` can take multiple keys across slots.
	deletedCount, err := gs.RedisClient.Del(ctx, keysToDelete...).Result()
	if err != nil {
		// This is a significant error during cleanup.
		return fmt.Errorf("failed to delete all player %s related keys from Redis: %w", playerUUID, err)
	}
	log.Printf("Service: Cleaned up %d Redis keys for player %s.", deletedCount, playerUUID)

	log.Printf("Service: Player %s is now fully offline and Redis keys cleaned.", playerUUID)
	return nil
}

// RefreshPlayerOnlineStatus updates the TTL for a player's online status.
func (gs *GameService) RefreshPlayerOnlineStatus(ctx context.Context, playerUUID string) error {
	// This simply calls the store to refresh the TTL. No complex logic needed here.
	err := gs.OnlinePlayersStore.RefreshPlayerOnlineStatus(ctx, playerUUID)
	if err != nil {
		if err == redis.Nil {
			// Player not found online, maybe they disconnected or TTL expired before refresh
			log.Printf("Service: Player %s not found in online sessions during refresh. May need to go online again.", playerUUID)
			return nil // Consider this not an error for a refresh operation
		}
		return fmt.Errorf("failed to refresh online status for player %s: %w", playerUUID, err)
	}
	return nil
}

// GetPlayerTotalPlaytime retrieves a player's total accumulated playtime from Redis.
func (gs *GameService) GetPlayerTotalPlaytime(ctx context.Context, playerUUID string) (float64, error) {
	playtime, err := gs.PlayerPlaytimeStore.GetPlayerPlaytime(ctx, playerUUID) // Calls Redis-only store
	if err != nil {
		return 0, fmt.Errorf("failed to get total playtime for player %s from Redis: %w", playerUUID, err)
	}
	return playtime, nil
}

// GetPlayerDeltaPlaytime retrieves a player's last session's playtime (delta) from Redis.
func (gs *GameService) GetPlayerDeltaPlaytime(ctx context.Context, playerUUID string) (float64, error) {
	deltatime, err := gs.PlayerPlaytimeStore.GetPlayerDeltaPlaytime(ctx, playerUUID) // Calls Redis-only store
	if err != nil {
		// As per requirement, return 1.0 with no error if key not found (or any other error)
		log.Printf("Warning: Could not retrieve delta playtime for %s: %v. Returning default 1.0.", playerUUID, err)
		return 1.0, nil
	}
	return deltatime, nil
}

// GetTeamTotalPlaytime retrieves the total playtime for a given team from Redis.
func (gs *GameService) GetTeamTotalPlaytime(ctx context.Context, teamID string) (float64, error) {
	totalPlaytime, err := gs.TeamPlaytimeStore.GetTeamPlaytime(ctx, teamID) // Calls Redis-only store
	if err != nil {
		return 0, fmt.Errorf("failed to get total playtime for team %s from Redis: %w", teamID, err)
	}
	return totalPlaytime, nil
}

// IsPlayerOnline checks if a player is currently marked as online in Redis.
func (gs *GameService) IsPlayerOnline(ctx context.Context, playerUUID string) (bool, error) {
	isOnline, err := gs.OnlinePlayersStore.IsPlayerOnline(ctx, playerUUID) // Calls Redis-only store
	if err != nil {
		return false, fmt.Errorf("failed to check online status for player %s: %w", playerUUID, err)
	}
	return isOnline, nil
}

// BanPlayer bans a player for a specified duration or permanently.
// It also attempts to force the player offline if they are currently online.
func (gs *GameService) BanPlayer(ctx context.Context, playerUUID string, expiresAt *time.Time, reason string) error {
	err := gs.BanStore.BanPlayer(ctx, playerUUID, expiresAt, reason) // Assumed Redis-only BanStore
	if err != nil {
		return fmt.Errorf("failed to ban player %s: %w", playerUUID, err)
	}
	log.Printf("Service: Player %s banned. Reason: %s, Expires: %v", playerUUID, reason, expiresAt)

	// If the player is currently online, mark them offline immediately
	isOnline, err := gs.OnlinePlayersStore.IsPlayerOnline(ctx, playerUUID)
	if err != nil {
		log.Printf("Warning: Could not check online status for %s after ban: %v", playerUUID, err)
	} else if isOnline {
		log.Printf("Player %s was online and is now forced offline due to ban.", playerUUID)
		// Calling PlayerOffline to ensure playtime is saved to Redis and session cleared
		if err := gs.PlayerOffline(ctx, playerUUID); err != nil {
			log.Printf("Warning: Failed to force player %s offline after ban: %v", playerUUID, err)
		}
	}
	return nil
}

// UnbanPlayer removes a ban from a player.
func (gs *GameService) UnbanPlayer(ctx context.Context, playerUUID string) error {
	err := gs.BanStore.UnbanPlayer(ctx, playerUUID) // Assumed Redis-only BanStore
	if err != nil {
		return fmt.Errorf("failed to unban player %s: %w", playerUUID, err)
	}
	log.Printf("Service: Player %s unbanned.", playerUUID)
	return nil
}
