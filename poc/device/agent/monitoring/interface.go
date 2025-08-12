package monitoring

import (
	"context"
	"time"
)

// WorkloadStatus represents the current status of a workload
type WorkloadStatus struct {
	WorkloadId string    `json:"workloadId"`
	Status     string    `json:"status"` // running, stopped, failed, unknown
	Health     string    `json:"health"` // healthy, unhealthy, unknown
	Message    string    `json:"message,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// WorkloadMonitor interface for different monitoring implementations
type WorkloadMonitor interface {
	Watch(ctx context.Context, appID string) error
	StopWatching(ctx context.Context, appID string) error
	GetStatus(ctx context.Context, appID string) (WorkloadStatus, error)
	GetType() string
}
