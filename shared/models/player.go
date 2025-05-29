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
	UUID               string     `bson:"_id" json:"UUID"`
	Username           string     `bson:"username" json:"Username"`
	Team               string     `bson:"team" json:"Team"`
	TotalPlaytimeTicks float64    `bson:"total_playtime_ticks" json:"TotalPlaytimeTicks"`
	DeltaPlaytimeTicks float64    `bson:"delta_playtime_ticks" json:"DeltaPlaytimeTicks"`
	Banned             bool       `bson:"banned" json:"Banned"`
	BanExpiresAt       *time.Time `bson:"ban_expires_at,omitempty" json:"BanExpiresAt"`
	LastLoginAt        *time.Time `bson:"last_login_at,omitempty" json:"LastLoginAt"`
	CreatedAt          *time.Time `bson:"created_at,omitempty" json:"CreatedAt"`
}
