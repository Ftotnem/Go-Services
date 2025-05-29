// shared/cluster/assignment_manager.go
package cluster

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/Ftotnem/GO-SERVICES/shared/registry"
	"github.com/stathat/consistent" // Your consistent hashing library
)

// ServiceAssignmentManager helps a service instance determine if it's responsible
// for a given entity (e.g., player, team) based on consistent hashing across active instances.
type ServiceAssignmentManager struct {
	registryClient   *registry.RegistryClient   // To get active service instances
	serviceRegistrar *registry.ServiceRegistrar // The type of service (e.g., "game-service", "chat-service")
	updateInterval   time.Duration              // How often to update the consistent hash ring
	consistentHash   *consistent.Consistent     // The consistent hash ring
	chMux            sync.RWMutex               // Protects access to consistentHash
	ctx              context.Context            // Context for managing lifecycle
	cancel           context.CancelFunc         // Cancel function for the context
}

// NewServiceAssignmentManager creates and initializes a new ServiceAssignmentManager.
// It requires an initialized RegistryClient, the ID and type of the current service,
// and how often the consistent hash ring should be updated.
func NewServiceAssignmentManager(
	registryClient *registry.RegistryClient,
	serviceRegistrar *registry.ServiceRegistrar,
	updateInterval time.Duration,
) *ServiceAssignmentManager {
	ctx, cancel := context.WithCancel(context.Background())

	sam := &ServiceAssignmentManager{
		registryClient:   registryClient,
		serviceRegistrar: serviceRegistrar,
		updateInterval:   updateInterval,
		consistentHash:   consistent.New(), // Initialize the consistent hash ring
		ctx:              ctx,
		cancel:           cancel,
	}

	// Add this instance to the ring initially
	sam.chMux.Lock()
	sam.consistentHash.Add(sam.serviceRegistrar.GetServiceID())
	sam.chMux.Unlock()

	log.Printf("ServiceAssignmentManager initialized for service '%s' (ID: %s) with update interval: %v",
		serviceRegistrar.GetServiceType(), serviceRegistrar.GetServiceID(), updateInterval)
	return sam
}

// Start begins the periodic update of the consistent hash ring.
// This method should be run in a goroutine.
func (sam *ServiceAssignmentManager) Start() {
	ticker := time.NewTicker(sam.updateInterval)
	defer ticker.Stop()

	log.Printf("ServiceAssignmentManager: Consistent Hash Updater loop started for service type '%s'.", sam.serviceRegistrar.GetServiceType())

	for {
		select {
		case <-sam.ctx.Done():
			log.Println("ServiceAssignmentManager: Consistent Hash Updater loop shutting down.")
			return
		case <-ticker.C:
			sam.updateConsistentHashRing()
		}
	}
}

// Stop gracefully shuts down the ServiceAssignmentManager.
func (sam *ServiceAssignmentManager) Stop() {
	sam.cancel()
}

// updateConsistentHashRing fetches current active services of its type
// and rebuilds the consistent hash ring if the set of active members has changed.
func (sam *ServiceAssignmentManager) updateConsistentHashRing() {
	activeServices, err := sam.registryClient.GetActiveServices(sam.ctx, sam.serviceRegistrar.GetServiceType())
	if err != nil {
		log.Printf("ERROR: ServiceAssignmentManager: Failed to get active services for type '%s': %v", sam.serviceRegistrar.GetServiceType(), err)
		return
	}

	// Extract only the instance IDs
	members := make([]string, 0, len(activeServices))
	for id := range activeServices {
		members = append(members, id)
	}
	slices.Sort(members) // Sort to ensure consistent comparison

	sam.chMux.Lock()
	defer sam.chMux.Unlock()

	currentMembers := sam.consistentHash.Members()
	slices.Sort(currentMembers) // Sort to ensure consistent comparison

	// Compare sorted slices to check if the set of members has truly changed
	if !slices.Equal(members, currentMembers) {
		newHashRing := consistent.New() // Create a new consistent hash instance
		for _, member := range members {
			newHashRing.Add(member)
		}
		sam.consistentHash = newHashRing // Replace the old ring with the new one

		log.Printf("ServiceAssignmentManager: Consistent Hash ring updated for '%s'. Active members: %v", sam.serviceRegistrar.GetServiceType(), newHashRing.Members())
	}
}

// IsResponsible checks if the current service instance is responsible for the given entity ID.
// It uses the consistent hash ring to determine which service instance is assigned to the entity.
func (sam *ServiceAssignmentManager) IsResponsible(entityID string) (bool, error) {
	sam.chMux.RLock() // Use RLock for read access
	defer sam.chMux.RUnlock()

	if len(sam.consistentHash.Members()) == 0 {
		// This can happen briefly during startup or if no services are registered.
		log.Printf("WARNING: ServiceAssignmentManager: Consistent hash ring for '%s' is empty. Cannot determine responsibility for entity '%s'.", sam.serviceRegistrar.GetServiceType(), entityID)
		return false, fmt.Errorf("consistent hash ring is empty for service type %s", sam.serviceRegistrar.GetServiceType())
	}

	responsibleService, err := sam.consistentHash.Get(entityID)
	if err != nil {
		// This error typically means the ring is empty, but we already check for that.
		// Can also happen if `consistent.New()` returns an invalid ring somehow, very unlikely.
		return false, fmt.Errorf("failed to get responsible service for entity '%s' (type %s): %w", entityID, sam.serviceRegistrar.GetServiceType(), err)
	}

	return responsibleService == sam.serviceRegistrar.GetServiceID(), nil
}
