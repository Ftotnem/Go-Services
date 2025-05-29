package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Ftotnem/GO-SERVICES/game/store"
	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis"
	playerserviceclient "github.com/Ftotnem/GO-SERVICES/shared/service"
	"github.com/redis/go-redis/v9"
)

// GameService holds references to the data stores and other dependencies needed
// for game-related business logic. This service now primarily interacts with Redis
// for real-time, in-session data, and delegates long-term persistence
// to other microservices (e.g., Player Service, Team Stats Service) via periodic updates.
type GameService struct {
	PlayerPlaytimeStore *store.PlayerPlaytimeStore               // For managing player playtime in Redis
	OnlinePlayersStore  *store.OnlinePlayersStore                // For managing online status and delta playtime in Redis
	TeamPlaytimeStore   *store.TeamPlaytimeStore                 // For managing team total playtimes in Redis
	BanStore            *store.BanStore                          // For managing player bans in Redis
	RedisClient         *redis.ClusterClient                     // Direct Redis client for player team lookup
	PlayerServiceClient *playerserviceclient.PlayerServiceClient // <--- Add this!
}

// NewGameService is the constructor for GameService.
// It now only accepts Redis-backed stores.
func NewGameService(
	playerPlaytimeStore *store.PlayerPlaytimeStore,
	onlinePlayersStore *store.OnlinePlayersStore,
	teamPlaytimeStore *store.TeamPlaytimeStore,
	banStore *store.BanStore,
	redisClient *redis.ClusterClient, // Pass the main Redis client for direct key lookups
	PlayerServiceClient *playerserviceclient.PlayerServiceClient,
) *GameService {
	return &GameService{
		PlayerPlaytimeStore: playerPlaytimeStore,
		OnlinePlayersStore:  onlinePlayersStore,
		TeamPlaytimeStore:   teamPlaytimeStore,
		BanStore:            banStore,
		RedisClient:         redisClient,         // Store the Redis client for general use
		PlayerServiceClient: PlayerServiceClient, // <--- Assign it!
	}
}

// HandlePlayerOnline marks a player as online and records their session start time.
func (gs *GameService) HandlePlayerOnline(ctx context.Context, playerUUID string) error {
	// 1. Check if player is banned
	isBanned, err := gs.BanStore.IsPlayerBanned(ctx, playerUUID)
	if err != nil {
		return fmt.Errorf("failed to check ban status for player %s: %w", playerUUID, err)
	}
	if isBanned {
		return fmt.Errorf("player %s is currently banned and cannot go online", playerUUID)
	}

	// 2. Set player online in Redis (e.g., store session start time)
	err = gs.OnlinePlayersStore.SetPlayerOnline(ctx, playerUUID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set player %s online in Redis: %w", playerUUID, err)
	}
	log.Printf("Service: Player %s marked online.", playerUUID)

	// 3. Initialize delta playtime - CHECK FOR ERRORS
	err = gs.PlayerPlaytimeStore.SetPlayerDeltaPlaytime(ctx, playerUUID, 1)
	if err != nil {
		return fmt.Errorf("failed to set delta playtime for player %s: %w", playerUUID, err)
	}

	// 4. Initialize total playtime - CHECK FOR ERRORS
	err = gs.PlayerPlaytimeStore.SetPlayerPlaytime(ctx, playerUUID, 1)
	if err != nil {
		return fmt.Errorf("failed to set playtime for player %s: %w", playerUUID, err)
	}

	return nil
}

// HandlePlayerOffline marks a player as offline, calculates playtime, and updates Redis.
// The actual persistence to a Player Microservice will happen via a separate, periodic job.
func (gs *GameService) HandlePlayerOffline(ctx context.Context, playerUUID string) error {
	// 1. Get session start time from Redis
	sessionStartTime, err := gs.OnlinePlayersStore.GetPlayerOnlineTime(ctx, playerUUID)
	if err != nil {
		// If not found, player was not properly marked online or already offline.
		log.Printf("Service: Player %s not found in online sessions or already offline. Skipping offline handling.", playerUUID)
		return nil // Not a critical error for this flow
	}

	// 2. Calculate playtime for this session
	sessionPlaytime := time.Since(sessionStartTime).Seconds()
	log.Printf("Service: Player %s was online for %.2f seconds.", playerUUID, sessionPlaytime)

	// 4. Update team total playtime in Redis
	// First, retrieve the player's team ID from Redis.
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

	// 6. Store delta playtime for short-term retrieval (e.g., for real-time leaderboards or next sync)
	err = gs.PlayerPlaytimeStore.SetPlayerDeltaPlaytime(ctx, playerUUID, sessionPlaytime)
	if err != nil {
		log.Printf("Warning: Failed to set delta playtime for player %s: %v", playerUUID, err)
	}

	log.Printf("Service: Player %s marked offline. Playtime updated in Redis.", playerUUID)
	return nil
}

// GetPlayerTotalPlaytime retrieves a player's total accumulated playtime from Redis.
// This is the current, in-session total, not necessarily the long-term persisted total.
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
		return 1.0, fmt.Errorf("failed to get delta playtime for player %s from Redis: %w", playerUUID, err)
	}
	return deltatime, nil
}

// GetTeamTotalPlaytime retrieves the total playtime for a given team from Redis.
// This is the current, in-session total, not necessarily the long-term persisted total.
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
	// If the player is currently online, mark them offline immediately
	isOnline, err := gs.OnlinePlayersStore.IsPlayerOnline(ctx, playerUUID)
	if err != nil {
		log.Printf("Warning: Could not check online status for %s after ban: %v", playerUUID, err)
	} else if isOnline {
		log.Printf("Player %s was online and is now forced offline due to ban.", playerUUID)
		// Calling HandlePlayerOffline to ensure playtime is saved to Redis and session cleared
		if err := gs.HandlePlayerOffline(ctx, playerUUID); err != nil {
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
	return nil
}
