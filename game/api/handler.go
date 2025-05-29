// game/api/handlers.go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Ftotnem/GO-SERVICES/game/service"
	"github.com/Ftotnem/GO-SERVICES/shared/api"
	"github.com/google/uuid"
	"github.com/gorilla/mux" // Still needed for mux.Vars
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

// OnlineStatusRequest is the structure for the request body of /game/online and /game/offline.
type OnlineStatusRequest struct {
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

// TeamTotalResponse defines the structure for the JSON response for a single team's total.
type TeamTotalResponse struct {
	TotalPlaytime float64 `json:"totalPlaytime"`
}

// BanRequest is the structure for the request body for banning/unbanning.
type BanRequest struct {
	UUID        string `json:"uuid"`
	DurationSec int64  `json:"duration_seconds"` // Duration in seconds. 0 for permanent, -1 to unban.
	Reason      string `json:"reason,omitempty"`
}

// --- Handler Methods (moved from main.go/gameservice.go) ---

// HandleOnline handles requests to mark a player as online.
// POST /game/online
// Body: { "uuid": "<player_uuid>" }
func (gah *GameAPIHandlers) HandleOnline(w http.ResponseWriter, r *http.Request) {
	var req OnlineStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	playerUUID, err := uuid.Parse(req.UUID)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Call the GameService business logic
	err = gah.GameService.HandlePlayerOnline(ctx, playerUUID.String())
	if err != nil {
		log.Printf("Error handling player %s online: %v", playerUUID.String(), err)
		// Map service-layer errors to HTTP status codes as needed, similar to player-api
		api.WriteError(w, http.StatusInternalServerError, "Failed to set player online status")
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": "Player set online", "uuid": playerUUID.String()})
	log.Printf("Player %s is now online.", playerUUID.String())
}

// HandleOffline handles requests to mark a player as offline and persist playtime.
// POST /game/offline
// Body: { "uuid": "<player_uuid>" }
func (gah *GameAPIHandlers) HandleOffline(w http.ResponseWriter, r *http.Request) {
	var req OnlineStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	playerUUID, err := uuid.Parse(req.UUID)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Call the GameService business logic
	err = gah.GameService.HandlePlayerOffline(ctx, playerUUID.String())
	if err != nil {
		log.Printf("Error handling player %s offline: %v", playerUUID.String(), err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to set player offline status")
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": "Player set offline", "uuid": playerUUID.String()})
	log.Printf("Player %s is now offline. Data persisted and Redis session keys cleared.", playerUUID.String())
}

// GetTeamTotal handles requests to retrieve the total playtime for a specific team.
// GET /game/total/{team}
func (gah *GameAPIHandlers) GetTeamTotal(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	vars := mux.Vars(r)
	teamID := vars["team"]

	if teamID == "" {
		api.WriteError(w, http.StatusBadRequest, "Team ID is required in the path (e.g., /game/total/AQUA_CREEPERS)")
		return
	}

	totalPlaytime, err := gah.GameService.GetTeamTotalPlaytime(ctx, teamID)
	if err != nil {
		log.Printf("Error retrieving total playtime for team '%s': %v", teamID, err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to retrieve team total playtime")
		return
	}

	response := TeamTotalResponse{
		TotalPlaytime: totalPlaytime,
	}

	api.WriteJSON(w, http.StatusOK, response)
}

// GetPlayerOnlineStatus handles requests to check player online status.
// GET /game/player/{uuid}/online
func (gah *GameAPIHandlers) GetPlayerOnlineStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playerUUIDStr := vars["uuid"]
	if playerUUIDStr == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
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

	api.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"uuid":     playerUUIDStr,
		"isOnline": isOnline,
	})
}

// HandleBanPlayer handles requests to ban a player.
// POST /game/ban
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
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var banExpiresAt *time.Time
	isPermanent := false

	if req.DurationSec == -1 {
		api.WriteError(w, http.StatusBadRequest, "Use /game/unban to unban a player")
		return
	} else if req.DurationSec == 0 {
		isPermanent = true
		// banExpiresAt remains nil for permanent ban
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
	if !isPermanent {
		responseMsg = fmt.Sprintf("Player %s banned until %v", playerUUID.String(), banExpiresAt.Format(time.RFC3339))
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{
		"message":      responseMsg,
		"uuid":         playerUUID.String(),
		"expires_at":   strconv.FormatInt(banExpiresAt.Unix(), 10), // Will be 0 for permanent, handle on client side
		"is_permanent": strconv.FormatBool(isPermanent),
	})
}

// HandleUnbanPlayer handles requests to unban a player.
// POST /game/unban
// Body: { "uuid": "<player_uuid>" }
func (gah *GameAPIHandlers) HandleUnbanPlayer(w http.ResponseWriter, r *http.Request) {
	var req OnlineStatusRequest // Re-use OnlineStatusRequest as it only needs UUID
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

// handleGetPlaytime handles requests to retrieve a player's total playtime from Redis.
// GET /playtime/{uuid}
func (gah *GameAPIHandlers) handleGetPlaytime(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playerUUIDStr := vars["uuid"]
	if playerUUIDStr == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	playerUUID, err := uuid.Parse(playerUUIDStr)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	playtime, err := gah.GameService.GetPlayerTotalPlaytime(ctx, playerUUID.String())
	if err != nil {
		// You'd ideally have a custom error for "not found" from your service layer
		// For now, mirroring previous logic
		log.Printf("Error getting total playtime for %s: %v", playerUUID.String(), err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to retrieve total playtime")
		return
	}

	api.WriteJSON(w, http.StatusOK, PlaytimeResponse{Playtime: playtime})
}

// handleGetDeltaPlaytime handles requests to retrieve a player's delta playtime from Redis.
// GET /deltatime/{uuid}
func (gah *GameAPIHandlers) handleGetDeltaPlaytime(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	playerUUIDStr := vars["uuid"]
	if playerUUIDStr == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	playerUUID, err := uuid.Parse(playerUUIDStr)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid UUID format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	deltaPlaytime, err := gah.GameService.GetPlayerDeltaPlaytime(ctx, playerUUID.String())
	if err != nil {
		// Return 0.0 with 200 OK if delta playtime is not found, as per Java client expectation
		log.Printf("Error getting delta playtime for %s: %v", playerUUID.String(), err)
		api.WriteJSON(w, http.StatusOK, DeltaPlaytimeResponse{Deltatime: 1.0})
		return
	}

	api.WriteJSON(w, http.StatusOK, DeltaPlaytimeResponse{Deltatime: deltaPlaytime})
}

// RegisterRoutes registers all API endpoints for the Game Service.
// This method is called from main.go to set up the HTTP routes.
func (gah *GameAPIHandlers) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/game/online", gah.HandleOnline).Methods("POST")
	router.HandleFunc("/game/offline", gah.HandleOffline).Methods("POST")
	router.HandleFunc("/game/total/{team}", gah.GetTeamTotal).Methods("GET")
	router.HandleFunc("/game/player/{uuid}/online", gah.GetPlayerOnlineStatus).Methods("GET")
	router.HandleFunc("/game/ban", gah.HandleBanPlayer).Methods("POST")
	router.HandleFunc("/game/unban", gah.HandleUnbanPlayer).Methods("POST")

	router.HandleFunc("/playtime/{uuid}", gah.handleGetPlaytime).Methods("GET")
	router.HandleFunc("/deltatime/{uuid}", gah.handleGetDeltaPlaytime).Methods("GET")
}
