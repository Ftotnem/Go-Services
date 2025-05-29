// shared/registry/types.go
package registry

import "time"

// ServiceInfo represents the details of a registered service instance.
// This information is stored in Redis and used for service discovery.
type ServiceInfo struct {
	ServiceID   string            `json:"serviceId"`          // Unique ID for this specific instance (e.g., a UUID)
	ServiceType string            `json:"serviceType"`        // Type of service (e.g., "game-service", "player-service")
	IP          string            `json:"ip"`                 // IP address where the service is listening
	Port        int               `json:"port"`               // Port where the service is listening
	LastSeen    time.Time         `json:"lastSeen"`           // Timestamp of the last successful heartbeat
	Metadata    map[string]string `json:"metadata,omitempty"` // Optional: additional key-value pairs (e.g., "version", "region")
}
