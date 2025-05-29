// shared/mongodb/client.go
package mongodb

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Client represents a wrapper around *mongo.Client for easier management.
type Client struct {
	mongoClient *mongo.Client
	database    string
}

// NewClient establishes a connection to the MongoDB server and returns a new Client instance.
func NewClient(connStr, databaseName string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(connStr))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping the primary to ensure connection is established
	err = client.Ping(ctx, readpref.Primary())
	if err != nil {
		// Disconnect if ping fails
		if disconnectErr := client.Disconnect(context.Background()); disconnectErr != nil {
			log.Printf("Warning: Failed to disconnect MongoDB client after ping failure: %v", disconnectErr)
		}
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	log.Println("Successfully connected to MongoDB!")
	return &Client{
		mongoClient: client,
		database:    databaseName,
	}, nil
}

// Collection returns a mongo.Collection for the specified collection name.
func (mc *Client) Collection(collectionName string) *mongo.Collection {
	return mc.mongoClient.Database(mc.database).Collection(collectionName)
}

// Disconnect closes the MongoDB client connection.
func (mc *Client) Disconnect(ctx context.Context) error {
	log.Println("Disconnecting from MongoDB...")
	return mc.mongoClient.Disconnect(ctx)
}

// RawClient provides access to the underlying *mongo.Client for advanced operations.
func (mc *Client) RawClient() *mongo.Client {
	return mc.mongoClient
}
