package workload

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/poc/device/agent/workload/monitoring"
	workloads "github.com/margo/dev-repo/shared-lib/workloads"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"github.com/margo/dev-repo/standard/pkg"
	"go.uber.org/zap"
)

type WorkloadWatcher interface {
	// Lifecycle management
	Start() error
	Stop() error

	// Workload monitoring operations
	StartWatching(ctx context.Context, app sbi.AppState) error
	StopWatching(ctx context.Context, appID string) error
	GetDeploymentStatus(ctx context.Context, appID string) (sbi.ComponentStatus, error)

	// Database subscriber interface for reactive monitoring
	database.DeploymentDatabaseSubscriber
}

type workloadWatcher struct {
	log      *zap.SugaredLogger
	database database.AgentDatabase
	monitors map[string]monitoring.WorkloadMonitor

	// Active watches tracking
	activeWatches map[string]context.CancelFunc
	watchesMutex  sync.RWMutex

	// Lifecycle management
	started  bool
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewWorkloadWatcher creates a new WorkloadWatcher instance
func NewWorkloadWatcher(log *zap.SugaredLogger, database database.AgentDatabase, helmClient *workloads.HelmClient, dockerComposeClient *workloads.DockerComposeClient) WorkloadWatcher {
	monitors := make(map[string]monitoring.WorkloadMonitor)

	// Add available monitors
	if helmClient != nil {
		monitors["helm.v3"] = monitoring.NewHelmMonitor(helmClient, database, log)
	}
	if dockerComposeClient != nil {
		monitors["compose"] = monitoring.NewDockerComposeMonitor(dockerComposeClient, log)
	}

	return &workloadWatcher{
		log:           log,
		database:      database,
		monitors:      monitors,
		activeWatches: make(map[string]context.CancelFunc),
		stopChan:      make(chan struct{}),
	}
}

func (ww *workloadWatcher) Start() error {
	ww.log.Info("Starting WorkloadWatcher")

	// Subscribe to database events for reactive monitoring
	if err := ww.database.Subscribe(ww); err != nil {
		return fmt.Errorf("failed to subscribe to database events: %w", err)
	}

	ww.started = true
	ww.log.Infow("WorkloadWatcher started successfully", "availableMonitors", ww.getAvailableMonitorTypes())
	return nil
}

func (ww *workloadWatcher) Stop() error {
	ww.log.Info("Stopping WorkloadWatcher")

	if !ww.started {
		ww.log.Warn("WorkloadWatcher not started")
		return nil
	}

	// Stop all active watches
	ww.watchesMutex.Lock()
	for appID, cancel := range ww.activeWatches {
		ww.log.Debugw("Stopping watch", "appId", appID)
		cancel()
	}
	ww.activeWatches = make(map[string]context.CancelFunc)
	ww.watchesMutex.Unlock()

	// Unsubscribe from database events
	if err := ww.database.Unsubscribe(ww.GetSubscriberID()); err != nil {
		ww.log.Warnw("Failed to unsubscribe from database events", "error", err)
	}

	// Signal stop and wait for goroutines
	close(ww.stopChan)
	ww.wg.Wait()

	ww.started = false
	ww.log.Info("WorkloadWatcher stopped successfully")
	return nil
}

// GetSubscriberID returns a unique identifier for this database subscriber
func (ww *workloadWatcher) GetSubscriberID() string {
	return "workload-watcher"
}

// OnDatabaseEvent handles database events and starts/stops monitoring accordingly
func (ww *workloadWatcher) OnDatabaseEvent(event database.DeploymentDatabaseEvent) error {
	ww.log.Debugw("WorkloadWatcher-Received database event",
		"type", event.Type,
		"appId", event.Deployment.DesiredState.AppId,
		"timestamp", event.Timestamp)

	// Create context with timeout for database event handling
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	switch event.Type {
	case database.EventDeploymentAdded:
		if event.Deployment.DesiredState != nil {
			ww.log.Infow("Starting monitoring for new app", "appId", event.Deployment.DesiredState.AppId)
			return ww.StartWatching(ctx, *event.Deployment.DesiredState)
		}
	// case database.EventAppDesiredStateChanged:
	// 	// For updates, we might need to restart monitoring with new configuration
	// 	if event.NewState != nil {
	// 		ww.log.Infow("Restarting monitoring for updated app", "appId", event.AppID)
	// 		// Stop existing watch
	// 		_ = ww.StopWatching(ctx, event.AppID)
	// 		// Start new watch
	// 		return ww.StartWatching(ctx, *event.NewState)
	// 	}
	case database.EventDeploymentDeleted:
		ww.log.Infow("Stopping monitoring for deleted app", "appId", event.Deployment.DesiredState.AppId)
		return ww.StopWatching(ctx, event.Deployment.DesiredState.AppId)
	default:
		ww.log.Debugw("Ignoring unhandled event type", "type", event.Type)
	}

	return nil
}

// StartWatching begins monitoring a specific workload
func (ww *workloadWatcher) StartWatching(ctx context.Context, app sbi.AppState) error {
	if app.AppId == "" {
		return fmt.Errorf("app ID is required")
	}

	ww.log.Infow("Starting to watch workload", "appId", app.AppId)

	// Determine monitor type from app deployment profile
	// TODO: Extract deployment profile type from app.AppDeployment
	deployment, err := pkg.ConvertAppStateToAppDeployment(app) // "helm.v3" // Default for now, should be extracted from app
	if err != nil {
		return err
	}

	deploymentType := deployment.Spec.DeploymentProfile.Type

	monitor, exists := ww.monitors[string(deploymentType)]
	if !exists {
		return fmt.Errorf("unsupported deployment profile type: %s (available: %v)",
			deploymentType, ww.getAvailableMonitorTypes())
	}

	// Create context for this specific watch
	watchCtx, watchCancel := context.WithCancel(context.Background())

	// Store the cancel function
	ww.watchesMutex.Lock()
	// Stop existing watch if any
	if existingCancel, exists := ww.activeWatches[app.AppId]; exists {
		existingCancel()
	}
	ww.activeWatches[app.AppId] = watchCancel
	ww.watchesMutex.Unlock()

	// Start monitoring
	ww.wg.Add(1)
	go func() {
		defer ww.wg.Done()
		if err := monitor.Watch(watchCtx, app.AppId); err != nil {
			ww.log.Errorw("Failed to watch workload", "appId", app.AppId, "error", err)
		}
	}()

	return nil
}

// StopWatching stops monitoring a specific workload
func (ww *workloadWatcher) StopWatching(ctx context.Context, appID string) error {
	if appID == "" {
		return fmt.Errorf("app ID is required")
	}

	ww.log.Infow("Stopping watch for workload", "appId", appID)

	ww.watchesMutex.Lock()
	defer ww.watchesMutex.Unlock()

	if cancel, exists := ww.activeWatches[appID]; exists {
		cancel()
		delete(ww.activeWatches, appID)
		ww.log.Debugw("Stopped watching workload", "appId", appID)
	} else {
		ww.log.Debugw("No active watch found for workload", "appId", appID)
	}

	return nil
}

// GetDeploymentStatus retrieves the current status of a workload
func (ww *workloadWatcher) GetDeploymentStatus(ctx context.Context, deploymentId string) (sbi.ComponentStatus, error) {
	if deploymentId == "" {
		return sbi.ComponentStatus{}, fmt.Errorf("app ID is required")
	}

	ww.log.Debugw("Getting workload status", "appId", deploymentId)

	// Get deployment from database to determine deployment type
	deployment, err := ww.database.GetDeployment(deploymentId)
	if err != nil {
		return sbi.ComponentStatus{}, fmt.Errorf("failed to get app from database: %w", err)
	}

	// TODO: Extract deployment profile type from app
	appDeployment, _ := pkg.ConvertAppStateToAppDeployment(*deployment.CurrentState) // "helm.v3" // Default for now

	monitor, exists := ww.monitors[string(appDeployment.Spec.DeploymentProfile.Type)]
	if !exists {
		return sbi.ComponentStatus{}, fmt.Errorf("unsupported deployment profile type: %s", appDeployment.Spec.DeploymentProfile.Type)
	}

	return monitor.GetStatus(ctx, deploymentId, "")
}

// getAvailableMonitorTypes returns list of available monitor types
func (ww *workloadWatcher) getAvailableMonitorTypes() []string {
	types := make([]string, 0, len(ww.monitors))
	for monitorType := range ww.monitors {
		types = append(types, monitorType)
	}
	return types
}
