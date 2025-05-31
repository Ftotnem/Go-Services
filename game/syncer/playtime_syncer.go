// game/syncer/playtime_syncer.go
package syncer

import (
	"context"
	"log"
	"time"

	"github.com/Ftotnem/GO-SERVICES/game/store"
	"github.com/Ftotnem/GO-SERVICES/shared/cluster"
	"github.com/Ftotnem/GO-SERVICES/shared/config"
	"github.com/Ftotnem/GO-SERVICES/shared/registry"
	player_service_client "github.com/Ftotnem/GO-SERVICES/shared/service" // Your HTTP Player Service client
)

// PlaytimeSyncer handles the periodic backup of player playtimes to the Player Service
// and synchronization of aggregated team totals from the Player Service back to Redis.
// It uses ServiceAssignmentManager to ensure only one instance in the cluster performs these global tasks.
type PlaytimeSyncer struct {
	config              *config.GameServiceConfig
	playerPlaytimeStore *store.PlayerPlaytimeStore
	teamPlaytimeStore   *store.TeamPlaytimeStore
	playerServiceClient player_service_client.PlayerServiceClient // HTTP client to Player Service
	assignmentManager   *cluster.ServiceAssignmentManager
	serviceRegistrar    *registry.ServiceRegistrar // Used for ServiceAssignmentManager initialization
	ctx                 context.Context
	cancel              context.CancelFunc
}

// NewPlaytimeSyncer creates a new PlaytimeSyncer instance.
// It relies on ServiceAssignmentManager to determine leadership for global sync tasks.
func NewPlaytimeSyncer(
	cfg *config.GameServiceConfig,
	playerPlaytimeStore *store.PlayerPlaytimeStore,
	teamPlaytimeStore *store.TeamPlaytimeStore,
	playerServiceClient player_service_client.PlayerServiceClient,
	registryClient *registry.RegistryClient, // Needed for ServiceAssignmentManager
	serviceRegistrar *registry.ServiceRegistrar,
) *PlaytimeSyncer {
	log.Println("PlaytimeSyncer: Initializing.")
	ctx, cancel := context.WithCancel(context.Background())

	// The assignment manager will be used to elect a leader for the global sync task.
	assignmentManager := cluster.NewServiceAssignmentManager(
		registryClient,
		serviceRegistrar,
		cfg.HeartbeatInterval, // Use heartbeat interval for consistent hash updates
	)

	return &PlaytimeSyncer{
		config:              cfg,
		playerPlaytimeStore: playerPlaytimeStore,
		teamPlaytimeStore:   teamPlaytimeStore,
		playerServiceClient: playerServiceClient,
		assignmentManager:   assignmentManager,
		serviceRegistrar:    serviceRegistrar,
		ctx:                 ctx,
		cancel:              cancel,
	}
}

// Start initiates the synchronization loop. This should be run in a goroutine.
func (ps *PlaytimeSyncer) Start() {
	// Using ps.config.BackupInterval for the ticker frequency.
	log.Printf("Playtime Syncer starting with sync interval: %v", ps.config.PersistenceInterval)
	ticker := time.NewTicker(ps.config.PersistenceInterval)
	defer ticker.Stop()

	// Start the ServiceAssignmentManager's update loop in a goroutine.
	go ps.assignmentManager.Start()

	for {
		select {
		case <-ps.ctx.Done():
			log.Println("Playtime Syncer shutting down.")
			ps.assignmentManager.Stop() // Stop the assignment manager when Syncer stops
			return
		case <-ticker.C:
			ps.performGlobalSync()
		}
	}
}

// Stop gracefully stops the synchronization loop.
func (ps *PlaytimeSyncer) Stop() {
	ps.cancel()
}

// performGlobalSync executes the backup and team sync logic.
// Only the cluster leader (determined by assignmentManager for a specific key) will perform this.
func (ps *PlaytimeSyncer) performGlobalSync() {
	// Use a unique, consistent key for the global sync task to ensure only one service instance picks it up.
	const globalSyncTaskKey = "global_playtime_sync_task"

	isLeader, err := ps.assignmentManager.IsResponsible(globalSyncTaskKey)
	if err != nil {
		log.Printf("ERROR: PlaytimeSyncer: Failed to check leadership for task '%s': %v", globalSyncTaskKey, err)
		return
	}

	if !isLeader {
		return // Not the responsible instance for this global task, so do nothing.
	}

	log.Printf("INFO: This instance is the leader for global playtime sync. Performing backup and team totals update.")

	// --- 1. Backup all current player playtimes from Redis to Player Service (MongoDB) ---
	// Create a context for the individual player updates during the backup.
	// Using ps.config.BackupTimeout for the total duration of this backup phase.
	backupCtx, backupCancel := context.WithTimeout(ps.ctx, ps.config.BackupTimeout)
	defer backupCancel()

	allPlayerPlaytimes, err := ps.playerPlaytimeStore.GetAllPlayerPlaytimes(backupCtx)
	if err != nil {
		log.Printf("ERROR: Syncer: Failed to get all player playtimes from Redis for backup: %v", err)
		// Continue to team sync even if player playtime backup fails.
	} else if len(allPlayerPlaytimes) > 0 {
		log.Printf("INFO: Syncer: Individually backing up %d player playtimes to Player Service.", len(allPlayerPlaytimes))
		// Loop through each player and call the individual UpdatePlayerPlaytime method.
		for uuid, totalPlaytime := range allPlayerPlaytimes {
			// Check if context has been canceled (e.g., timeout) before proceeding
			select {
			case <-backupCtx.Done():
				log.Printf("WARNING: Syncer: Backup context canceled during individual player playtime updates: %v", backupCtx.Err())
				goto endBackupLoop // Exit the loop and proceed to team sync
			default:
				// Continue
			}

			// Assuming your PlayerServiceClient has an UpdatePlayerPlaytime method that takes UUID and playtime
			err := ps.playerServiceClient.UpdatePlayerPlaytime(backupCtx, uuid, totalPlaytime)
			if err != nil {
				log.Printf("ERROR: Syncer: Failed to update playtime for player %s in Player Service: %v", uuid, err)
				// Log the error but continue to try other players.
			}
		}
		log.Println("INFO: Syncer: Individual player playtime backup completed.")
	} else {
		log.Println("INFO: Syncer: No player playtimes found in Redis to backup.")
	}

endBackupLoop: // Label to jump to if backupCtx is done.

	// --- 2. Trigger team total aggregation in Player Service and update Redis with results ---
	// Using ps.config.SyncTimeout for the context of the team sync operation.
	syncCtx, syncCancel := context.WithTimeout(ps.ctx, ps.config.SyncTimeout)
	defer syncCancel()

	// Use your existing playerServiceClient.SyncTeamTotals method
	resp, err := ps.playerServiceClient.SyncTeamTotals(syncCtx)
	if err != nil {
		log.Printf("ERROR: Syncer: Failed to trigger player service team totals sync: %v", err)
		return // Crucial error, cannot update Redis with stale data if sync failed
	}

	log.Println("INFO: Syncer: Successfully triggered player service team totals sync. Updating Redis with aggregated team totals...")

	if resp == nil || len(resp.TeamTotals) == 0 { // Check resp and its TeamTotals field
		log.Println("INFO: Syncer: No team totals received from player service sync.")
		return
	}

	// Update Redis with the received authoritative team totals
	for teamID, totalPlaytime := range resp.TeamTotals { // Iterate over resp.TeamTotals
		// Check if context has been canceled (e.g., timeout) before proceeding
		select {
		case <-syncCtx.Done():
			log.Printf("WARNING: Syncer: Team sync context canceled during Redis updates: %v", syncCtx.Err())
			return // Exit the loop and function
		default:
			// Continue
		}

		err := ps.teamPlaytimeStore.SetTeamPlaytime(syncCtx, teamID, totalPlaytime) // Overwrite existing Redis value
		if err != nil {
			log.Printf("ERROR: Syncer: Failed to update Redis for team %s total playtime: %v", teamID, err)
		} else {
			log.Printf("INFO: Syncer: Successfully updated Redis total playtime for team '%s' to %.2f ticks.", teamID, totalPlaytime)
		}
	}
	log.Println("INFO: Syncer: Finished updating Redis with aggregated team totals.")
}
