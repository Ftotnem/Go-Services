// shared/models/player.go
package models

import "time"

type Team struct {
	Name               string     `bson:"_id"` // Team name as _id (e.g., "AQUA_CREEPERS")
	PlayerCount        int64      `bson:"player_count"`
	TotalPlaytimeTicks float64    `bson:"total_playtime"` // Aggregate playtime for the team
	CreatedAt          *time.Time `bson:"created_at"`
	LastUpdated        *time.Time `bson:"last_updated"`
}
