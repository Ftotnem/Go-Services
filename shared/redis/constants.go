// player/redis/constants.go
package redis // This is the package name for the player's redis client

import "fmt" // Needed for ErrRedisKeyNotFound

const (
	// Key constants for Redis player data
	OnlineKeyPrefix         = "online:{%s}:"              // Key for player online status: online:{uuid}
	PlaytimeKeyPrefix       = "playtime:{%s}:"            // Key for total playtime: playtime:{uuid}
	DeltaPlaytimeKeyPrefix  = "deltatime:{%s}:"           // Key for delta playtime since last persist: deltatime:{uuid}
	BannedKeyPrefix         = "banned:{%s}:"              // Key for player ban status: banned:{uuid}
	PlayerTeamKeyPrefix     = "team:{%s}:"                // Key for player's assigned team: team:{uuid}
	TeamTotalPlaytimePrefix = "team_total_playtime:{%s}:" // Key for total playtime of a team: team_total_playtime:{teamID}
)

// Define a custom error for when a Redis key is not found (can also be a constant)
var ErrRedisKeyNotFound = fmt.Errorf("redis key not found")
