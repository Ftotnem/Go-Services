// game/api/handlers.go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Ftotnem/GO-SERVICES/game/service"
	"github.com/Ftotnem/GO-SERVICES/shared/api"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// GameAPIHandlers holds references to the services that handle business logic for the game service.
type GameAPIHandlers struct {
	GameService *service.GameService // Assuming you have a game service business logic layer
}

// NewGameAPIHandlers is the constructor for your Game API handlers.
// It takes the actual GameService (business logic) as a dependency.
func NewGameAPIHandlers(gs *service.GameService) *GameAPIHandlers {
	return &GameAPIHandlers{
		GameService: gs,
	}
}

// --- Request/Response DTOs (Data Transfer Objects) ---
// These are specific to the API and might differ slightly from your models if needed.

// PlayerUUIDRequest is a general structure for requests that only need a player UUID.
type PlayerUUIDRequest struct {
	UUID string `json:"uuid"`
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

// BanRequest is the structure for the request body for banning.
type BanRequest struct {
	UUID        string `json:"uuid"`
	DurationSec int64  `json:"duration_seconds"` // Duration in seconds. 0 for permanent.
	Reason      string `json:"reason,omitempty"`
}

// BanResponse is the structure for the JSON response after a ban operation.
type BanResponse struct {
	Message     string `json:"message"`
	UUID        string `json:"uuid"`
	ExpiresAt   int64  `json:"expires_at,omitempty"` // Unix timestamp, 0 for permanent
	IsPermanent bool   `json:"is_permanent"`
}

// --- Handler Methods ---

// HandlePlayerOnline handles requests to mark a player as online and load their data.
// POST /game/player/online
// Body: { "uuid": "<player_uuid>" }
func (gah *GameAPIHandlers) HandlePlayerOnline(w http.ResponseWriter, r *http.Request) {
	var req PlayerUUIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	playerUUID, err := uuid.Parse(req.UUID)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second) // Increased timeout for external service call
	defer cancel()

	err = gah.GameService.PlayerOnline(ctx, playerUUID.String())
	if err != nil {
		log.Printf("Error processing player %s online: %v", playerUUID.String(), err)
		// Specific error handling for banned players
		if err.Error() == fmt.Sprintf("player %s is currently banned and cannot go online", playerUUID.String()) {
			api.WriteError(w, http.StatusForbidden, err.Error())
		} else {
			api.WriteError(w, http.StatusInternalServerError, "Failed to set player online status")
		}
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": "Player set online and data loaded", "uuid": playerUUID.String()})
	log.Printf("Player %s is now online.", playerUUID.String())
}

// HandlePlayerOffline handles requests to mark a player as offline and persist playtime.
// POST /game/player/offline
// Body: { "uuid": "<player_uuid>" }
func (gah *GameAPIHandlers) HandlePlayerOffline(w http.ResponseWriter, r *http.Request) {
	var req PlayerUUIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	playerUUID, err := uuid.Parse(req.UUID)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second) // Increased timeout for external service call
	defer cancel()

	err = gah.GameService.PlayerOffline(ctx, playerUUID.String())
	if err != nil {
		log.Printf("Error processing player %s offline: %v", playerUUID.String(), err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to set player offline status or persist data")
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": "Player set offline, data persisted and Redis keys cleaned", "uuid": playerUUID.String()})
	log.Printf("Player %s is now offline. Data persisted and Redis session keys cleared.", playerUUID.String())
}

// HandleRefreshOnline handles requests to refresh a player's online status (heartbeat).
// POST /game/player/refresh-online
// Body: { "uuid": "<player_uuid>" }
func (gah *GameAPIHandlers) HandleRefreshOnline(w http.ResponseWriter, r *http.Request) {
	var req PlayerUUIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	playerUUID, err := uuid.Parse(req.UUID)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	err = gah.GameService.RefreshPlayerOnlineStatus(ctx, playerUUID.String())
	if err != nil {
		log.Printf("Error refreshing online status for player %s: %v", playerUUID.String(), err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to refresh player online status")
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": "Player online status refreshed", "uuid": playerUUID.String()})
	log.Printf("Player %s online status refreshed.", playerUUID.String())
}

// GetPlayerTotalPlaytime handles requests to retrieve a player's total playtime from Redis.
// GET /game/player/{uuid}/playtime
func (gah *GameAPIHandlers) GetPlayerTotalPlaytime(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playerUUIDStr := vars["uuid"]
	if playerUUIDStr == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	if _, err := uuid.Parse(playerUUIDStr); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	playtime, err := gah.GameService.GetPlayerTotalPlaytime(ctx, playerUUIDStr)
	if err != nil {
		log.Printf("Error getting total playtime for %s: %v", playerUUIDStr, err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to retrieve total playtime")
		return
	}

	api.WriteJSON(w, http.StatusOK, PlaytimeResponse{Playtime: playtime})
}

// GetPlayerDeltaPlaytime handles requests to retrieve a player's delta playtime from Redis.
// GET /game/player/{uuid}/deltatime
func (gah *GameAPIHandlers) GetPlayerDeltaPlaytime(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playerUUIDStr := vars["uuid"]
	if playerUUIDStr == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	if _, err := uuid.Parse(playerUUIDStr); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	deltaPlaytime, err := gah.GameService.GetPlayerDeltaPlaytime(ctx, playerUUIDStr)
	if err != nil {
		log.Printf("Error getting delta playtime for %s: %v. Returning default 1.0.", playerUUIDStr, err)
		api.WriteJSON(w, http.StatusOK, DeltaPlaytimeResponse{Deltatime: 1.0})
		return
	}

	api.WriteJSON(w, http.StatusOK, DeltaPlaytimeResponse{Deltatime: deltaPlaytime})
}

// GetTeamTotalPlaytime handles requests to retrieve the total playtime for a specific team.
// GET /game/team/{teamId}/playtime
func (gah *GameAPIHandlers) GetTeamTotalPlaytime(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	vars := mux.Vars(r)
	teamID := vars["teamId"] // Changed from "team" to "teamId" for clarity

	if teamID == "" {
		api.WriteError(w, http.StatusBadRequest, "Team ID is required in the path (e.g., /game/team/AQUA_CREEPERS/playtime)")
		return
	}

	totalPlaytime, err := gah.GameService.GetTeamTotalPlaytime(ctx, teamID)
	if err != nil {
		log.Printf("Error retrieving total playtime for team '%s': %v", teamID, err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to retrieve team total playtime")
		return
	}

	response := TeamTotalPlaytimeResponse{
		TeamID:        teamID,
		TotalPlaytime: totalPlaytime,
	}

	api.WriteJSON(w, http.StatusOK, response)
}

// GetPlayerOnlineStatus handles requests to check player online status.
// GET /game/player/{uuid}/is-online
func (gah *GameAPIHandlers) GetPlayerOnlineStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playerUUIDStr := vars["uuid"]
	if playerUUIDStr == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	if _, err := uuid.Parse(playerUUIDStr); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	isOnline, err := gah.GameService.IsPlayerOnline(ctx, playerUUIDStr)
	if err != nil {
		log.Printf("Error checking online status for %s: %v", playerUUIDStr, err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to check player online status")
		return
	}

	api.WriteJSON(w, http.StatusOK, PlayerOnlineStatusResponse{
		UUID:     playerUUIDStr,
		IsOnline: isOnline,
	})
}

// HandleBanPlayer handles requests to ban a player.
// POST /game/admin/ban
// Body: { "uuid": "<player_uuid>", "duration_seconds": <seconds>, "reason": "..." }
func (gah *GameAPIHandlers) HandleBanPlayer(w http.ResponseWriter, r *http.Request) {
	var req BanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	playerUUID, err := uuid.Parse(req.UUID)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
		// Handle unban via separate endpoint as per discussion
	} else if req.DurationSec == -1 {
		api.WriteError(w, http.StatusBadRequest, "Use /game/admin/unban to unban a player")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var banExpiresAt *time.Time
	isPermanent := false

	if req.DurationSec == 0 {
		isPermanent = true
		banExpiresAt = nil // Explicitly nil for permanent ban
	} else {
		expires := time.Now().Add(time.Duration(req.DurationSec) * time.Second)
		banExpiresAt = &expires
	}

	err = gah.GameService.BanPlayer(ctx, playerUUID.String(), banExpiresAt, req.Reason)
	if err != nil {
		log.Printf("Error banning player %s: %v", playerUUID.String(), err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to ban player")
		return
	}

	responseMsg := fmt.Sprintf("Player %s banned", playerUUID.String())
	var expiresAtUnix int64 = 0
	if !isPermanent {
		responseMsg = fmt.Sprintf("Player %s banned until %v", playerUUID.String(), banExpiresAt.Format(time.RFC3339))
		expiresAtUnix = banExpiresAt.Unix()
	}

	api.WriteJSON(w, http.StatusOK, BanResponse{
		Message:     responseMsg,
		UUID:        playerUUID.String(),
		ExpiresAt:   expiresAtUnix,
		IsPermanent: isPermanent,
	})
}

// HandleUnbanPlayer handles requests to unban a player.
// POST /game/admin/unban
// Body: { "uuid": "<player_uuid>" }
func (gah *GameAPIHandlers) HandleUnbanPlayer(w http.ResponseWriter, r *http.Request) {
	var req PlayerUUIDRequest // Re-use PlayerUUIDRequest as it only needs UUID
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	playerUUID, err := uuid.Parse(req.UUID)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	err = gah.GameService.UnbanPlayer(ctx, playerUUID.String())
	if err != nil {
		log.Printf("Error unbanning player %s: %v", playerUUID.String(), err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to unban player")
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": "Player unbanned", "uuid": playerUUID.String()})
}

// RegisterRoutes registers all API endpoints for the Game Service.
// This method is called from main.go to set up the HTTP routes.
func (gah *GameAPIHandlers) RegisterRoutes(router *mux.Router) {
	// Player status and playtime
	router.HandleFunc("/game/player/online", gah.HandlePlayerOnline).Methods("POST")
	router.HandleFunc("/game/player/offline", gah.HandlePlayerOffline).Methods("POST")
	router.HandleFunc("/game/player/refresh-online", gah.HandleRefreshOnline).Methods("POST") // New endpoint for heartbeat
	router.HandleFunc("/game/player/{uuid}/playtime", gah.GetPlayerTotalPlaytime).Methods("GET")
	router.HandleFunc("/game/player/{uuid}/deltatime", gah.GetPlayerDeltaPlaytime).Methods("GET")
	router.HandleFunc("/game/player/{uuid}/is-online", gah.GetPlayerOnlineStatus).Methods("GET")

	// Team playtime
	router.HandleFunc("/game/team/{teamId}/playtime", gah.GetTeamTotalPlaytime).Methods("GET") // Changed path variable name

	// Admin (ban/unban)
	router.HandleFunc("/game/admin/ban", gah.HandleBanPlayer).Methods("POST")
	router.HandleFunc("/game/admin/unban", gah.HandleUnbanPlayer).Methods("POST")
}
