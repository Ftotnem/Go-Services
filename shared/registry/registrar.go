package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Ftotnem/GO-SERVICES/shared/config"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ServiceRegistrar handles the self-registration and heartbeating of a service instance.
type ServiceRegistrar struct {
	redisClient *redis.ClusterClient
	serviceType string               // <--- Now passed explicitly
	cfg         *config.CommonConfig // <--- Use CommonConfig directly
	serviceID   string
	stopChan    chan struct{}
	doneChan    chan struct{}
}

// NewServiceRegistrar creates a new ServiceRegistrar.
// It now takes the serviceType and a reference to the common config.
func NewServiceRegistrar(redisClient *redis.ClusterClient, serviceType string, config *config.CommonConfig) *ServiceRegistrar {
	// Generate a unique ServiceID if not provided
	serviceID := fmt.Sprintf("%s-%s", serviceType, uuid.New().String())

	return &ServiceRegistrar{
		redisClient: redisClient,
		serviceType: serviceType,
		cfg:         config, // Store the pointer to common config
		serviceID:   serviceID,
		stopChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
	}
}

// Start begins the service registration and heartbeating process in a goroutine.
func (sr *ServiceRegistrar) Start() {
	log.Printf("Starting service registrar for %s (ID: %s) at %s:%d",
		sr.serviceType, sr.serviceID, sr.cfg.ServiceIP, sr.cfg.ServicePort) // Use commonConfig

	go sr.run()
}

// Stop signals the registrar to stop its operations and waits for it to finish.
func (sr *ServiceRegistrar) Stop() {
	log.Printf("Signaling service registrar for %s (ID: %s) to stop...", sr.serviceType, sr.serviceID)
	close(sr.stopChan)
	<-sr.doneChan
	log.Printf("Service registrar for %s (ID: %s) stopped successfully.", sr.serviceType, sr.serviceID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hashKey := fmt.Sprintf("%s%s", RedisRegistryHashPrefix, sr.serviceType)
	if _, err := sr.redisClient.HDel(ctx, hashKey, sr.serviceID).Result(); err != nil {
		log.Printf("ERROR: Failed to remove service %s (ID: %s) from Redis registry on shutdown: %v",
			sr.serviceType, sr.serviceID, err)
	} else {
		log.Printf("INFO: Service %s (ID: %s) removed from Redis registry on shutdown.",
			sr.serviceType, sr.serviceID)
	}
}

// run is the main loop for the registrar's background goroutine.
func (sr *ServiceRegistrar) run() {
	defer close(sr.doneChan)

	ticker := time.NewTicker(sr.cfg.HeartbeatInterval) // <--- Use commonConfig
	defer ticker.Stop()

	sr.registerService()

	if sr.cfg.RegistryCleanupInterval > 0 { // <--- Use commonConfig
		sr.startCleanupLoop()
	}

	for {
		select {
		case <-ticker.C:
			sr.registerService()
		case <-sr.stopChan:
			return
		}
	}
}

// registerService performs the actual registration/heartbeat in Redis.
func (sr *ServiceRegistrar) registerService() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	serviceInfo := ServiceInfo{
		ServiceID:   sr.serviceID, // Keep this unique ID generated in NewServiceRegistrar
		ServiceType: sr.serviceType,
		IP:          sr.cfg.ServiceIP,   // <--- Use commonConfig
		Port:        sr.cfg.ServicePort, // <--- Use commonConfig
		LastSeen:    time.Now().Unix(),
		Metadata:    map[string]string{"version": "1.0"}, // Still add metadata if desired
	}

	infoJSON, err := json.Marshal(serviceInfo)
	if err != nil {
		log.Printf("ERROR: Failed to marshal ServiceInfo for %s (ID: %s): %v",
			sr.serviceType, sr.serviceID, err)
		return
	}

	hashKey := fmt.Sprintf("%s%s", RedisRegistryHashPrefix, sr.serviceType)
	if _, err := sr.redisClient.HSet(ctx, hashKey, sr.serviceID, infoJSON).Result(); err != nil {
		log.Printf("ERROR: Failed to register/heartbeat service %s (ID: %s) to Redis: %v",
			sr.serviceType, sr.serviceID, err)
	} else {
		log.Printf("INFO: Service %s (ID: %s) heartbeated successfully.", sr.serviceType, sr.serviceID)
	}
}

// startCleanupLoop starts a background goroutine to periodically clean up stale service entries.
func (sr *ServiceRegistrar) startCleanupLoop() {
	go func() {
		cleanupTicker := time.NewTicker(sr.cfg.RegistryCleanupInterval) // <--- Use commonConfig
		defer cleanupTicker.Stop()
		log.Printf("Starting registry cleanup loop for %s, checking every %v", sr.serviceType, sr.cfg.RegistryCleanupInterval)

		for {
			select {
			case <-cleanupTicker.C:
				sr.performCleanup()
			case <-sr.stopChan:
				log.Println("Registry cleanup loop stopping.")
				return
			}
		}
	}()
}

// performCleanup iterates through registered services and removes stale ones.
func (sr *ServiceRegistrar) performCleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hashKey := fmt.Sprintf("%s%s", RedisRegistryHashPrefix, sr.serviceType)
	results, err := sr.redisClient.HGetAll(ctx, hashKey).Result()
	if err != nil {
		log.Printf("ERROR: Cleanup failed to get all services for type %s: %v", sr.serviceType, err)
		return
	}

	currentTime := time.Now()
	for instanceID, infoJSON := range results {
		var info ServiceInfo
		if err := json.Unmarshal([]byte(infoJSON), &info); err != nil {
			log.Printf("WARNING: Cleanup: Failed to unmarshal ServiceInfo for ID %s (type %s): %v. Deleting.", instanceID, sr.serviceType, err)
			if _, delErr := sr.redisClient.HDel(ctx, hashKey, instanceID).Result(); delErr != nil {
				log.Printf("ERROR: Cleanup: Failed to delete corrupt entry %s for type %s: %v", instanceID, sr.serviceType, delErr)
			}
			continue
		}
		lastSeenTime := time.UnixMilli(info.LastSeen)

		if currentTime.Sub(lastSeenTime) > sr.cfg.HeartbeatTTL { // <--- Use commonConfig
			if _, delErr := sr.redisClient.HDel(ctx, hashKey, instanceID).Result(); delErr != nil {
				log.Printf("ERROR: Cleanup: Failed to delete stale service %s (ID: %s) for type %s: %v",
					info.ServiceType, instanceID, sr.serviceType, delErr)
			} else {
				log.Printf("INFO: Cleanup: Removed stale service %s (ID: %s) from registry.", info.ServiceType, instanceID)
			}
		}
	}
}

// GetServiceID returns the unique ID assigned to this service instance.
func (sr *ServiceRegistrar) GetServiceID() string {
	return sr.serviceID
}

// GetServiceType returns the type of this service instance.
func (sr *ServiceRegistrar) GetServiceType() string {
	return sr.serviceType
}
