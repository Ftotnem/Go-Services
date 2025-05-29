// shared/service/player.go (Revised)
package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Ftotnem/GO-SERVICES/shared/api"
	"github.com/Ftotnem/GO-SERVICES/shared/models"
	"github.com/google/uuid" // Use standard google/uuid
)

// PlayerServiceClient is a client for the Player Data Service.
type PlayerServiceClient struct {
	apiClient *api.Client
}

// NewPlayerClient creates a new Player Data Service client.
func NewPlayerClient(baseURL string) *PlayerServiceClient {
	// Pass the default HTTP client for inter-service communication
	return &PlayerServiceClient{
		apiClient: api.NewClient(baseURL, api.NewDefaultHTTPClient()),
	}
}

// UpdateBanStatusRequest is the structure for the request body for updating ban status.
// This mirrors the UpdateBanStatusRequest in your player-service.
type UpdateBanStatusRequest struct {
	Banned       bool       `json:"banned"`
	BanExpiresAt *time.Time `json:"banExpiresAt"`
}

// UpdatePlaytimeRequest is the structure for updating playtime.
type UpdatePlaytimeRequest struct {
	TicksToSet float64 `json:"ticksToSet"` // Matches the server-side field name
}

// UpdateDeltaPlaytimeRequest is the structure for updating delta playtime.
type UpdateDeltaPlaytimeRequest struct {
	TicksToSet float64 `json:"ticksToSet"` // Matches the server-side field name
}

// CreateProfileRequest is the structure for creating a new player profile.
// This directly maps to the server's CreateProfileRequest.
type CreateProfileRequest struct {
	UUID string `json:"uuid"`
}

// SyncPlayerPlaytimeResponse defines the expected response structure from the player service's sync endpoint.
type SyncPlayerPlaytimeResponse struct {
	TeamTotals map[string]float64 `json:"teamTotals"` // Map of teamID to calculated total playtime
	Message    string             `json:"message"`
}

// GetProfile fetches a player's profile by UUID.
// GET /profiles/{uuid}
// Returns *models.Player if found, nil and error if not found or other issue.
// Specifically returns api.ErrNotFound if the profile does not exist (HTTP 404).
func (c *PlayerServiceClient) GetProfile(ctx context.Context, playerUUID uuid.UUID) (*models.Player, error) {
	profile := &models.Player{}
	err := c.apiClient.Get(ctx, fmt.Sprintf("/profiles/%s", playerUUID.String()), profile)
	if err != nil {
		// Check if the error indicates a 404 Not Found
		if apiErr, ok := err.(*api.HTTPError); ok && apiErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: player profile %s", api.ErrNotFound, playerUUID.String()) // Wrap with a specific error
		}
		return nil, fmt.Errorf("failed to get player profile %s: %w", playerUUID.String(), err)
	}
	return profile, nil
}

// UpdateProfileBanStatus sends a PUT request to update a player profile's ban status.
// PUT /profiles/{uuid}/ban
func (c *PlayerServiceClient) UpdateProfileBanStatus(ctx context.Context, playerUUID uuid.UUID, banned bool, banExpiresAt *time.Time) error {
	reqData := UpdateBanStatusRequest{
		Banned:       banned,
		BanExpiresAt: banExpiresAt,
	}
	return c.apiClient.Put(ctx, fmt.Sprintf("/profiles/%s/ban", playerUUID.String()), reqData, nil)
}

// UpdateProfileLastLogin sends a PUT request to update a player profile's last login timestamp.
// PUT /profiles/{uuid}/lastlogin
func (c *PlayerServiceClient) UpdateProfileLastLogin(ctx context.Context, playerUUID uuid.UUID) error {
	// No request body is needed for this endpoint as the server generates the timestamp.
	return c.apiClient.Put(ctx, fmt.Sprintf("/profiles/%s/lastlogin", playerUUID.String()), nil, nil)
}

// UpdateProfilePlaytime sends a PUT request to update a player profile's total playtime.
// PUT /profiles/{uuid}/playtime
func (c *PlayerServiceClient) UpdateProfilePlaytime(ctx context.Context, playerUUID uuid.UUID, playtimeTicks float64) error {
	reqData := UpdatePlaytimeRequest{
		TicksToSet: playtimeTicks,
	}
	return c.apiClient.Put(ctx, fmt.Sprintf("/profiles/%s/playtime", playerUUID.String()), reqData, nil)
}

// UpdateProfileDeltaPlaytime sends a PUT request to update a player profile's delta playtime.
// PUT /profiles/{uuid}/deltaplaytime
func (c *PlayerServiceClient) UpdateProfileDeltaPlaytime(ctx context.Context, playerUUID uuid.UUID, deltaPlaytimeTicks float64) error {
	reqData := UpdateDeltaPlaytimeRequest{
		TicksToSet: deltaPlaytimeTicks,
	}
	return c.apiClient.Put(ctx, fmt.Sprintf("/profiles/%s/deltaplaytime", playerUUID.String()), reqData, nil)
}

// SyncPlayerPlaytime triggers the player service to synchronize playtime data from Redis to MongoDB
// and also returns the aggregated team totals.
// POST /player/sync-playtime
func (c *PlayerServiceClient) SyncPlayerPlaytime(ctx context.Context) (*SyncPlayerPlaytimeResponse, error) {
	// apiClient.Post expects the path relative to the baseURL, a request body (nil if none),
	// and a response target.
	var resp SyncPlayerPlaytimeResponse
	err := c.apiClient.Post(ctx, "/teams/sync-totals", nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to trigger player service sync playtime: %w", err)
	}
	return &resp, nil
}
