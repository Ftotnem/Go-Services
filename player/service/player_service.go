// player/service/player_service.go
package service

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/Ftotnem/GO-SERVICES/player/mojang"
	"github.com/Ftotnem/GO-SERVICES/player/store"
	"github.com/Ftotnem/GO-SERVICES/shared/models"
	"go.mongodb.org/mongo-driver/mongo" // For checking specific MongoDB errors
)

// Custom Errors for clear communication to API layer
var (
	ErrProfileAlreadyExists = fmt.Errorf("player profile already exists")
	ErrProfileNotFound      = fmt.Errorf("player profile not found")
	ErrTeamNotFound         = fmt.Errorf("team not found")
)

// PlayerService encapsulates the business logic for player profiles.
type PlayerService struct {
	playerStore   *store.PlayerStore
	teamStore     *store.TeamStore
	mojangService *mojang.MojangService // Dependency on MojangService
}

// NewPlayerService creates a new PlayerService instance.
func NewPlayerService(ps *store.PlayerStore, ts *store.TeamStore, ms *mojang.MojangService) *PlayerService {
	return &PlayerService{
		playerStore:   ps,
		teamStore:     ts,
		mojangService: ms,
	}
}

// CreateProfile handles the creation of a new player profile, including team assignment and username lookup.
func (ps *PlayerService) CreateProfile(ctx context.Context, playerUUID string) (*models.Player, error) {
	now := time.Now()

	// --- Team Assignment Logic (from your original code) ---
	// This business logic belongs in the service layer.
	allTeams, err := ps.teamStore.GetAllTeams(ctx) // Get all teams from store
	if err != nil {
		log.Printf("ERROR: Could not retrieve all teams for assignment: %v. Proceeding with random assignment fallback.", err)
		// Fallback to hardcoded list if DB call fails
		allTeams = []models.Team{{Name: "AQUA_CREEPERS"}, {Name: "PURPLE_SWORDERS"}} // Fallback teams
	}

	var assignedTeamName string
	minPlayers := int64(-1)
	leastPopulatedTeams := []string{}

	if len(allTeams) > 0 {
		teamCounts := make(map[string]int64)
		for _, team := range allTeams {
			count, err := ps.teamStore.GetTeamPlayerCount(ctx, team.Name)
			if err != nil {
				log.Printf("WARN: Could not retrieve player count for team %s: %v. Skipping for least populated calculation.", team.Name, err)
				teamCounts[team.Name] = -1 // Mark as error
			} else {
				teamCounts[team.Name] = count
			}
		}

		for _, team := range allTeams {
			count := teamCounts[team.Name]
			if count == -1 {
				continue
			} // Skip errored teams

			if minPlayers == -1 || count < minPlayers {
				minPlayers = count
				leastPopulatedTeams = []string{team.Name}
			} else if count == minPlayers {
				leastPopulatedTeams = append(leastPopulatedTeams, team.Name)
			}
		}
	}

	if len(leastPopulatedTeams) > 0 {
		assignedTeamName = leastPopulatedTeams[rand.Intn(len(leastPopulatedTeams))]
		log.Printf("INFO: Assigned player %s to team %s (least populated).", playerUUID, assignedTeamName)
	} else {
		// Fallback: if no teams or all failed, assign randomly from hardcoded list
		log.Printf("WARN: No valid teams found or all failed to get count. Assigning player %s to random fallback team.", playerUUID)
		fallbackTeams := []string{"AQUA_CREEPERS", "PURPLE_SWORDERS"} // Ensure these are also in your EnsureTeamsExist
		assignedTeamName = fallbackTeams[rand.Intn(len(fallbackTeams))]
	}
	// --- End Team Assignment Logic ---

	newProfile := &models.Player{
		UUID:               playerUUID,
		Username:           "", // Placeholder, will be filled by Mojang API asynchronously
		Team:               assignedTeamName,
		TotalPlaytimeTicks: 0.0,
		DeltaPlaytimeTicks: 1.0,
		Banned:             false,
		CreatedAt:          &now,
		LastLoginAt:        &now,
	}

	err = ps.playerStore.CreatePlayer(ctx, newProfile) // Call the store method
	if err != nil {
		if err.Error() == fmt.Sprintf("player profile %s already exists", playerUUID) {
			return nil, ErrProfileAlreadyExists // Return custom error
		}
		return nil, fmt.Errorf("service failed to create player profile: %w", err)
	}

	// Increment the player count for the assigned team (async is fine here)
	go func(team string) {
		teamCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ps.teamStore.IncrementTeamPlayerCount(teamCtx, team); err != nil {
			log.Printf("ERROR: Failed to increment player count for team %s after creating profile %s: %v", team, playerUUID, err)
		}
	}(assignedTeamName)

	// Asynchronously fetch username for the newly created profile
	go func(uuid string) {
		mojangCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		username, mojangErr := ps.mojangService.GetUsernameByUUID(mojangCtx, uuid) // Use MojangService
		if mojangErr != nil {
			log.Printf("WARN: Failed to fetch username from Mojang for UUID %s: %v", uuid, mojangErr)
			return
		}

		updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer updateCancel()

		if updateErr := ps.playerStore.UpdatePlayerUsername(updateCtx, uuid, username); updateErr != nil { // Call store method
			log.Printf("WARN: Failed to update username for player profile %s in DB: %v", uuid, updateErr)
		} else {
			log.Printf("INFO: Successfully updated username for player profile %s to %s.", uuid, username)
			// No need to update newProfile here as response is already sent
		}
	}(playerUUID)

	return newProfile, nil
}

// GetProfile retrieves a player's profile.
func (ps *PlayerService) GetProfile(ctx context.Context, uuid string) (*models.Player, error) {
	profile, err := ps.playerStore.GetPlayerByUUID(ctx, uuid)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrProfileNotFound // Return custom error
		}
		return nil, fmt.Errorf("service failed to get player profile: %w", err)
	}
	// It's generally a good practice to update the last login on a specific login event,
	// rather than every GET. If this is a login event, then update last login here.
	// We'll keep the async update from your original code for now, but consider moving it.
	go func() {
		updateCtx, updateCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer updateCancel()
		if err := ps.playerStore.UpdatePlayerLastLogin(updateCtx, uuid); err != nil {
			log.Printf("WARN: Failed to update last login for player profile %s: %v", uuid, err)
		}
	}()
	return profile, nil
}

// UpdateProfilePlaytime updates a player's total playtime.
func (ps *PlayerService) UpdateProfilePlaytime(ctx context.Context, uuid string, ticksToSet float64) error {
	err := ps.playerStore.UpdatePlayerPlaytime(ctx, uuid, ticksToSet)
	if err != nil {
		if err.Error() == fmt.Sprintf("player %s not found for playtime update", uuid) { // Check specific store error
			return ErrProfileNotFound
		}
		return fmt.Errorf("service failed to update player playtime: %w", err)
	}
	return nil
}

// UpdateProfileDeltaPlaytime updates a player's delta playtime.
func (ps *PlayerService) UpdateProfileDeltaPlaytime(ctx context.Context, uuid string, ticksToSet float64) error {
	err := ps.playerStore.UpdatePlayerDeltaPlaytime(ctx, uuid, ticksToSet)
	if err != nil {
		if err.Error() == fmt.Sprintf("player %s not found for delta playtime update", uuid) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("service failed to update player delta playtime: %w", err)
	}
	return nil
}

// UpdateProfileBanStatus updates a player's ban status.
func (ps *PlayerService) UpdateProfileBanStatus(ctx context.Context, uuid string, banned bool, banExpiresAt *time.Time) error {
	err := ps.playerStore.UpdatePlayerBanStatus(ctx, uuid, banned, banExpiresAt)
	if err != nil {
		if err.Error() == fmt.Sprintf("player %s not found for ban status update", uuid) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("service failed to update player ban status: %w", err)
	}
	return nil
}

// UpdateProfileLastLogin updates a player's last login timestamp.
func (ps *PlayerService) UpdateProfileLastLogin(ctx context.Context, uuid string) error {
	err := ps.playerStore.UpdatePlayerLastLogin(ctx, uuid)
	if err != nil {
		if err.Error() == fmt.Sprintf("player %s not found for last login update", uuid) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("service failed to update player last login: %w", err)
	}
	return nil
}
