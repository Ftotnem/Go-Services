// shared/service/player.go
package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Ftotnem/GO-SERVICES/shared/api"
	"github.com/Ftotnem/GO-SERVICES/shared/models" // This should contain your Player model
	"github.com/google/uuid"                       // Use standard google/uuid
)

// PlayerServiceClient is a client for the Player Data Service.
// It uses an internal apiClient to make HTTP requests to the Player Service.
type PlayerServiceClient struct {
	apiClient *api.Client
}

// NewPlayerClient creates a new Player Data Service client.
// It takes the base URL of the Player Service as an argument.
func NewPlayerClient(baseURL string) *PlayerServiceClient {
	// Pass the default HTTP client for inter-service communication
	return &PlayerServiceClient{
		apiClient: api.NewClient(baseURL, api.NewDefaultHTTPClient()),
	}
}

// --- Request/Response DTOs for Player Service Communication ---
// These mirror the DTOs defined in your player/api/handlers.go for consistency.

// UpdateBanStatusRequest is the structure for the request body when updating ban status.
type UpdateBanStatusRequest struct {
	Banned       bool       `json:"banned"`
	BanExpiresAt *time.Time `json:"banExpiresAt"`
}

// UpdatePlaytimeRequest is the structure for updating total playtime.
type UpdatePlaytimeRequest struct {
	TicksToSet float64 `json:"ticksToSet"`
}

// UpdateDeltaPlaytimeRequest is the structure for updating delta playtime.
type UpdateDeltaPlaytimeRequest struct {
	TicksToSet float64 `json:"ticksToSet"`
}

// CreateProfileRequest is the structure for creating a new player profile.
type CreateProfileRequest struct {
	UUID string `json:"uuid"`
}

// SyncTeamTotalsResponse defines the expected response structure from the player service's team sync endpoint.
type SyncTeamTotalsResponse struct {
	TeamTotals map[string]float64 `json:"teamTotals"` // Map of teamID to calculated total playtime
	Message    string             `json:"message"`
}

// --- Client Methods for Player Service API Endpoints ---

// GetPlayerProfile fetches a player's profile by UUID.
// It directly calls the Player Service's GET /profiles/{uuid} endpoint.
// Returns *models.Player if found, nil and error if not found or other issue.
// Specifically returns api.ErrNotFound if the profile does not exist (HTTP 404).
func (c *PlayerServiceClient) GetPlayerProfile(ctx context.Context, playerUUID string) (*models.Player, error) {
	parsedUUID, err := uuid.Parse(playerUUID)
	if err != nil {
		return nil, fmt.Errorf("invalid player UUID format: %w", err)
	}

	profile := &models.Player{}
	err = c.apiClient.Get(ctx, fmt.Sprintf("/profiles/%s", parsedUUID.String()), profile)
	if err != nil {
		// Check if the error indicates a 404 Not Found from the Player Service
		if apiErr, ok := err.(*api.HTTPError); ok && apiErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: player profile %s", api.ErrNotFound, playerUUID) // Wrap with a specific error for "not found"
		}
		return nil, fmt.Errorf("failed to get player profile %s from Player Service: %w", playerUUID, err)
	}
	return profile, nil
}

// CreatePlayerProfile sends a POST request to create a new player profile.
// It calls the Player Service's POST /profiles endpoint.
func (c *PlayerServiceClient) CreatePlayerProfile(ctx context.Context, playerUUID string) (*models.Player, error) {
	reqData := CreateProfileRequest{UUID: playerUUID}
	createdProfile := &models.Player{} // Expect the created profile back
	err := c.apiClient.Post(ctx, "/profiles", reqData, createdProfile)
	if err != nil {
		// Handle specific errors like conflict if the profile already exists
		if apiErr, ok := err.(*api.HTTPError); ok && apiErr.StatusCode == http.StatusConflict {
			return nil, fmt.Errorf("%w: player profile %s already exists", api.ErrConflict, playerUUID)
		}
		return nil, fmt.Errorf("failed to create player profile %s in Player Service: %w", playerUUID, err)
	}
	return createdProfile, nil
}

// UpdatePlayerPlaytime sends a PUT request to update a player profile's total playtime.
// It calls the Player Service's PUT /profiles/{uuid}/playtime endpoint.
func (c *PlayerServiceClient) UpdatePlayerPlaytime(ctx context.Context, playerUUID string, playtimeTicks float64) error {
	parsedUUID, err := uuid.Parse(playerUUID)
	if err != nil {
		return fmt.Errorf("invalid player UUID format: %w", err)
	}

	reqData := UpdatePlaytimeRequest{
		TicksToSet: playtimeTicks,
	}
	err = c.apiClient.Put(ctx, fmt.Sprintf("/profiles/%s/playtime", parsedUUID.String()), reqData, nil)
	if err != nil {
		if apiErr, ok := err.(*api.HTTPError); ok && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: player profile %s", api.ErrNotFound, playerUUID)
		}
		return fmt.Errorf("failed to update playtime for player %s in Player Service: %w", playerUUID, err)
	}
	return nil
}

// UpdatePlayerDeltaPlaytime sends a PUT request to update a player profile's delta playtime.
// It calls the Player Service's PUT /profiles/{uuid}/deltaplaytime endpoint.
func (c *PlayerServiceClient) UpdatePlayerDeltaPlaytime(ctx context.Context, playerUUID string, deltaPlaytimeTicks float64) error {
	parsedUUID, err := uuid.Parse(playerUUID)
	if err != nil {
		return fmt.Errorf("invalid player UUID format: %w", err)
	}

	reqData := UpdateDeltaPlaytimeRequest{
		TicksToSet: deltaPlaytimeTicks,
	}
	err = c.apiClient.Put(ctx, fmt.Sprintf("/profiles/%s/deltaplaytime", parsedUUID.String()), reqData, nil)
	if err != nil {
		if apiErr, ok := err.(*api.HTTPError); ok && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: player profile %s", api.ErrNotFound, playerUUID)
		}
		return fmt.Errorf("failed to update delta playtime for player %s in Player Service: %w", playerUUID, err)
	}
	return nil
}

// UpdatePlayerBanStatus sends a PUT request to update a player profile's ban status.
// It calls the Player Service's PUT /profiles/{uuid}/ban endpoint.
func (c *PlayerServiceClient) UpdatePlayerBanStatus(ctx context.Context, playerUUID string, banned bool, banExpiresAt *time.Time) error {
	parsedUUID, err := uuid.Parse(playerUUID)
	if err != nil {
		return fmt.Errorf("invalid player UUID format: %w", err)
	}

	reqData := UpdateBanStatusRequest{
		Banned:       banned,
		BanExpiresAt: banExpiresAt,
	}
	err = c.apiClient.Put(ctx, fmt.Sprintf("/profiles/%s/ban", parsedUUID.String()), reqData, nil)
	if err != nil {
		if apiErr, ok := err.(*api.HTTPError); ok && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: player profile %s", api.ErrNotFound, playerUUID)
		}
		return fmt.Errorf("failed to update ban status for player %s in Player Service: %w", playerUUID, err)
	}
	return nil
}

// UpdatePlayerLastLogin sends a PUT request to update a player profile's last login timestamp.
// It calls the Player Service's PUT /profiles/{uuid}/lastlogin endpoint.
func (c *PlayerServiceClient) UpdatePlayerLastLogin(ctx context.Context, playerUUID string) error {
	parsedUUID, err := uuid.Parse(playerUUID)
	if err != nil {
		return fmt.Errorf("invalid player UUID format: %w", err)
	}

	// No request body is needed for this endpoint as the server generates the timestamp.
	err = c.apiClient.Put(ctx, fmt.Sprintf("/profiles/%s/lastlogin", parsedUUID.String()), nil, nil)
	if err != nil {
		if apiErr, ok := err.(*api.HTTPError); ok && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: player profile %s", api.ErrNotFound, playerUUID)
		}
		return fmt.Errorf("failed to update last login for player %s in Player Service: %w", playerUUID, err)
	}
	return nil
}

// SyncTeamTotals triggers the player service to synchronize playtime data from Redis to MongoDB
// and also returns the aggregated team totals.
// It calls the Player Service's POST /teams/sync-totals endpoint.
func (c *PlayerServiceClient) SyncTeamTotals(ctx context.Context) (*SyncTeamTotalsResponse, error) {
	var resp SyncTeamTotalsResponse
	// The Player Service handler expects a POST to "/teams/sync-totals" with no request body.
	err := c.apiClient.Post(ctx, "/teams/sync-totals", nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to trigger player service team totals sync: %w", err)
	}
	return &resp, nil
}
