package goutbox

import (
	"context"
	"time"
)

// StartTasksForTest exports the private startTasks method for testing purposes.
func (s *Service) StartTasksForTest(ctx context.Context, pub Publisher) error {
	return s.startTasks(ctx, pub)
}

// GetLoopInterval returns the configured loop interval for testing purposes.
func (s *Service) GetLoopInterval() time.Duration {
	return s.loopInterval
}
