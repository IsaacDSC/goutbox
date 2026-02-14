package goutbox

import "time"

// Default configuration values
const (
	DefaultLoopInterval = 1 * time.Minute
	DefaultMaxAttempts  = 3
)

// Config holds the task-specific configuration
type Config struct {
	MaxAttempts int
}

// serviceConfig holds global service configuration
type serviceConfig struct {
	loopInterval time.Duration
}

// doConfig holds per-call configuration for Do()
type doConfig struct {
	maxAttempts int
}

// ServiceOption is a functional option for configuring the Service
type ServiceOption func(*serviceConfig)

// DoOption is a functional option for configuring Do() calls
type DoOption func(*doConfig)

// WithLoopInterval sets the interval for the retry loop in Start().
// If d <= 0, the default interval (1 minute) is used.
func WithLoopInterval(d time.Duration) ServiceOption {
	return func(c *serviceConfig) {
		if d > 0 {
			c.loopInterval = d
		}
	}
}

// WithMaxAttempts sets the maximum number of retry attempts for a task.
// If n <= 0, the default (3) is used.
func WithMaxAttempts(n int) DoOption {
	return func(c *doConfig) {
		if n > 0 {
			c.maxAttempts = n
		}
	}
}
