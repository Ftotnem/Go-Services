// player/store/player_store.go
package store

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Ftotnem/GO-SERVICES/shared/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// PlayerStore represents the MongoDB data store for player profiles.
type PlayerStore struct {
	collection *mongo.Collection
	// No direct MojangClient or TeamStore here! Stores should only do DB stuff.
}

// NewPlayerStore creates a new PlayerStore instance.
// The mongo.Client comes from your shared/mongodb package.
func NewPlayerStore(collection *mongo.Collection) *PlayerStore {
	return &PlayerStore{
		collection: collection,
	}
}

// CreatePlayer inserts a new player document (profile) into the collection.
func (ps *PlayerStore) CreatePlayer(ctx context.Context, player *models.Player) error {
	_, err := ps.collection.InsertOne(ctx, player)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("player profile %s already exists", player.UUID) // Return a simple error string for service to handle
		}
		return fmt.Errorf("failed to create player profile %s: %w", player.UUID, err)
	}
	return nil
}

// GetPlayerByUUID retrieves a player profile by their UUID.
func (ps *PlayerStore) GetPlayerByUUID(ctx context.Context, uuid string) (*models.Player, error) {
	var profile models.Player
	filter := bson.M{"_id": uuid}
	err := ps.collection.FindOne(ctx, filter).Decode(&profile)
	if err != nil {
		return nil, err // Return mongo.ErrNoDocuments if not found
	}
	return &profile, nil
}

// UpdatePlayerUsername updates only the Username field for a player profile.
func (ps *PlayerStore) UpdatePlayerUsername(ctx context.Context, uuid, username string) error {
	filter := bson.M{"_id": uuid}
	update := bson.M{"$set": bson.M{"username": username}}
	res, err := ps.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update username for player %s: %w", uuid, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("player %s not found for username update", uuid)
	}
	return nil
}

// UpdatePlayerPlaytime updates a player profile's total playtime.
func (ps *PlayerStore) UpdatePlayerPlaytime(ctx context.Context, uuid string, newCurrentPlaytime float64) error {
	filter := bson.M{"_id": uuid}
	update := bson.M{"$set": bson.M{"current_playtime": newCurrentPlaytime}}
	res, err := ps.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to set playtime for player %s: %w", uuid, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("player %s not found for playtime update", uuid)
	}
	return nil
}

// UpdatePlayerDeltaPlaytime updates a player profile's delta playtime.
func (ps *PlayerStore) UpdatePlayerDeltaPlaytime(ctx context.Context, uuid string, newDeltaPlaytime float64) error {
	filter := bson.M{"_id": uuid}
	update := bson.M{"$set": bson.M{"delta_playtime_ticks": newDeltaPlaytime}}
	res, err := ps.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to set delta playtime for player %s: %w", uuid, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("player %s not found for delta playtime update", uuid)
	}
	return nil
}

// UpdatePlayerBanStatus updates a player profile's ban status.
func (ps *PlayerStore) UpdatePlayerBanStatus(ctx context.Context, uuid string, banned bool, expiresAt *time.Time) error {
	filter := bson.M{"_id": uuid}
	update := bson.M{"$set": bson.M{"banned": banned, "ban_expires_at": expiresAt}}
	res, err := ps.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update ban status for player %s: %w", uuid, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("player %s not found for ban status update", uuid)
	}
	return nil
}

// UpdatePlayerLastLogin updates only the LastLoginAt timestamp for a player profile.
func (ps *PlayerStore) UpdatePlayerLastLogin(ctx context.Context, uuid string) error {
	filter := bson.M{"_id": uuid}
	now := time.Now()
	update := bson.M{"$set": bson.M{"last_login_at": &now}}
	res, err := ps.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update last login for player %s: %w", uuid, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("player %s not found for last login update", uuid)
	}
	return nil
}

// AggregateTeamPlaytimes performs a MongoDB aggregation to calculate total playtime per team.
func (ps *PlayerStore) AggregateTeamPlaytimes(ctx context.Context) (map[string]float64, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$team"},
			{Key: "calculatedTotal", Value: bson.D{{Key: "$sum", Value: "$current_playtime"}}},
		}}},
	}

	cursor, err := ps.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("error running aggregation for team totals: %w", err)
	}
	defer cursor.Close(ctx)

	teamTotalsMap := make(map[string]float64)
	for cursor.Next(ctx) {
		var result struct {
			TeamID          string  `bson:"_id"`
			CalculatedTotal float64 `bson:"calculatedTotal"`
		}
		if err := cursor.Decode(&result); err != nil {
			log.Printf("WARN: Error decoding aggregation result: %v", err) // Log and continue
			continue
		}
		teamTotalsMap[result.TeamID] = result.CalculatedTotal
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("error during aggregation cursor iteration: %w", err)
	}
	return teamTotalsMap, nil
}
