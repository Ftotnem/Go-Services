// player/api/handlers.go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Ftotnem/GO-SERVICES/player/service"
	"github.com/Ftotnem/GO-SERVICES/shared/api"
	"github.com/gorilla/mux"
)

// PlayerAPIHandlers holds references to the services that handle business logic.
type PlayerAPIHandlers struct {
	PlayerService *service.PlayerService
	TeamService   *service.TeamService
}

// NewPlayerAPIHandlers is the constructor for your API handlers.
func NewPlayerAPIHandlers(ps *service.PlayerService, ts *service.TeamService) *PlayerAPIHandlers {
	return &PlayerAPIHandlers{
		PlayerService: ps,
		TeamService:   ts,
	}
}

// --- Request/Response DTOs (Data Transfer Objects) ---
// These are specific to the API and might differ slightly from your models if needed.
type CreateProfileRequest struct {
	UUID string `json:"uuid"`
}

type UpdatePlaytimeRequest struct {
	TicksToSet float64 `json:"ticksToSet"`
}

type UpdateDeltaPlaytimeRequest struct {
	TicksToSet float64 `json:"ticksToSet"`
}

type UpdateBanStatusRequest struct {
	Banned       bool       `json:"banned"`
	BanExpiresAt *time.Time `json:"banExpiresAt"`
}

type SyncTeamTotalsResponse struct {
	TeamTotals map[string]float64 `json:"teamTotals"`
	Message    string             `json:"message"`
}

// --- Handler Methods ---

// CreateProfileHandler handles requests to create a new player profile.
// POST /profiles
func (pah *PlayerAPIHandlers) CreateProfileHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.UUID == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	createdProfile, err := pah.PlayerService.CreateProfile(ctx, req.UUID) // Call the service layer
	if err != nil {
		switch err { // Map service-layer errors to HTTP status codes
		case service.ErrProfileAlreadyExists:
			api.WriteError(w, http.StatusConflict, fmt.Sprintf("Profile with UUID %s already exists", req.UUID))
		default:
			log.Printf("Error creating player profile %s: %v", req.UUID, err)
			api.WriteError(w, http.StatusInternalServerError, "Failed to create player profile")
		}
		return
	}

	api.WriteJSON(w, http.StatusCreated, createdProfile) // 201 Created
	log.Printf("Player profile %s created successfully.", createdProfile.UUID)
}

// GetProfileHandler handles requests to retrieve a player profile by UUID.
// GET /profiles/{uuid}
func (pah *PlayerAPIHandlers) GetProfileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["uuid"]
	if uuid == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	profile, err := pah.PlayerService.GetProfile(ctx, uuid) // Call the service layer
	if err != nil {
		switch err {
		case service.ErrProfileNotFound:
			api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Player profile with UUID %s not found", uuid))
		default:
			log.Printf("Error getting player profile %s: %v", uuid, err)
			api.WriteError(w, http.StatusInternalServerError, "Failed to retrieve player profile")
		}
		return
	}

	api.WriteJSON(w, http.StatusOK, profile)
	log.Printf("Player profile %s retrieved successfully.", profile.UUID)
}

// UpdateProfilePlaytimeHandler handles requests to update a player's playtime.
// PUT /profiles/{uuid}/playtime
func (pah *PlayerAPIHandlers) UpdateProfilePlaytimeHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["uuid"]
	if uuid == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	var req UpdatePlaytimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	err := pah.PlayerService.UpdateProfilePlaytime(ctx, uuid, req.TicksToSet) // Call the service layer
	if err != nil {
		switch err {
		case service.ErrProfileNotFound:
			api.WriteError(w, http.StatusNotFound, "Player profile not found")
		default:
			log.Printf("Error updating playtime for player profile %s: %v", uuid, err)
			api.WriteError(w, http.StatusInternalServerError, "Failed to update playtime")
		}
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Playtime updated for player profile %s", uuid)})
}

// UpdateProfileDeltaPlaytimeHandler handles requests to update a player's delta playtime.
// PUT /profiles/{uuid}/deltaplaytime
func (pah *PlayerAPIHandlers) UpdateProfileDeltaPlaytimeHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["uuid"]
	if uuid == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	var req UpdateDeltaPlaytimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	err := pah.PlayerService.UpdateProfileDeltaPlaytime(ctx, uuid, req.TicksToSet)
	if err != nil {
		switch err {
		case service.ErrProfileNotFound:
			api.WriteError(w, http.StatusNotFound, "Player profile not found")
		default:
			log.Printf("Error updating delta playtime for player profile %s: %v", uuid, err)
			api.WriteError(w, http.StatusInternalServerError, "Failed to update delta playtime")
		}
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Delta playtime updated for player profile %s", uuid)})
}

// UpdateProfileBanStatusHandler handles requests to update a player's ban status.
// PUT /profiles/{uuid}/ban
func (pah *PlayerAPIHandlers) UpdateProfileBanStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["uuid"]
	if uuid == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	var req UpdateBanStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	err := pah.PlayerService.UpdateProfileBanStatus(ctx, uuid, req.Banned, req.BanExpiresAt)
	if err != nil {
		switch err {
		case service.ErrProfileNotFound:
			api.WriteError(w, http.StatusNotFound, "Player profile not found")
		default:
			log.Printf("Error updating ban status for player profile %s: %v", uuid, err)
			api.WriteError(w, http.StatusInternalServerError, "Failed to update ban status")
		}
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Ban status updated for player profile %s", uuid)})
}

// UpdateProfileLastLoginHandler handles requests to update only a player's last login timestamp.
// PUT /profiles/{uuid}/lastlogin
func (pah *PlayerAPIHandlers) UpdateProfileLastLoginHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["uuid"]
	if uuid == "" {
		api.WriteError(w, http.StatusBadRequest, "Player UUID is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	err := pah.PlayerService.UpdateProfileLastLogin(ctx, uuid)
	if err != nil {
		switch err {
		case service.ErrProfileNotFound:
			api.WriteError(w, http.StatusNotFound, "Player profile not found")
		default:
			log.Printf("Error updating last login for player profile %s: %v", uuid, err)
			api.WriteError(w, http.StatusInternalServerError, "Failed to update last login")
		}
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Last login updated for player profile %s", uuid)})
}

// SyncTeamTotalsHandler aggregates player playtimes from MongoDB and updates team totals.
// POST /teams/sync-totals
func (pah *PlayerAPIHandlers) SyncTeamTotalsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second) // Longer timeout for aggregation
	defer cancel()

	teamTotals, err := pah.TeamService.SyncTeamTotals(ctx) // Call the service layer
	if err != nil {
		log.Printf("Error in team total playtime aggregation: %v", err)
		api.WriteError(w, http.StatusInternalServerError, "Failed to aggregate team totals")
		return
	}

	api.WriteJSON(w, http.StatusOK, SyncTeamTotalsResponse{
		TeamTotals: teamTotals,
		Message:    "Team totals aggregated and updated in MongoDB successfully.",
	})
}

// RegisterRoutes registers all API endpoints for the Player Service.
// This method is called from main.go to set up the HTTP routes.
func (pah *PlayerAPIHandlers) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/profiles", pah.CreateProfileHandler).Methods("POST")
	router.HandleFunc("/profiles/{uuid}", pah.GetProfileHandler).Methods("GET")
	router.HandleFunc("/profiles/{uuid}/playtime", pah.UpdateProfilePlaytimeHandler).Methods("PUT")
	router.HandleFunc("/profiles/{uuid}/deltaplaytime", pah.UpdateProfileDeltaPlaytimeHandler).Methods("PUT")
	router.HandleFunc("/profiles/{uuid}/ban", pah.UpdateProfileBanStatusHandler).Methods("PUT")
	router.HandleFunc("/profiles/{uuid}/lastlogin", pah.UpdateProfileLastLoginHandler).Methods("PUT")

	router.HandleFunc("/teams/sync-totals", pah.SyncTeamTotalsHandler).Methods("POST")
}
