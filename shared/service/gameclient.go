// File: github.com/Ftotnem/Backend/go/shared/service/gameclient.go
package service

import (
	"context"
	"fmt"

	"github.com/Ftotnem/GO-SERVICES/shared/api"
)

// GameServiceClient is a client for the Game Service.
// It interacts with the Game Service's HTTP API.
type GameServiceClient struct {
	apiClient *api.Client
}

// NewGameClient creates a new Game Service client.
// It initializes the underlying API client with the Game Service's base URL.
func NewGameClient(baseURL string) *GameServiceClient {
	return &GameServiceClient{
		apiClient: api.NewClient(baseURL, api.NewDefaultHTTPClient()),
	}
}

// --- Request/Response DTOs for Game Service Communication ---
// These mirror the DTOs defined in your game/api/handlers.go for consistency.

// PlayerUUIDRequest is a general structure for requests that only need a player UUID.
type PlayerUUIDRequest struct {
	UUID string `json:"uuid"`
}

// BanRequest is the structure for the request body for banning.
type BanRequest struct {
	UUID        string `json:"uuid"`
	DurationSec int64  `json:"duration_seconds"` // Duration in seconds. 0 for permanent.
	Reason      string `json:"reason,omitempty"`
}

// PlaytimeResponse is the structure for the JSON response for playtime requests.
type PlaytimeResponse struct {
	Playtime float64 `json:"playtime"`
}

// DeltaPlaytimeResponse is the structure for the JSON response for delta playtime requests.
type DeltaPlaytimeResponse struct {
	Deltatime float64 `json:"deltatime"`
}

// TeamTotalPlaytimeResponse defines the structure for the JSON response for a single team's total playtime.
type TeamTotalPlaytimeResponse struct {
	TeamID        string  `json:"teamId"`
	TotalPlaytime float64 `json:"totalPlaytime"`
}

// PlayerOnlineStatusResponse defines the structure for the JSON response for player online status.
type PlayerOnlineStatusResponse struct {
	UUID     string `json:"uuid"`
	IsOnline bool   `json:"isOnline"`
}

// BanResponse is the structure for the JSON response after a ban operation.
type BanResponse struct {
	Message     string `json:"message"`
	UUID        string `json:"uuid"`
	ExpiresAt   int64  `json:"expires_at,omitempty"` // Unix timestamp, 0 for permanent
	IsPermanent bool   `json:"is_permanent"`
}

// --- Client Methods for Game Service API Endpoints ---

// PlayerOnline sends a POST request to mark a player as online and load their data.
// Corresponds to POST /game/player/online.
func (c *GameServiceClient) PlayerOnline(ctx context.Context, playerUUID string) error {
	reqData := PlayerUUIDRequest{
		UUID: playerUUID,
	}
	// The Game Service responds with a simple message, so we expect nil for the response target.
	return c.apiClient.Post(ctx, "/game/player/online", reqData, nil)
}

// PlayerOffline sends a POST request to mark a player as offline and persist playtime.
// Corresponds to POST /game/player/offline.
func (c *GameServiceClient) PlayerOffline(ctx context.Context, playerUUID string) error {
	reqData := PlayerUUIDRequest{
		UUID: playerUUID,
	}
	// The Game Service responds with a simple message, so we expect nil for the response target.
	return c.apiClient.Post(ctx, "/game/player/offline", reqData, nil)
}

// RefreshPlayerOnlineStatus sends a POST request to refresh a player's online status (heartbeat).
// Corresponds to POST /game/player/refresh-online.
func (c *GameServiceClient) RefreshPlayerOnlineStatus(ctx context.Context, playerUUID string) error {
	reqData := PlayerUUIDRequest{
		UUID: playerUUID,
	}
	// The Game Service responds with a simple message, so we expect nil for the response target.
	return c.apiClient.Post(ctx, "/game/player/refresh-online", reqData, nil)
}

// GetPlayerTotalPlaytime sends a GET request to retrieve a player's total playtime.
// Corresponds to GET /game/player/{uuid}/playtime.
func (c *GameServiceClient) GetPlayerTotalPlaytime(ctx context.Context, playerUUID string) (*PlaytimeResponse, error) {
	resp := &PlaytimeResponse{}
	err := c.apiClient.Get(ctx, fmt.Sprintf("/game/player/%s/playtime", playerUUID), resp)
	if err != nil {
		return nil, fmt.Errorf("failed to get total playtime for player %s: %w", playerUUID, err)
	}
	return resp, nil
}

// GetPlayerDeltaPlaytime sends a GET request to retrieve a player's delta playtime.
// Corresponds to GET /game/player/{uuid}/deltatime.
func (c *GameServiceClient) GetPlayerDeltaPlaytime(ctx context.Context, playerUUID string) (*DeltaPlaytimeResponse, error) {
	resp := &DeltaPlaytimeResponse{}
	err := c.apiClient.Get(ctx, fmt.Sprintf("/game/player/%s/deltatime", playerUUID), resp)
	if err != nil {
		return nil, fmt.Errorf("failed to get delta playtime for player %s: %w", playerUUID, err)
	}
	return resp, nil
}

// GetTeamTotalPlaytime sends a GET request to retrieve the total playtime for a specific team.
// Corresponds to GET /game/team/{teamId}/playtime.
func (c *GameServiceClient) GetTeamTotalPlaytime(ctx context.Context, teamID string) (*TeamTotalPlaytimeResponse, error) {
	resp := &TeamTotalPlaytimeResponse{}
	err := c.apiClient.Get(ctx, fmt.Sprintf("/game/team/%s/playtime", teamID), resp)
	if err != nil {
		return nil, fmt.Errorf("failed to get total playtime for team %s: %w", teamID, err)
	}
	return resp, nil
}

// GetPlayerOnlineStatus sends a GET request to check a player's online status.
// Corresponds to GET /game/player/{uuid}/is-online.
func (c *GameServiceClient) GetPlayerOnlineStatus(ctx context.Context, playerUUID string) (*PlayerOnlineStatusResponse, error) {
	resp := &PlayerOnlineStatusResponse{}
	err := c.apiClient.Get(ctx, fmt.Sprintf("/game/player/%s/is-online", playerUUID), resp)
	if err != nil {
		return nil, fmt.Errorf("failed to get online status for player %s: %w", playerUUID, err)
	}
	return resp, nil
}

// BanPlayer sends a POST request to ban a player.
// Corresponds to POST /game/admin/ban.
func (c *GameServiceClient) BanPlayer(ctx context.Context, playerUUID string, durationSec int64, reason string) (*BanResponse, error) {
	reqData := BanRequest{
		UUID:        playerUUID,
		DurationSec: durationSec,
		Reason:      reason,
	}
	resp := &BanResponse{}
	err := c.apiClient.Post(ctx, "/game/admin/ban", reqData, resp)
	if err != nil {
		return nil, fmt.Errorf("failed to ban player %s: %w", playerUUID, err)
	}
	return resp, nil
}

// UnbanPlayer sends a POST request to unban a player.
// Corresponds to POST /game/admin/unban.
func (c *GameServiceClient) UnbanPlayer(ctx context.Context, playerUUID string) error {
	reqData := PlayerUUIDRequest{ // Re-use PlayerUUIDRequest as it only needs UUID
		UUID: playerUUID,
	}
	// The Game Service responds with a simple message, so we expect nil for the response target.
	return c.apiClient.Post(ctx, "/game/admin/unban", reqData, nil)
}
