// File: github.com/Ftotnem/Backend/go/shared/service/gameclient.go (Revised)
package service

import (
	"context"
	"time"

	"github.com/Ftotnem/GO-SERVICES/shared/api"
	"github.com/google/uuid" // Use standard google/uuid
)

// Client is a client for the Game Service.
type GameServiceClient struct {
	apiClient *api.Client
}

// NewGameClient creates a new Game Service client.
func NewGameClient(baseURL string) *GameServiceClient {
	// Pass the default HTTP client for inter-service communication
	return &GameServiceClient{
		apiClient: api.NewClient(baseURL, api.NewDefaultHTTPClient()),
	}
}

// OnlineStatusRequest represents the payload for online/offline updates.
type OnlineStatusRequest struct {
	UUID string `json:"uuid"`
}

// BanRequest is the structure for the request body for banning/unbanning.
type BanRequest struct {
	UUID        string `json:"uuid"`
	DurationSec int64  `json:"duration_seconds"` // Duration in seconds. 0 for permanent, -1 to unban.
	Reason      string `json:"reason,omitempty"`
}

// SendPlayerOnline sends a POST request to the /game/online endpoint.
func (c *GameServiceClient) SendPlayerOnline(ctx context.Context, playerUUID uuid.UUID) error {
	reqData := OnlineStatusRequest{
		UUID: playerUUID.String(),
	}
	// Use the apiClient's Post method. No response body is expected, so result is nil.
	return c.apiClient.Post(ctx, "/game/online", reqData, nil)
}

// SendPlayerOffline sends a POST request to the /game/offline endpoint.
func (c *GameServiceClient) SendPlayerOffline(ctx context.Context, playerUUID uuid.UUID) error {
	reqData := OnlineStatusRequest{
		UUID: playerUUID.String(),
	}
	// Use the apiClient's Post method. No response body is expected, so result is nil.
	return c.apiClient.Post(ctx, "/game/offline", reqData, nil)
}

// BanPlayer sends a POST request to the /game/ban endpoint to ban a player.
func (c *GameServiceClient) BanPlayer(ctx context.Context, playerUUID uuid.UUID, duration time.Duration, reason string) error {
	reqData := BanRequest{
		UUID:        playerUUID.String(),
		DurationSec: int64(duration.Seconds()),
		Reason:      reason,
	}
	// Use the apiClient's Post method. No response body is expected, so result is nil.
	return c.apiClient.Post(ctx, "/game/ban", reqData, nil)
}

// UnbanPlayer sends a POST request to the /game/unban endpoint to unban a player.
func (c *GameServiceClient) UnbanPlayer(ctx context.Context, playerUUID uuid.UUID) error {
	reqData := OnlineStatusRequest{ // Re-use OnlineStatusRequest as it only needs UUID
		UUID: playerUUID.String(),
	}
	// Use the apiClient's Post method. No response body is expected, so result is nil.
	return c.apiClient.Post(ctx, "/game/unban", reqData, nil)
}
