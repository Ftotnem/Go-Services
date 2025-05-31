package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	gameapi "github.com/Ftotnem/GO-SERVICES/game/api" // Assuming you have a game API package
	"github.com/Ftotnem/GO-SERVICES/game/service"     // The game service business logic
	"github.com/Ftotnem/GO-SERVICES/game/store"       // The Redis-only stores
	"github.com/Ftotnem/GO-SERVICES/game/syncer"
	"github.com/Ftotnem/GO-SERVICES/game/updater"
	"github.com/Ftotnem/GO-SERVICES/shared/api"
	"github.com/Ftotnem/GO-SERVICES/shared/config"
	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis" // For Redis client utility
	"github.com/Ftotnem/GO-SERVICES/shared/registry"     // For service registration
	playerserviceclient "github.com/Ftotnem/GO-SERVICES/shared/service"
)

func main() {
	// --- 1. Load Configuration ---
	cfg, err := config.LoadGameServiceConfig() // Assuming this loads game-specific config
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	log.Printf("Configuration loaded for Game Service. Listening on: %s", cfg.ListenAddr)

	// --- 2. Connect to Redis Cluster ---
	redisClient, err := redisu.NewRedisClusterClient(cfg.RedisAddrs)
	if err != nil {
		log.Fatalf("Failed to connect to Redis Cluster: %v", err)
	}
	defer func() {
		if err = redisClient.Close(); err != nil {
			log.Fatalf("Error closing Redis client: %v", err)
		}
		log.Println("Redis Client closed.")
	}()
	log.Println("Connected to Redis Cluster.")

	// --- 3. Initialize Data Stores (Redis-only) ---
	// These are the stores that interact directly with Redis
	playerPlaytimeStore := store.NewPlayerPlaytimeStore(redisClient)
	onlinePlayersStore := store.NewOnlinePlayersStore(redisClient, cfg.RedisOnlineTTL) // Assuming this store exists and is Redis-only
	teamPlaytimeStore := store.NewTeamPlaytimeStore(redisClient)
	banStore := store.NewBanStore(redisClient) // Assuming this store exists and is Redis-only

	playerserviceclient := playerserviceclient.NewPlayerClient(cfg.PlayerServiceURL)

	// --- 4. Initialize Business Logic Service (passing stores) ---
	// The GameService handles all real-time game logic using Redis-backed data.
	gameService := service.NewGameService(
		playerPlaytimeStore,
		onlinePlayersStore,
		teamPlaytimeStore,
		banStore,
		redisClient, // Pass the main Redis client for direct lookups (e.g., player team)
		playerserviceclient,
	)
	log.Println("Game Service business logic initialized.")

	// --- 5. Initialize API Handlers (passing business logic services) ---
	// Assuming gameapi.NewGameAPIHandlers and its RegisterRoutes method exist.
	gameAPIHandlers := gameapi.NewGameAPIHandlers(gameService)

	// --- 6. Initialize and Start Service Registrar ---
	// The Game Service registers itself with the service discovery system.
	registrar := registry.NewServiceRegistrar(redisClient, "game-service", &cfg.CommonConfig)
	go registrar.Start()   // Start the heartbeating goroutine
	defer registrar.Stop() // Ensure registrar stops on shutdown
	log.Printf("Service registrar started for 'game-service' with Address: %s", cfg.ListenAddr)

	// The serviceTimeout for RegistryClient should be related to HeartbeatTTL from CommonConfig
	registryClient := registry.NewRegistryClient(redisClient, cfg.HeartbeatTTL)

	updater := updater.NewGameUpdater(cfg, registryClient, onlinePlayersStore, playerPlaytimeStore, registrar)
	go updater.Start()
	defer updater.Stop()

	syncer := syncer.NewPlaytimeSyncer(cfg, playerPlaytimeStore, teamPlaytimeStore, *playerserviceclient, registryClient, registrar)
	go syncer.Start()
	defer syncer.Stop()

	// --- 7. Setup HTTP Server and Register Routes ---
	baseServer := api.NewBaseServer(cfg.ListenAddr, log.Default()) // Assumes NewBaseServer takes address and sets up mux.Router
	gameAPIHandlers.RegisterRoutes(baseServer.Router)
	log.Println("HTTP routes registered.")

	// --- 8. Start HTTP Server ---
	go func() {
		log.Printf("HTTP server starting on %s...", cfg.ListenAddr)
		if err := baseServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed to start: %v", err)
		}
	}()

	// --- 9. Graceful Shutdown ---
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop // Wait for interrupt signal

	log.Println("Shutting down Game Service...")

	// Create a context with a timeout for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Perform graceful shutdown of the HTTP server
	if err := baseServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("HTTP server graceful shutdown failed: %v", err)
	}
	log.Println("Game Service HTTP server gracefully stopped.")
	// defer registrar.Stop() // This will be called via defer
	// defer redisClient.Close() // This will be called via defer
	log.Println("Game Service gracefully shut down.")
}
