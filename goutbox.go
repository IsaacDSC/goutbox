package goutbox

import (
	"context"
	"fmt"
	"log"
	"time"
)

type Store interface {
	Create(ctx context.Context, task Task) error
	GetTasksWithError(ctx context.Context) ([]Task, error) //Get tasks with error and updatedAt more than 1 minute ago
	UpdateTaskError(ctx context.Context, task Task) error
	DiscardTask(ctx context.Context, key string, isOk bool) error
}

type Service struct {
	store        Store
	loopInterval time.Duration
}

func New(s Store, opts ...ServiceOption) *Service {
	cfg := &serviceConfig{
		loopInterval: DefaultLoopInterval,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Service{
		store:        s,
		loopInterval: cfg.loopInterval,
	}
}

type Publisher func(ctx context.Context, eventName string, payload any) error

func (s *Service) Do(ctx context.Context, eventName string, payload any, cb Publisher, opts ...DoOption) error {
	cfg := &doConfig{
		maxAttempts: DefaultMaxAttempts,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cb(ctx, eventName, payload); err == nil {
		return nil
	}

	if err := s.store.Create(ctx, Task{
		Key:     eventName,
		Payload: payload,
		Config: Config{
			MaxAttempts: cfg.maxAttempts,
		},
	}); err != nil {
		return fmt.Errorf("failed to store task error: %w", err)
	}

	return nil
}

func (s *Service) Start(ctx context.Context, fn Publisher) {
	trigger := time.NewTicker(s.loopInterval)
	defer trigger.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-trigger.C:
			if err := s.startTasks(ctx, fn); err != nil {
				log.Printf("failed to start tasks: %v", err)
			}
		}
	}

}

func (s *Service) startTasks(ctx context.Context, pub Publisher) error {
	tasks, err := s.store.GetTasksWithError(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tasks with error: %w", err)
	}

	for _, task := range tasks {
		if task.Attempts >= task.Config.MaxAttempts {
			if err := s.store.DiscardTask(ctx, task.Key, false); err != nil {
				log.Printf("failed to discard task: %v", err)
			}
			continue
		}

		if err := pub(ctx, task.Key, task.Payload); err != nil {
			task.Error = append(task.Error, err)
			task.Attempts++
			if err := s.store.UpdateTaskError(ctx, task); err != nil {
				log.Printf("failed to update task error: %v", err)
			}
			continue
		}

		if err := s.store.DiscardTask(ctx, task.Key, true); err != nil {
			log.Printf("failed to discard success task: %v", err)
		}
	}

	return nil

}
