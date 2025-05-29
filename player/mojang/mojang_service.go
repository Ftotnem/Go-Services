package mojang // This file is part of your main executable

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync" // For waiting on the background goroutine
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	mongodbu "github.com/Ftotnem/GO-SERVICES/shared/mongodb"
)

// --- Mojang Service Core ---

// mojangProfile represents the structure of the JSON response from Mojang's Session Server.
type mojangProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MojangService is the central component for Mojang API interactions and background username filling.
type MojangService struct {
	// For Mojang API calls
	httpClient    *http.Client
	mojangBaseURL string

	// For the background filler job's MongoDB interactions
	playerCollection *mongo.Collection // Directly use the collection for simplicity in this consolidated file
	fillerInterval   time.Duration

	// Control for the background job
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewMojangService creates a new instance of MojangService.
// It sets up the HTTP client for Mojang API calls and configures the MongoDB collection
// for the background username filler job.
func NewMojangService(
	mongoClient *mongodbu.Client, // Dependency injected MongoDB client
	playersCollectionName string, // The MongoDB collection to update
	fillerInterval time.Duration, // How often the filler job should run
) *MojangService {
	return &MojangService{
		httpClient:       &http.Client{Timeout: 5 * time.Second}, // Short timeout for external API
		mojangBaseURL:    "https://sessionserver.mojang.com/session/minecraft/profile",
		playerCollection: mongoClient.Collection(playersCollectionName),
		fillerInterval:   fillerInterval,
		stopChan:         make(chan struct{}), // Initialize stop channel
	}
}

// GetUsernameByUUID fetches a Minecraft username from Mojang's API using the player's UUID.
// This is the direct Mojang API interaction part of the service.
func (ms *MojangService) GetUsernameByUUID(ctx context.Context, uuid string) (string, error) {
	url := fmt.Sprintf("%s/%s", ms.mojangBaseURL, uuid)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create Mojang API request: %w", err)
	}

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make Mojang API request for UUID %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("mojang profile not found for UUID %s (Status: %d)", uuid, resp.StatusCode)
		}
		return "", fmt.Errorf("unexpected status from Mojang API for UUID %s: %d", uuid, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Mojang API response body for UUID %s: %w", uuid, err)
	}

	var profile mojangProfile
	if err := json.Unmarshal(bodyBytes, &profile); err != nil {
		return "", fmt.Errorf("failed to unmarshal Mojang API response for UUID %s: %w", uuid, err)
	}

	if profile.Name == "" {
		return "", fmt.Errorf("mojang API returned empty username for UUID %s", uuid)
	}

	return profile.Name, nil
}

// StartFillerJob begins the background username filler job.
// You would call this once from your main function to start the background process.
func (ms *MojangService) StartFillerJob() {
	ms.wg.Add(1) // Increment wait group counter for the goroutine

	defer ms.wg.Done() // Decrement when goroutine exits

	ticker := time.NewTicker(ms.fillerInterval)
	defer ticker.Stop()

	log.Printf("MojangService: Background username filler job started, running every %v", ms.fillerInterval)

	// Run immediately once, then on ticker intervals
	ms.performSingleFillerIteration()

	for {
		select {
		case <-ticker.C: // Wait for the next tick
			ms.performSingleFillerIteration()
		case <-ms.stopChan: // Signal to stop
			log.Println("MojangService: Background username filler job stopping.")
			return
		}
	}
}

// StopFillerJob signals the background job to cease operations and waits for it to finish.
// You would call this during your application's graceful shutdown.
func (ms *MojangService) StopFillerJob() {
	log.Println("MojangService: Signaling background username filler job to stop...")
	close(ms.stopChan) // Close the channel to signal shutdown
	ms.wg.Wait()       // Wait for the goroutine to complete its work
	log.Println("MojangService: Background username filler job stopped successfully.")
}

// performSingleFillerIteration contains the core logic for one pass of finding and updating usernames.
func (ms *MojangService) performSingleFillerIteration() {
	log.Println("MojangService: Running username filler job iteration...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // Timeout for this iteration
	defer cancel()

	// Find profiles with empty usernames
	filter := bson.M{"username": ""}
	cursor, err := ms.playerCollection.Find(ctx, filter)
	if err != nil {
		log.Printf("MojangService: Error during filler job - finding profiles: %v", err)
		return
	}
	defer cursor.Close(ctx) // Ensure cursor is closed

	type playerProfile struct {
		UUID string `bson:"_id"` // Only need the UUID
	}
	var profilesToUpdate []playerProfile
	if err := cursor.All(ctx, &profilesToUpdate); err != nil {
		log.Printf("MojangService: Error decoding profiles with empty usernames: %v", err)
		return
	}

	if len(profilesToUpdate) == 0 {
		log.Println("MojangService: No profiles with empty usernames found to process.")
		return
	}

	log.Printf("MojangService: Found %d profiles with empty usernames to process.", len(profilesToUpdate))

	for _, p := range profilesToUpdate {
		// Respect context cancellation and add a small delay
		select {
		case <-ctx.Done():
			log.Printf("MojangService: Filler job iteration cancelled for UUID %s: %v", p.UUID, ctx.Err())
			return // Stop processing current batch
		case <-time.After(100 * time.Millisecond): // Pause before next API call to avoid rate limits
			// Continue
		}

		// Fetch username from Mojang
		username, mojangErr := ms.GetUsernameByUUID(ctx, p.UUID) // Use MojangService's own method
		if mojangErr != nil {
			log.Printf("MojangService: WARN: Filler job failed to fetch username for UUID %s: %v", p.UUID, mojangErr)
			continue
		}

		// Update username in MongoDB
		updateFilter := bson.M{"_id": p.UUID}
		updateDoc := bson.M{"$set": bson.M{"username": username}}
		_, updateErr := ms.playerCollection.UpdateOne(ctx, updateFilter, updateDoc, options.Update().SetUpsert(false))
		if updateErr != nil {
			log.Printf("MojangService: WARN: Filler job failed to update username for profile %s in DB: %v", p.UUID, updateErr)
		} else {
			log.Printf("MojangService: INFO: Filler job successfully updated username for profile %s to %s.", p.UUID, username)
		}
	}
}
