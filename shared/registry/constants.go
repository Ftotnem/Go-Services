// shared/registry/constants.go
package registry

const (
	// RedisRegistryHashPrefix is the prefix used for Redis hash keys that store
	// service registration data. The full key format will be:
	// "services:<serviceType>"
	// Example: "services:game-service"
	RedisRegistryHashPrefix = "services:"

	// Add any other common registry-related constants here
)
