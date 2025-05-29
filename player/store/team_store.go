// player/store/team_store.go
package store

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Ftotnem/GO-SERVICES/shared/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TeamStore represents the MongoDB data store for team profiles.
type TeamStore struct {
	collection *mongo.Collection
}

// NewTeamStore creates a new TeamStore instance.
func NewTeamStore(collection *mongo.Collection) *TeamStore {
	return &TeamStore{
		collection: collection,
	}
}

// EnsureTeamsExist initializes default team documents if they don't exist.
func (ts *TeamStore) EnsureTeamsExist(ctx context.Context, teams []string) error {
	for _, teamName := range teams {
		filter := bson.M{"_id": teamName}
		update := bson.M{
			"$setOnInsert": bson.M{
				"player_count":         0,
				"total_playtime_ticks": 0.0,
				"created_at":           time.Now(),
				"last_updated":         time.Now(),
			},
		}
		opts := options.Update().SetUpsert(true)

		result, err := ts.collection.UpdateOne(ctx, filter, update, opts)
		if err != nil {
			return fmt.Errorf("failed to upsert team %s: %w", teamName, err)
		}
		if result.UpsertedID != nil {
			log.Printf("INFO: Initialized team '%s' in database.", teamName)
		}
	}
	return nil
}

// GetTeamPlayerCount retrieves the current player count for a given team.
func (ts *TeamStore) GetTeamPlayerCount(ctx context.Context, teamName string) (int64, error) {
	var team models.Team
	filter := bson.M{"_id": teamName}

	err := ts.collection.FindOne(ctx, filter).Decode(&team)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil // Team not found (though EnsureTeamsExist should prevent this generally)
		}
		return 0, fmt.Errorf("failed to get player count for team %s: %w", teamName, err)
	}
	return team.PlayerCount, nil
}

// IncrementTeamPlayerCount atomically increments the player count for a team.
func (ts *TeamStore) IncrementTeamPlayerCount(ctx context.Context, teamName string) error {
	filter := bson.M{"_id": teamName}
	update := bson.M{
		"$inc": bson.M{"player_count": 1},
		"$set": bson.M{"last_updated": time.Now()},
	}
	res, err := ts.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to increment player count for team %s: %w", teamName, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("team %s not found for player count increment", teamName)
	}
	return nil
}

// DecrementTeamPlayerCount atomically decrements the player count for a team.
func (ts *TeamStore) DecrementTeamPlayerCount(ctx context.Context, teamName string) error {
	filter := bson.M{"_id": teamName}
	update := bson.M{
		"$inc": bson.M{"player_count": -1},
		"$set": bson.M{"last_updated": time.Now()},
	}
	res, err := ts.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to decrement player count for team %s: %w", teamName, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("team %s not found for player count decrement", teamName)
	}
	return nil
}

// UpdateTeamTotalPlaytime updates the total playtime for a team.
func (ts *TeamStore) UpdateTeamTotalPlaytime(ctx context.Context, teamName string, newTotalPlaytime float64) error {
	filter := bson.M{"_id": teamName}
	update := bson.M{
		"$set": bson.M{"total_playtime_ticks": newTotalPlaytime, "last_updated": time.Now()},
	}
	opts := options.Update().SetUpsert(true) // SetUpsert(true) here is crucial for aggregation updates
	res, err := ts.collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("failed to update total playtime for team %s: %w", teamName, err)
	}
	if res.MatchedCount == 0 && res.UpsertedID == nil { // If not matched and not upserted, something is wrong
		return fmt.Errorf("team %s not found or updated for total playtime", teamName)
	}
	return nil
}

// GetAllTeams retrieves all team documents.
func (ts *TeamStore) GetAllTeams(ctx context.Context) ([]models.Team, error) {
	var teams []models.Team
	cursor, err := ts.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to find all teams: %w", err)
	}
	defer cursor.Close(ctx)

	if err = cursor.All(ctx, &teams); err != nil {
		return nil, fmt.Errorf("failed to decode all teams: %w", err)
	}
	return teams, nil
}
