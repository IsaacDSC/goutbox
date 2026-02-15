-- Migration for creating the outbox table
-- This file is embedded in the binary via go:embed
-- And also used by docker-compose for initialization
-- Table name: outbox_tasks (fixed for simplicity and to prevent SQL injection)

-- Create table only if it doesn't exist (idempotent)
CREATE TABLE IF NOT EXISTS outbox_tasks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key          VARCHAR(255) NOT NULL,
    payload      JSONB NOT NULL,
    errors       JSONB DEFAULT '[]',
    attempts     INTEGER DEFAULT 0,
    max_attempts INTEGER DEFAULT 3,
    status       VARCHAR(50) DEFAULT 'pending',
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for filtering by status
CREATE INDEX IF NOT EXISTS idx_outbox_tasks_status ON outbox_tasks(status);

-- Index for ordering by updated_at (retry ordering)
CREATE INDEX IF NOT EXISTS idx_outbox_tasks_updated_at ON outbox_tasks(updated_at);

-- Index for searching by key
CREATE INDEX IF NOT EXISTS idx_outbox_tasks_key ON outbox_tasks(key);

-- Partial index for pending tasks (optimizes GetTasksWithError)
CREATE INDEX IF NOT EXISTS idx_outbox_tasks_pending ON outbox_tasks(status, updated_at) 
    WHERE status = 'pending';

-- Unique index to ensure only one pending task per key
CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_tasks_unique_pending_key ON outbox_tasks(key) 
    WHERE status = 'pending';
