package models

import (
	"time"
)

// Booster represents and active booster a player has
type Booster struct {
	ID        string
	Type      string
	Value     float64
	ExpiersAt time.Time
	Source    string
}

// Player repcrenset a player's profile data stored presistently in MongoDB

type Player struct {
	UUID            string     `bson:"_id" json:"uuid"`                    // Minecraft UUID (primary key)
	Username        string     `bson:"username" json:"username"`           // Real Minecraft username from Mojang
	TeamUsername    string     `bson:"team_username" json:"team_username"` // Renamed field: e.g., "AQUA_CREEPER1", "PURPLE_AXOLOTL69"
	Team            string     `bson:"team" json:"team"`                   // Assigned team (e.g., "AQUA_CREEPERS", "PURPLE_AXOLOTLS")
	CurrentPlaytime float64    `bson:"current_playtime" json:"current_playtime"`
	DeltaPlaytime   float64    `bson:"delta_playtime" json:"delta_playtime"`
	Banned          bool       `bson:"banned" json:"banned"`
	BanExpiresAt    *time.Time `bson:"ban_expires_at,omitempty" json:"ban_expires_at,omitempty"`
	CreatedAt       *time.Time `bson:"created_at" json:"created_at"`
	LastLoginAt     *time.Time `bson:"last_login_at" json:"last_login_at"`
}
