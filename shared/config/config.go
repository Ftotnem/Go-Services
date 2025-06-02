// shared/config/config.go
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// CommonConfig holds configuration fields that are shared across multiple services.
type CommonConfig struct {
	RedisAddrs              []string      // Redis server addresses (e.g., "redis-cluster:6379")
	RedisPassword           string        // NEW: Redis password for authentication
	HeartbeatInterval       time.Duration // How often to send a heartbeat to registry (e.g., 5s)
	HeartbeatTTL            time.Duration // How long an instance is considered alive without a heartbeat (e.g., 15s)
	RegistryCleanupInterval time.Duration // How often the registry actively cleans stale entries (e.g., 30s)
	ServiceIP               string        // The IP address this service advertises for registration (Kubernetes Pod IP)
	ServicePort             int           // The port this service listens on, used for registration
}

// GameServiceConfig holds configuration specific to the game-service.
type GameServiceConfig struct {
	CommonConfig                            // Embed CommonConfig
	ListenAddr                string        // Address for the HTTP server (e.g., ":8082")
	RedisOnlineTTL            time.Duration // TTL for 'online:<uuid>' keys in Redis (e.g., 15s)
	TickInterval              time.Duration // Duration for the game tick (e.g., 50ms)
	PersistenceInterval       time.Duration // Duration for periodic persistence (e.g., 1m)
	PlayerServiceURL          string        // The URL to the used player-service (e.g., "http://player-service:8081")
	GameServiceInstanceID     int           // Unique identifier for this game service instance (e.g., 0, 1, 2 for sharding)
	TotalGameServiceInstances int           // Total number of active game service instances (e.g., 1, 3 for sharding)
	BackupTimeout             time.Duration // NEW: Timeout for the full player playtime backup operation (e.g., 60 seconds)
	SyncTimeout               time.Duration // NEW: Timeout for the team total sync operation (e.g., 30 seconds)
}

// PlayerServiceConfig holds configuration specific to the player-service.
type PlayerServiceConfig struct {
	CommonConfig                           // Embed CommonConfig
	ListenAddr               string        // Address for the HTTP server to listen on (e.g., ":8081")
	MongoDBConnStr           string        // MongoDB connection string
	MongoDBDatabase          string        // MongoDB database name (e.g., "minecraft_players")
	MongoDBPlayersCollection string        // MongoDB collection for players (e.g., "players")
	MongoDBTeamCollection    string        // MongoDB collection for team related info
	UsernameFillerInterval   time.Duration // An interval for where to perform Background tasks (e.g., Username Filler Jobs)
	DefaultTeams             []string
}

// LoadCommonConfig loads common configuration from environment variables.
func LoadCommonConfig() (CommonConfig, error) {
	cfg := CommonConfig{}
	var err error

	// Redis Addresses
	redisAddrsStr := os.Getenv("REDIS_ADDRS")
	if redisAddrsStr == "" {
		cfg.RedisAddrs = []string{"redis-cluster-headless.minecraft-cluster.svc.cluster.local:6379"} // Default for K8s Service
	} else {
		for _, addr := range strings.Split(redisAddrsStr, ",") {
			cfg.RedisAddrs = append(cfg.RedisAddrs, strings.TrimSpace(addr))
		}
	}

	// NEW: Redis Password
	cfg.RedisPassword = os.Getenv("REDIS_PASSWORD")
	fmt.Println(cfg.RedisPassword)
	cfg.HeartbeatInterval, err = getDuration("SERVICE_HEARTBEAT_INTERVAL", 5*time.Second)
	if err != nil {
		return cfg, err
	}
	cfg.HeartbeatTTL, err = getDuration("SERVICE_HEARTBEAT_TTL", 15*time.Second)
	if err != nil {
		return cfg, err
	}
	cfg.RegistryCleanupInterval, err = getDuration("SERVICE_REGISTRY_CLEANUP_INTERVAL", 30*time.Second)
	if err != nil {
		return cfg, err
	}

	// Service IP (for registration, from Kubernetes Pod IP)
	cfg.ServiceIP = os.Getenv("POD_IP") // Injected by Kubernetes
	if cfg.ServiceIP == "" {
		// Fallback for local development outside K8s or if not injected
		cfg.ServiceIP = "0.0.0.0"
		fmt.Printf("WARNING: POD_IP not set, defaulting ServiceIP to %s\n", cfg.ServiceIP)
	}

	return cfg, nil
}

// Helper function to parse duration from environment variable
func getDuration(envKey string, defaultVal time.Duration) (time.Duration, error) {
	valStr := os.Getenv(envKey)
	if valStr == "" {
		return defaultVal, nil
	}
	d, err := time.ParseDuration(valStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration format for %s: %w", envKey, err)
	}
	return d, nil
}

// Helper function to parse int from environment variable
func getInt(envKey string, defaultVal int) (int, error) {
	valStr := os.Getenv(envKey)
	if valStr == "" {
		return defaultVal, nil
	}
	i, err := strconv.Atoi(valStr)
	if err != nil {
		return 0, fmt.Errorf("invalid integer format for %s: %w", envKey, err)
	}
	return i, nil
}

// extractPort extracts the numeric port from a listen address (e.g., ":8082" -> 8082, "0.0.0.0:8082" -> 8082)
func extractPort(listenAddr string) (int, error) {
	_, portStr, err := net.SplitHostPort(listenAddr)
	if err != nil {
		// If SplitHostPort fails, check if ListenAddr is just a port (e.g., ":8082")
		if strings.HasPrefix(listenAddr, ":") {
			portStr = strings.TrimPrefix(listenAddr, ":")
		} else {
			return 0, fmt.Errorf("invalid ListenAddr format for port extraction: %w", err)
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid port number '%s': %w", portStr, err)
	}
	return port, nil
}

// LoadGameServiceConfig loads configuration for the game-service.
func LoadGameServiceConfig() (*GameServiceConfig, error) {
	common, err := LoadCommonConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load common config for game-service: %w", err)
	}

	cfg := &GameServiceConfig{
		CommonConfig:     common,
		ListenAddr:       os.Getenv("GAME_SERVICE_LISTEN_ADDR"),
		PlayerServiceURL: os.Getenv("PLAYERS_SERVICE_URL"),
	}

	// Apply defaults for specific fields if not set
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8082"
	}
	if cfg.PlayerServiceURL == "" {
		cfg.PlayerServiceURL = "http://player-service:8081" // Default for K8s internal DNS
	}

	// Extract ServicePort from ListenAddr
	cfg.ServicePort, err = extractPort(cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to extract port from GAME_SERVICE_LISTEN_ADDR '%s': %w", cfg.ListenAddr, err)
	}
	// Durations
	cfg.RedisOnlineTTL, err = getDuration("REDIS_ONLINE_TTL", 15*time.Second)
	if err != nil {
		return cfg, err
	}
	cfg.TickInterval, err = getDuration("GAME_SERVICE_TICK_INTERVAL", 50*time.Millisecond)
	if err != nil {
		return nil, err
	}
	cfg.PersistenceInterval, err = getDuration("GAME_SERVICE_PERSISTENCE_INTERVAL", 30*time.Second)
	if err != nil {
		return nil, err
	}

	cfg.GameServiceInstanceID, err = getInt("GAME_SERVICE_INSTANCE_ID", 0)
	if err != nil {
		return nil, err
	}
	cfg.TotalGameServiceInstances, err = getInt("TOTAL_GAME_SERVICE_INSTANCES", 1)
	if err != nil {
		return nil, err
	}

	// Final validation for instance IDs
	if cfg.TotalGameServiceInstances <= 0 {
		return nil, fmt.Errorf("TOTAL_GAME_SERVICE_INSTANCES must be a positive integer (got %d)", cfg.TotalGameServiceInstances)
	}
	if cfg.GameServiceInstanceID < 0 || cfg.GameServiceInstanceID >= cfg.TotalGameServiceInstances {
		return nil, fmt.Errorf("GAME_SERVICE_INSTANCE_ID (%d) must be non-negative and less than TOTAL_GAME_SERVICE_INSTANCES (%d)", cfg.GameServiceInstanceID, cfg.TotalGameServiceInstances)
	}

	backupTimeoutStr := os.Getenv("GAME_BACKUP_TIMEOUT")
	cfg.BackupTimeout, err = time.ParseDuration(backupTimeoutStr)
	if err != nil {
		cfg.BackupTimeout = 60 * time.Second // Default timeout for the full player playtime backup operation
	}

	syncTimeoutStr := os.Getenv("GAME_SYNC_TIMEOUT")
	cfg.SyncTimeout, err = time.ParseDuration(syncTimeoutStr)
	if err != nil {
		cfg.SyncTimeout = 30 * time.Second // Default timeout for the team total sync operation
	}

	return cfg, nil
}

// LoadPlayerServiceConfig loads configuration for the player-service.
func LoadPlayerServiceConfig() (*PlayerServiceConfig, error) {
	common, err := LoadCommonConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load common config for player-service: %w", err)
	}

	cfg := &PlayerServiceConfig{
		CommonConfig:             common,
		ListenAddr:               os.Getenv("PLAYER_SERVICE_LISTEN_ADDR"),
		MongoDBConnStr:           os.Getenv("MONGODB_CONN_STR"),
		MongoDBDatabase:          os.Getenv("MONGODB_DATABASE"),
		MongoDBPlayersCollection: os.Getenv("MONGODB_PLAYERS_COLLECTION"),
		MongoDBTeamCollection:    os.Getenv("MONGODB_TEAM_COLLECTION"),
		DefaultTeams:             []string{"AQUA_CREEPERS", "PURPLE_SWORDERS"},
	}

	// Apply defaults
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8081"
	}
	if cfg.MongoDBConnStr == "" {
		cfg.MongoDBConnStr = "mongodb://mongodb-service:27017" // Default for K8s Service mongodb://mongodb-service:27017
	}
	if cfg.MongoDBDatabase == "" {
		cfg.MongoDBDatabase = "minestom"
	}
	if cfg.MongoDBPlayersCollection == "" {
		cfg.MongoDBPlayersCollection = "players"
	}
	if cfg.MongoDBTeamCollection == "" {
		cfg.MongoDBTeamCollection = "teams"
	}

	cfg.UsernameFillerInterval = 30 * time.Second

	// Extract ServicePort from ListenAddr
	cfg.ServicePort, err = extractPort(cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to extract port from PLAYER_SERVICE_LISTEN_ADDR '%s': %w", cfg.ListenAddr, err)
	}

	return cfg, nil
}
