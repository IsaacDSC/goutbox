package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IsaacDSC/goutbox"
	_ "github.com/lib/pq" // PostgreSQL driver
)

// migrationSQL contains the embedded SQL migration script.
// The same file is used by docker-compose for initialization.
// This ensures consistency between development and production environments.
//
//go:embed migrations/init.sql
var migrationSQL string

// TaskStatus represents the state of a task in the outbox
type TaskStatus string

const (
	// StatusPending indicates the task is waiting to be processed
	StatusPending TaskStatus = "pending"

	// StatusProcessing indicates the task is currently being processed
	StatusProcessing TaskStatus = "processing"

	// StatusCompleted indicates the task was processed successfully
	StatusCompleted TaskStatus = "completed"

	// StatusFailed indicates the task failed after all retry attempts
	StatusFailed TaskStatus = "failed"
)

// String returns the string representation of the status
func (s TaskStatus) String() string {
	return string(s)
}

// IsValid checks if the status is a valid value
func (s TaskStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusProcessing, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

// IsFinal checks if the status represents a final state (cannot change anymore)
func (s TaskStatus) IsFinal() bool {
	return s == StatusCompleted || s == StatusFailed
}

// PostgresStore implements the goutbox.Store interface using PostgreSQL as backend.
// Provides durable persistence and support for multiple application instances.
// Table used: outbox_tasks (fixed for simplicity and security).
type PostgresStore struct {
	// db is the PostgreSQL database connection
	db *sql.DB

	// retryInterval defines the minimum time a task must wait before being retried
	// Default: 500ms
	retryInterval time.Duration

	// skipMigration when true, does not run automatic migrations when creating the store
	// Useful when migrations are managed externally (e.g., Flyway, golang-migrate)
	// Default: false
	skipMigration bool
}

// PostgresOption is a function that configures the PostgresStore
// Follows the Functional Options pattern for flexible configuration
type PostgresOption func(*PostgresStore)

// WithRetryInterval sets the minimum interval between retry attempts
// Tasks will only be returned by GetTasksWithError after this interval
func WithRetryInterval(d time.Duration) PostgresOption {
	return func(ps *PostgresStore) {
		if d > 0 {
			ps.retryInterval = d
		}
	}
}

// WithSkipMigration disables automatic migration execution.
// Use when migrations are managed by an external tool
// such as Flyway, golang-migrate, or when the table was already created manually.
func WithSkipMigration() PostgresOption {
	return func(ps *PostgresStore) {
		ps.skipMigration = true
	}
}

// NewPostgresStore creates a new PostgresStore instance.
// Receives a database connection and configuration options.
// By default, runs migrations automatically (idempotent - safe to run multiple times).
// Use WithSkipMigration() to disable automatic migrations.
func NewPostgresStore(db *sql.DB, opts ...PostgresOption) (*PostgresStore, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	ps := &PostgresStore{
		db:            db,
		retryInterval: 500 * time.Millisecond,
	}

	// Apply configuration options
	for _, opt := range opts {
		opt(ps)
	}

	// Run migrations only if not disabled
	// Migrations are idempotent (IF NOT EXISTS), safe to run multiple times
	if !ps.skipMigration {
		if err := ps.migrate(); err != nil {
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	return ps, nil
}

// Create stores a new task in the database with pending status.
// If a task with the same key already exists in pending status, increments the attempts counter.
func (ps *PostgresStore) Create(ctx context.Context, task goutbox.Task) error {
	// Serialize payload to JSONB
	payloadJSON, err := json.Marshal(task.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Values ($1-$4) are parameterized to prevent SQL injection
	const query = `
		INSERT INTO outbox_tasks (key, payload, max_attempts, status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) WHERE status = 'pending'
		DO UPDATE SET 
			attempts = outbox_tasks.attempts + 1,
			updated_at = NOW()
	`

	// Use StatusPending constant to ensure consistency
	_, err = ps.db.ExecContext(ctx, query, task.Key, payloadJSON, task.Config.MaxAttempts, StatusPending)
	return err
}

// GetTasksWithError retrieves tasks with pending status that are ready for retry.
// Returns only tasks whose updated_at is older than the configured retryInterval.
// Limits to 100 tasks per query to avoid overload.
func (ps *PostgresStore) GetTasksWithError(ctx context.Context) ([]goutbox.Task, error) {
	// Values ($1, $2) are parameterized to prevent SQL injection
	const query = `
		SELECT key, payload, errors, attempts, max_attempts
		FROM outbox_tasks
		WHERE status = $1
		AND updated_at < NOW() - make_interval(secs => $2)
		ORDER BY updated_at ASC
		LIMIT 100
	`

	// Convert retryInterval to seconds (float64 to support milliseconds)
	intervalSeconds := ps.retryInterval.Seconds()
	rows, err := ps.db.QueryContext(ctx, query, StatusPending, intervalSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []goutbox.Task
	for rows.Next() {
		var task goutbox.Task
		var payloadJSON, errorsJSON []byte
		var maxAttempts int

		if err := rows.Scan(&task.Key, &payloadJSON, &errorsJSON, &task.Attempts, &maxAttempts); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Deserialize JSONB payload
		if err := json.Unmarshal(payloadJSON, &task.Payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
		}

		task.Config.MaxAttempts = maxAttempts
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

// UpdateTaskError updates a task with error information after a failed attempt.
// Increments the attempts counter and updates the timestamp for retry control.
func (ps *PostgresStore) UpdateTaskError(ctx context.Context, task goutbox.Task) error {
	// Serialize errors for JSONB storage
	errorsJSON, err := json.Marshal(formatErrors(task.Error))
	if err != nil {
		return fmt.Errorf("failed to marshal errors: %w", err)
	}

	// Values ($1-$4) are parameterized to prevent SQL injection
	const query = `
		UPDATE outbox_tasks
		SET errors = $1,
			attempts = $2,
			updated_at = NOW()
		WHERE key = $3 AND status = $4
	`

	_, err = ps.db.ExecContext(ctx, query, errorsJSON, task.Attempts, task.Key, StatusPending)
	return err
}

// formatErrors converts a slice of errors to a slice of strings for JSON serialization
func formatErrors(errs []error) []string {
	result := make([]string, len(errs))
	for i, err := range errs {
		if err != nil {
			result[i] = err.Error()
		}
	}
	return result
}

// DiscardTask marks a task as finished.
// If isOk=true, marks as StatusCompleted (success).
// If isOk=false, marks as StatusFailed (permanent failure).
func (ps *PostgresStore) DiscardTask(ctx context.Context, key string, isOk bool) error {
	// Determine final status based on result
	var status TaskStatus
	if isOk {
		status = StatusCompleted
	} else {
		status = StatusFailed
	}

	// Values ($1-$3) are parameterized to prevent SQL injection
	const query = `
		UPDATE outbox_tasks
		SET status = $1,
			updated_at = NOW()
		WHERE key = $2 AND status = $3
	`

	_, err := ps.db.ExecContext(ctx, query, status, key, StatusPending)
	return err
}

// Close closes the database connection.
// Should be called when the store is no longer needed to release resources.
func (ps *PostgresStore) Close() error {
	return ps.db.Close()
}

// migrate creates the table and indexes if they don't exist.
// This function is IDEMPOTENT: can be executed multiple times without error.
// Uses the embedded SQL file (migrations/init.sql) via go:embed.
// The same SQL file is used by docker-compose.
func (ps *PostgresStore) migrate() error {
	_, err := ps.db.Exec(migrationSQL)
	return err
}
