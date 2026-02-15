package main

import (
	"context"
	"database/sql"
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
	"github.com/IsaacDSC/goutbox/stores/postgres"
	_ "github.com/lib/pq"
)

func main() {
	// Connect to PostgreSQL
	// Use environment variable or default connection string
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://goutbox:goutbox_secret@localhost:5432/goutbox_db?sslmode=disable"
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	log.Println("Connected to PostgreSQL")

	// Create PostgreSQL store with options
	// Migrations run automatically (idempotent)
	store, err := postgres.NewPostgresStore(db,
		postgres.WithRetryInterval(1*time.Second),
	)
	if err != nil {
		log.Fatalf("failed to create postgres store: %v", err)
	}

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

		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "Event %s queued successfully\n", payload.EventName)
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		log.Println("Starting server on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")

	// Cancel context to stop outbox
	cancel()

	// Wait for outbox to finish processing
	<-outboxDone

	// Graceful shutdown of HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)

	// Close store
	store.Close()

	log.Println("Shutdown complete")
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

// curl -i -X POST http://localhost:8080/event -H "Content-Type: application/json" -d '{"event_name": "event1", "payload": {"key": "value"}}'
