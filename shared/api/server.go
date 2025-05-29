// shared/api/server.go (Revised)
package api

import (
	"context"
	"fmt"
	"log" // For internal server logging
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type BaseServer struct {
	Router *mux.Router
	Server *http.Server
	Logger *log.Logger // Add a logger for server-specific messages
}

func NewBaseServer(addr string, logger *log.Logger) *BaseServer {
	if logger == nil {
		logger = log.Default() // Use default logger if none provided
	}

	router := mux.NewRouter()

	// Apply common middleware
	router.Use(LoggingMiddleware) // LoggingMiddleware now uses `log`
	router.Use(CORSMiddleware)

	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &BaseServer{
		Router: router,
		Server: server,
		Logger: logger,
	}
}

func (bs *BaseServer) Start() error {
	bs.Logger.Printf("Starting HTTP server on %s...", bs.Server.Addr)
	// ListenAndServe returns http.ErrServerClosed on graceful shutdown
	if err := bs.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server failed: %w", err)
	}
	return nil
}

func (bs *BaseServer) Shutdown(ctx context.Context) error {
	bs.Logger.Println("Shutting down HTTP server...")
	return bs.Server.Shutdown(ctx)
}
