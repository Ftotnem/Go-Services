// main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	playerapi "github.com/Ftotnem/GO-SERVICES/player/api"
	"github.com/Ftotnem/GO-SERVICES/player/mojang"
	"github.com/Ftotnem/GO-SERVICES/player/service"
	"github.com/Ftotnem/GO-SERVICES/player/store"
	"github.com/Ftotnem/GO-SERVICES/shared/api"
	"github.com/Ftotnem/GO-SERVICES/shared/config"
	mongodbu "github.com/Ftotnem/GO-SERVICES/shared/mongodb"
	redisu "github.com/Ftotnem/GO-SERVICES/shared/redis"
	"github.com/Ftotnem/GO-SERVICES/shared/registry"
)

func main() {
	// --- 1. Load Configuration ---
	cfg, err := config.LoadPlayerServiceConfig() // Assuming you have a config loader
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// --- 2. Connect to MongoDB ---
	mongoClient, err := mongodbu.NewClient(cfg.MongoDBConnStr, cfg.MongoDBDatabase) // Assuming NewClient is in shared/mongodb
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		if err = mongoClient.Disconnect(context.Background()); err != nil {
			log.Fatalf("Failed to disconnect from MongoDB: %v", err)
		}
		log.Println("Disconnected from MongoDB.")
	}()
	// --- 3. Connect to Redis ---
	redisClient, err := redisu.NewRedisClusterClient(cfg.RedisAddrs, cfg.RedisPassword)
	if err != nil {
		log.Fatalf("Failed to connect to Redis Cluster: %v", err)
	}
	defer func() {
		if err = redisClient.Close(); err != nil {
			log.Fatalf("Error closing Redis client: %v", err)
		}
		log.Println("Redis Client closed..")
	}()

	// --- 4. Initialize Data Stores (passing MongoDB collections) ---
	playersCollection := mongoClient.Collection(cfg.MongoDBPlayersCollection) // Use your actual collection name from config
	teamsCollection := mongoClient.Collection(cfg.MongoDBTeamCollection)      // Use your actual collection name from config

	playerStore := store.NewPlayerStore(playersCollection)
	teamStore := store.NewTeamStore(teamsCollection)

	// --- 5. Initialize External Services ---
	mojangService := mojang.NewMojangService(mongoClient, cfg.MongoDBPlayersCollection, cfg.UsernameFillerInterval) // Adjusted constructor
	go mojangService.StartFillerJob()                                                                               // Start background job
	defer mojangService.StopFillerJob()

	// --- 6. Ensure Initial Data Exists (e.g., default teams) ---
	if err := teamStore.EnsureTeamsExist(context.Background(), cfg.DefaultTeams); err != nil { // Assuming DefaultTeams is []string in config
		log.Fatalf("Failed to ensure default teams exist: %v", err)
	}

	// --- 7. Initialize Business Logic Services (passing stores and external services) ---
	playerService := service.NewPlayerService(playerStore, teamStore, mojangService)
	teamService := service.NewTeamService(teamStore, playerStore) // TeamService needs both stores for aggregation

	// --- 8. Initialize API Handlers (passing business logic services) ---
	playerAPIHandlers := playerapi.NewPlayerAPIHandlers(playerService, teamService)

	// --- 9. Initialize and Start Service Registrar ---
	// No need for a separate 'serviceConfig' struct now, use common config directly
	registrar := registry.NewServiceRegistrar(redisClient, "player-service", &cfg.CommonConfig) // <--- Pass common config directly
	go registrar.Start()                                                                        // Start the heartbeating goroutine
	defer registrar.Stop()                                                                      // Ensure registrar stops on shutdown

	// --- 10. Setup HTTP Server and Register Routes ---
	baseServer := api.NewBaseServer(cfg.ListenAddr, log.Default()) // Assuming NewBaseServer takes address and sets up mux.Router
	playerAPIHandlers.RegisterRoutes(baseServer.Router)

	// --- 11. Start HTTP Server ---
	go func() {
		log.Printf("HTTP server starting on %s...", cfg.ListenAddr)
		if err := baseServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed to start: %v", err)
		}
	}()

	// --- 12. Graceful Shutdown ---
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop // Wait for interrupt signal

	log.Println("Shutting down server...")

	// Create a context with a timeout for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := baseServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("HTTP server graceful shutdown failed: %v", err)
	}
	log.Println("Server gracefully stopped.")
}
