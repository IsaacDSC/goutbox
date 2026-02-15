package stores

import (
	"context"
	"log"
	"time"

	"github.com/IsaacDSC/goutbox"
)

type MemStore struct {
	db map[time.Time]goutbox.Task
}

func NewMemStore() *MemStore {
	return &MemStore{
		db: make(map[time.Time]goutbox.Task),
	}
}

func (ms *MemStore) Create(ctx context.Context, task goutbox.Task) error {
	ms.db[time.Now()] = task
	return nil
}

// Get tasks with error and updatedAt more than 1 minute ago
func (ms *MemStore) GetTasksWithError(ctx context.Context) ([]goutbox.Task, error) {
	var tasks []goutbox.Task

	for t, task := range ms.db {
		if time.Since(t) > time.Millisecond*500 {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

func (ms *MemStore) UpdateTaskError(ctx context.Context, task goutbox.Task) error {
	for t, tk := range ms.db {
		// remove task with same key
		if tk.Key == task.Key {
			delete(ms.db, t)
			ms.db[time.Now()] = task
			break
		}
	}

	return nil
}

func (ms *MemStore) DiscardTask(ctx context.Context, key string, isOk bool) error {
	for t, tk := range ms.db {
		if tk.Key == key {
			delete(ms.db, t)
			break
		}
	}

	return nil
}

func (ms *MemStore) Close() error {
	// backup in database or file OR schedule process all tasks in next cycle
	for ts, task := range ms.db {
		// if task is less than 500ms old, add to process in next cycle
		log.Printf("task %s with error, created at %s, pending to process in next cycle", task.Key, ts)
	}

	log.Printf("save in persistent store, pending tasks: %d", len(ms.db))

	return nil
}
