package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IsaacDSC/goutbox"
	"github.com/IsaacDSC/goutbox/stores"
)

func main() {
	store := stores.NewMemStore()

	// Context with cancellation to control shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Configure outbox with custom loop interval (default is 1 minute)
	outbox := goutbox.New(store, goutbox.WithLoopInterval(30*time.Second))

	// Channel to signal that outbox has finished
	outboxDone := make(chan struct{})
	go func() {
		outbox.Start(ctx, publisher)
		close(outboxDone)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /event", func(w http.ResponseWriter, r *http.Request) {
		// handle event
		var payload struct {
			EventName string         `json:"event_name"`
			Payload   map[string]any `json:"payload"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Use WithMaxAttempts to configure retry attempts per event (default is 3)
		err := outbox.Do(r.Context(), payload.EventName, payload.Payload,
			func(ctx context.Context, eventName string, payload any) error {
				return publisher(ctx, eventName, payload)
			},
			goutbox.WithMaxAttempts(5),
		)

		if err != nil {
			log.Printf("failed to handle event: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Channel to capture system signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start the server in a goroutine
	go func() {
		log.Println("Server starting on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-quit
	log.Println("Shutting down server...")

	// Context with timeout for server shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Graceful shutdown of HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}
	log.Println("HTTP server stopped")

	// Cancel outbox context to stop processing
	cancel()

	// Wait for outbox to finish
	<-outboxDone
	log.Println("Outbox stopped")

	// Close the store
	store.Close()
	log.Println("Store closed")

	log.Println("Graceful shutdown completed")
}

// simulatePublisher simulates event publishing with random failures
func publisher(ctx context.Context, eventName string, payload any) error {
	// 50% chance of error
	isError := rand.Intn(2) == 1

	if isError {
		log.Printf("failed to publish event: %s, payload: %v", eventName, payload)
		return fmt.Errorf("failed to publish event: %s", eventName)
	}

	log.Printf("successfully published event: %s, payload: %v", eventName, payload)

	return nil
}

// curl -X -i POST http://localhost:8080/event -H "Content-Type: application/json" -d '{"event_name": "event1", "payload": {"key": "value"}}'
