package updater

import (
	"context"
	"log"
	"time"

	"github.com/Ftotnem/GO-SERVICES/game/store"             // Your store package for PlayerPlaytimeStore and OnlinePlayersStore
	cluster "github.com/Ftotnem/GO-SERVICES/shared/cluster" // Your cluster package (ServiceAssignmentManager)
	"github.com/Ftotnem/GO-SERVICES/shared/config"          // Import for config.CommonConfig
	"github.com/Ftotnem/GO-SERVICES/shared/registry"        // Your registry package (RegistryClient)
)

// GameUpdater handles the periodic updates for online players' playtime.
type GameUpdater struct {
	config              *config.GameServiceConfig         // Use CommonConfig directly
	registryClient      *registry.RegistryClient          // New: Direct dependency on RegistryClient
	assignmentManager   *cluster.ServiceAssignmentManager // Use ServiceAssignmentManager
	onlinePlayersStore  *store.OnlinePlayersStore         // Dependency for getting online UUIDs
	playerPlaytimeStore *store.PlayerPlaytimeStore        // Dependency for incrementing playtime
	serviceRegistrar    *registry.ServiceRegistrar        // Store my service type
	ctx                 context.Context
	cancel              context.CancelFunc
}

// NewGameUpdater creates a new GameUpdater instance.
// It requires the CommonConfig, RegistryClient, OnlinePlayersStore, PlayerPlaytimeStore,
// and the service ID and type that this *specific* instance will use for registration.
func NewGameUpdater(
	cfg *config.GameServiceConfig,
	registryClient *registry.RegistryClient,
	onlinePlayersStore *store.OnlinePlayersStore,
	playerPlaytimeStore *store.PlayerPlaytimeStore,
	serviceRegistrar *registry.ServiceRegistrar,
) *GameUpdater {
	log.Println("GameUpdater: Initialized.")
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize the ServiceAssignmentManager
	assignmentManager := cluster.NewServiceAssignmentManager(
		registryClient,
		serviceRegistrar,
		cfg.HeartbeatInterval, // Using heartbeat interval for consistent hash updates
	)

	gu := &GameUpdater{
		config:              cfg,
		registryClient:      registryClient,
		assignmentManager:   assignmentManager,
		onlinePlayersStore:  onlinePlayersStore,
		playerPlaytimeStore: playerPlaytimeStore,
		serviceRegistrar:    serviceRegistrar,
		ctx:                 ctx,
		cancel:              cancel,
	}
	log.Printf("DEBUG: Configured TickInterval before updater start: %v", gu.config.TickInterval)
	return gu
}

// Start initiates the game update loop. This should be run in a goroutine.
func (gu *GameUpdater) Start() {
	log.Printf("Game Updater starting with tick interval: %v", gu.config.TickInterval)
	ticker := time.NewTicker(gu.config.TickInterval)
	defer ticker.Stop()

	// Start the ServiceAssignmentManager's update loop in a goroutine
	go gu.assignmentManager.Start()

	for {
		select {
		case <-gu.ctx.Done():
			log.Println("Game Updater shutting down.")
			gu.assignmentManager.Stop() // Stop the assignment manager when GameUpdater stops
			return
		case <-ticker.C:
			gu.performGameTick()
		}
	}
}

// Stop gracefully stops the game update loop.
func (gu *GameUpdater) Stop() {
	gu.cancel()
}

// performGameTick executes the logic for a single game tick.
func (gu *GameUpdater) performGameTick() {
	// Use GetAllOnlinePlayers and then extract UUIDs
	onlinePlayersMap, err := gu.onlinePlayersStore.GetAllOnlinePlayers(gu.ctx)
	if err != nil {
		log.Printf("Error getting online players map for game tick: %v", err)
		return
	}

	if len(onlinePlayersMap) == 0 {
		return
	}

	// Extract just the UUIDs from the map keys
	onlineUUIDs := make([]string, 0, len(onlinePlayersMap))
	for uuid := range onlinePlayersMap {
		onlineUUIDs = append(onlineUUIDs, uuid)
	}

	playersToUpdate := make([]string, 0, len(onlineUUIDs))

	for _, uuid := range onlineUUIDs {
		isResponsible, err := gu.assignmentManager.IsResponsible(uuid)
		if err != nil {
			log.Printf("WARNING: GameUpdater: Failed to check responsibility for UUID %s: %v", uuid, err)
			continue
		}

		if isResponsible {
			playersToUpdate = append(playersToUpdate, uuid)
		}
	}

	if len(playersToUpdate) == 0 {
		return
	}

	//log.Printf("Performing game tick for %d players assigned to this instance.", len(playersToUpdate))
	
	for _, uuid := range playersToUpdate {
		if err := gu.playerPlaytimeStore.IncrementPlayerPlaytime(gu.ctx, uuid); err != nil {
			log.Printf("Error incrementing total playtime for %s: %v", uuid, err)
		}
	}
}
