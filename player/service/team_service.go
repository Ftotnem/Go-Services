// player/service/team_service.go
package service

import (
	"context"
	"fmt"
	"log"

	"github.com/Ftotnem/GO-SERVICES/player/store"
)

// TeamService encapsulates the business logic for teams.
type TeamService struct {
	teamStore   *store.TeamStore
	playerStore *store.PlayerStore // Used for aggregation, still part of business logic
}

// NewTeamService creates a new TeamService instance.
func NewTeamService(ts *store.TeamStore, ps *store.PlayerStore) *TeamService {
	return &TeamService{
		teamStore:   ts,
		playerStore: ps,
	}
}

// SyncTeamTotals aggregates player playtimes and updates team totals in the database.
func (ts *TeamService) SyncTeamTotals(ctx context.Context) (map[string]float64, error) {
	log.Println("Starting team total playtime aggregation job (service layer)...")

	// Call the store to perform the aggregation
	teamTotalsMap, err := ts.playerStore.AggregateTeamPlaytimes(ctx)
	if err != nil {
		return nil, fmt.Errorf("service failed to aggregate team totals: %w", err)
	}

	// Iterate through aggregation results and update MongoDB Team collection via the store
	for teamName, calculatedTotal := range teamTotalsMap {
		if err := ts.teamStore.UpdateTeamTotalPlaytime(ctx, teamName, calculatedTotal); err != nil {
			log.Printf("ERROR: Failed to update total playtime for team %s in MongoDB: %v", teamName, err)
			// Decide if you want to stop or continue. For an aggregation job, often continue.
		} else {
			log.Printf("INFO: Successfully updated MongoDB total playtime for team '%s' to %.2f ticks.", teamName, calculatedTotal)
		}
	}

	log.Println("Team total playtime aggregation job finished (service layer).")
	return teamTotalsMap, nil
}
