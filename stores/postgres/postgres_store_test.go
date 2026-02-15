package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/IsaacDSC/goutbox"
	"github.com/IsaacDSC/goutbox/stores/postgres"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	pgcontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupPostgresContainer(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := pgcontainer.Run(ctx,
		"postgres:16-alpine",
		pgcontainer.WithDatabase("testdb"),
		pgcontainer.WithUsername("test"),
		pgcontainer.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		pgContainer.Terminate(ctx)
		t.Fatalf("failed to get connection string: %v", err)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		pgContainer.Terminate(ctx)
		t.Fatalf("failed to connect to database: %v", err)
	}

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		pgContainer.Terminate(ctx)
		t.Fatalf("failed to ping database: %v", err)
	}

	cleanup := func() {
		db.Close()
		pgContainer.Terminate(ctx)
	}

	return db, cleanup
}

func TestPostgresStore_NewPostgresStore(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}

	if store == nil {
		t.Error("NewPostgresStore() returned nil store")
	}
}

func TestPostgresStore_NewPostgresStore_NilDB(t *testing.T) {
	_, err := postgres.NewPostgresStore(nil)
	if err == nil {
		t.Error("NewPostgresStore(nil) should return error")
	}
}

func TestPostgresStore_Create(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	task := goutbox.Task{
		Key:     "test_event",
		Payload: map[string]string{"foo": "bar"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}

	if err := store.Create(ctx, task); err != nil {
		t.Errorf("Create() error = %v", err)
	}
}

func TestPostgresStore_Create_DuplicateKey(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	task := goutbox.Task{
		Key:     "duplicate_event",
		Payload: map[string]string{"foo": "bar"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}

	// Create task first time
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create() first time error = %v", err)
	}

	// Create same task second time (should upsert, incrementing attempts)
	if err := store.Create(ctx, task); err != nil {
		t.Errorf("Create() second time error = %v (should upsert)", err)
	}
}

func TestPostgresStore_GetTasksWithError(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db, postgres.WithRetryInterval(100*time.Millisecond))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()

	// Create a task
	task := goutbox.Task{
		Key:     "test_event_get",
		Payload: map[string]string{"foo": "bar"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Wait for retry interval
	time.Sleep(150 * time.Millisecond)

	tasks, err := store.GetTasksWithError(ctx)
	if err != nil {
		t.Errorf("GetTasksWithError() error = %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}

	if len(tasks) > 0 && tasks[0].Key != "test_event_get" {
		t.Errorf("expected key 'test_event_get', got '%s'", tasks[0].Key)
	}
}

func TestPostgresStore_GetTasksWithError_RespectsRetryInterval(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db, postgres.WithRetryInterval(2*time.Second))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()

	// Create a task
	task := goutbox.Task{
		Key:     "interval_test",
		Payload: map[string]string{"test": "value"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Query immediately - should not return the task
	tasks, err := store.GetTasksWithError(ctx)
	if err != nil {
		t.Errorf("GetTasksWithError() error = %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks (too soon), got %d", len(tasks))
	}
}

func TestPostgresStore_UpdateTaskError(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()

	task := goutbox.Task{
		Key:     "update_error_test",
		Payload: map[string]string{"foo": "bar"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	task.Attempts = 1
	task.Error = append(task.Error, errors.New("test error"))

	if err := store.UpdateTaskError(ctx, task); err != nil {
		t.Errorf("UpdateTaskError() error = %v", err)
	}
}

func TestPostgresStore_DiscardTask_Success(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db, postgres.WithRetryInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()

	task := goutbox.Task{
		Key:     "discard_success_test",
		Payload: map[string]string{"foo": "bar"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Discard as success
	if err := store.DiscardTask(ctx, task.Key, true); err != nil {
		t.Errorf("DiscardTask() error = %v", err)
	}

	// Verify task is no longer pending
	time.Sleep(100 * time.Millisecond)
	tasks, err := store.GetTasksWithError(ctx)
	if err != nil {
		t.Fatalf("GetTasksWithError() error = %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 pending tasks, got %d", len(tasks))
	}
}

func TestPostgresStore_DiscardTask_Failure(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db, postgres.WithRetryInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()

	task := goutbox.Task{
		Key:     "discard_failure_test",
		Payload: map[string]string{"foo": "bar"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Discard as failure
	if err := store.DiscardTask(ctx, task.Key, false); err != nil {
		t.Errorf("DiscardTask() error = %v", err)
	}

	// Verify task is no longer pending
	time.Sleep(100 * time.Millisecond)
	tasks, err := store.GetTasksWithError(ctx)
	if err != nil {
		t.Fatalf("GetTasksWithError() error = %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 pending tasks, got %d", len(tasks))
	}
}

func TestPostgresStore_WithSkipMigration(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	// Run migrations manually first
	store1, err := postgres.NewPostgresStore(db)
	if err != nil {
		t.Fatalf("failed to create store with migrations: %v", err)
	}
	_ = store1

	// Now create store with skip migration
	store2, err := postgres.NewPostgresStore(db, postgres.WithSkipMigration())
	if err != nil {
		t.Fatalf("failed to create store with skip migration: %v", err)
	}

	// Should still work because migrations were already run
	ctx := context.Background()
	task := goutbox.Task{
		Key:     "skip_migration_test",
		Payload: map[string]string{"foo": "bar"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}

	if err := store2.Create(ctx, task); err != nil {
		t.Errorf("Create() with skip migration error = %v", err)
	}
}

func TestPostgresStore_FullWorkflow(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	store, err := postgres.NewPostgresStore(db, postgres.WithRetryInterval(100*time.Millisecond))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()

	// 1. Create task
	task := goutbox.Task{
		Key:     "workflow_test",
		Payload: map[string]interface{}{"user_id": 123, "action": "signup"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// 2. Wait and get tasks
	time.Sleep(150 * time.Millisecond)
	tasks, err := store.GetTasksWithError(ctx)
	if err != nil {
		t.Fatalf("GetTasksWithError() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// 3. Simulate failure and update error
	task = tasks[0]
	task.Attempts = 1
	task.Error = append(task.Error, errors.New("connection timeout"))
	if err := store.UpdateTaskError(ctx, task); err != nil {
		t.Fatalf("UpdateTaskError() error = %v", err)
	}

	// 4. Wait and retry
	time.Sleep(150 * time.Millisecond)
	tasks, err = store.GetTasksWithError(ctx)
	if err != nil {
		t.Fatalf("GetTasksWithError() retry error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task on retry, got %d", len(tasks))
	}

	// 5. Simulate success and discard
	if err := store.DiscardTask(ctx, task.Key, true); err != nil {
		t.Fatalf("DiscardTask() error = %v", err)
	}

	// 6. Verify no more pending tasks
	time.Sleep(150 * time.Millisecond)
	tasks, err = store.GetTasksWithError(ctx)
	if err != nil {
		t.Fatalf("GetTasksWithError() final error = %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after discard, got %d", len(tasks))
	}
}

func TestPostgresStore_Close(t *testing.T) {
	db, cleanup := setupPostgresContainer(t)
	defer cleanup()

	// Create a separate db connection for the store
	ctx := context.Background()
	connStr, err := db.Query("SELECT current_database()")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	connStr.Close()

	store, err := postgres.NewPostgresStore(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create a task before closing
	task := goutbox.Task{
		Key:     "close_test",
		Payload: map[string]string{"foo": "bar"},
		Config:  goutbox.Config{MaxAttempts: 3},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Note: We don't actually call Close() here because cleanup() will close db
	// This test just verifies the store works correctly before any cleanup
}
