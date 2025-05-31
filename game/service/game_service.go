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

// PlayerOffline marks a player as offline, calculates playtime, updates Redis, and triggers persistence.
func (gs *GameService) PlayerOffline(ctx context.Context, playerUUID string) error {
	// 1. Get session start time from Redis
	sessionStartTime, err := gs.OnlinePlayersStore.GetPlayerOnlineTime(ctx, playerUUID)
	if err != nil {
		if err == redis.Nil {
			log.Printf("Service: Player %s not found in online sessions or already offline. Skipping offline handling.", playerUUID)
			return nil // Not a critical error for this flow
		}
		return fmt.Errorf("failed to get player %s online start time from Redis: %w", playerUUID, err)
	}

	// 2. Calculate playtime for this session
	sessionPlaytime := time.Since(sessionStartTime).Seconds()
	log.Printf("Service: Player %s was online for %.2f seconds.", playerUUID, sessionPlaytime)

	// 3. Update player's total playtime and delta playtime in Redis
	// Get current total playtime to increment it
	currentTotalPlaytime, err := gs.PlayerPlaytimeStore.GetPlayerPlaytime(ctx, playerUUID)
	if err != nil && err != redis.Nil { // It might be nil if player was not fully initialized, handle gracefully
		log.Printf("Warning: Failed to get current total playtime for %s: %v. Proceeding with delta.", playerUUID, err)
		currentTotalPlaytime = 0 // Default to 0 if an error (not nil) occurs.
	}
	newTotalPlaytime := currentTotalPlaytime + sessionPlaytime
	err = gs.PlayerPlaytimeStore.SetPlayerPlaytime(ctx, playerUUID, newTotalPlaytime)
	if err != nil {
		log.Printf("Warning: Failed to update total playtime for player %s in Redis: %v", playerUUID, err)
	}

	err = gs.PlayerPlaytimeStore.SetPlayerDeltaPlaytime(ctx, playerUUID, sessionPlaytime)
	if err != nil {
		log.Printf("Warning: Failed to set delta playtime for player %s in Redis: %v", playerUUID, err)
	}

	// 4. Update team total playtime in Redis
	playerTeamKey := fmt.Sprintf(redisu.PlayerTeamKeyPrefix, playerUUID)
	teamID, err := gs.RedisClient.Get(ctx, playerTeamKey).Result()
	if err == redis.Nil {
		log.Printf("Warning: Player %s has no team assigned in Redis (key %s). Cannot increment team playtime.", playerUUID, playerTeamKey)
	} else if err != nil {
		log.Printf("Warning: Failed to get team ID for player %s from Redis: %v", playerUUID, err)
	} else {
		err = gs.TeamPlaytimeStore.IncrementTeamPlaytime(ctx, teamID, sessionPlaytime)
		if err != nil {
			log.Printf("Warning: Failed to increment team %s playtime in Redis for player %s: %v", teamID, playerUUID, err)
		}
	}

	// 5. Remove player from online status in Redis
	err = gs.OnlinePlayersStore.RemovePlayerOnline(ctx, playerUUID)
	if err != nil {
		return fmt.Errorf("failed to remove player %s from online status in Redis: %w", playerUUID, err)
	}

	// 6. Persist updated total playtime to Player Service (MongoDB)
	err = gs.PlayerServiceClient.UpdatePlayerPlaytime(ctx, playerUUID, newTotalPlaytime)
	if err != nil {
		log.Printf("Error: Failed to persist player %s total playtime to Player Service: %v", playerUUID, err)
		// Consider implementing a retry mechanism or a dead-letter queue here
	}

	// 7. Clean up other player-specific keys in Redis (e.g., team key)
	if err = gs.RedisClient.Del(ctx, playerTeamKey).Err(); err != nil {
		log.Printf("Warning: Failed to delete player team key %s from Redis: %v", playerTeamKey, err)
	}
	// Add other keys related to player session if they exist and need cleanup
	// E.g., if there were specific in-game session data stored with a UUID prefix

	log.Printf("Service: Player %s marked offline. Playtime updated and data persisted to DB.", playerUUID)
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
