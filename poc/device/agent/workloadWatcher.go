package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/margo/dev-repo/poc/device/agent/database"
	workloads "github.com/margo/dev-repo/shared-lib/workloads"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"github.com/margo/dev-repo/standard/pkg"
	"go.uber.org/zap"
)

// WorkloadMonitor interface for different monitoring implementations
type WorkloadMonitor interface {
	Watch(ctx context.Context, appID string) error
	StopWatching(ctx context.Context, appID string) error
	GetStatus(ctx context.Context, appID string) (WorkloadStatus, error)
	GetType() string
}

// WorkloadStatus represents the current status of a workload
type WorkloadStatus struct {
	WorkloadId string    `json:"workloadId"`
	Status     string    `json:"status"` // running, stopped, failed, unknown
	Health     string    `json:"health"` // healthy, unhealthy, unknown
	Message    string    `json:"message,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

type WorkloadWatcher interface {
	// Lifecycle management
	Start() error
	Stop() error

	// Workload monitoring operations
	StartWatching(ctx context.Context, app sbi.AppState) error
	StopWatching(ctx context.Context, appID string) error
	GetWorkloadStatus(ctx context.Context, appID string) (WorkloadStatus, error)

	// Database subscriber interface for reactive monitoring
	database.WorkloadDatabaseSubscriber
}

type workloadWatcher struct {
	log      *zap.SugaredLogger
	database database.AgentDatabase
	monitors map[string]WorkloadMonitor

	// Active watches tracking
	activeWatches map[string]context.CancelFunc
	watchesMutex  sync.RWMutex

	// Lifecycle management
	started  bool
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// HelmMonitor implements WorkloadMonitor for Helm deployments
type HelmMonitor struct {
	client   *workloads.HelmClient
	database database.AgentDatabase // Add this field
	log      *zap.SugaredLogger
}

func NewHelmMonitor(client *workloads.HelmClient, database database.AgentDatabase, log *zap.SugaredLogger) *HelmMonitor {
	return &HelmMonitor{
		client:   client,
		database: database, // Add this field
		log:      log,
	}
}

func (h *HelmMonitor) GetType() string {
	return "helm.v3"
}

func (h *HelmMonitor) Watch(ctx context.Context, appID string) error {
	h.log.Infow("Starting to watch Helm workload", "appId", appID)

	// Start monitoring loop
	go h.monitorLoop(ctx, appID)

	return nil
}

func (h *HelmMonitor) StopWatching(ctx context.Context, appID string) error {
	h.log.Infow("Stopping watch for Helm workload", "appId", appID)
	// Context cancellation will stop the monitoring loop
	return nil
}

// In HelmMonitor.GetStatus() - FIXED
func (h *HelmMonitor) GetStatus(ctx context.Context, appID string) (WorkloadStatus, error) {
	h.log.Debugw("Getting Helm workload status", "appId", appID)

	select {
	case <-ctx.Done():
		return WorkloadStatus{}, ctx.Err()
	default:
	}

	// FIX: Get the actual release name used during deployment
	app, err := h.database.GetWorkload(appID) // You'll need to pass database to HelmMonitor
	if err != nil {
		return WorkloadStatus{
			WorkloadId: appID,
			Status:     "unknown",
			Health:     "unknown",
			Message:    fmt.Sprintf("Failed to get app from database: %v", err),
			Timestamp:  time.Now(),
		}, nil
	}

	appDeployment, err := pkg.ConvertAppStateToAppDeployment(app)
	if err != nil {
		return WorkloadStatus{
			WorkloadId: appID,
			Status:     "unknown",
			Health:     "unknown",
			Message:    fmt.Sprintf("Failed to convert app: %v", err),
			Timestamp:  time.Now(),
		}, nil
	}

	component := appDeployment.Spec.DeploymentProfile.Components[0]
	componentAsHelm, err := component.AsHelmApplicationDeploymentProfileComponent()
	if err != nil {
		return WorkloadStatus{
			WorkloadId: appID,
			Status:     "unknown",
			Health:     "unknown",
			Message:    fmt.Sprintf("Invalid helm component: %v", err),
			Timestamp:  time.Now(),
		}, nil
	}

	releaseName := generateReleaseName(appID, componentAsHelm.Name)
	namespace := ""

	// Get release status from Helm
	status, err := h.client.GetReleaseStatus(ctx, releaseName, namespace)
	if err != nil {
		return WorkloadStatus{
			WorkloadId: appID,
			Status:     "unknown",
			Health:     "unknown",
			Message:    fmt.Sprintf("Failed to get status: %v", err),
			Timestamp:  time.Now(),
		}, nil
	}

	return WorkloadStatus{
		WorkloadId: appID,
		Status:     status.Status,
		Health:     h.determineHealth(status),
		Message:    status.Description,
		Timestamp:  time.Now(),
	}, nil
}

func (h *HelmMonitor) monitorLoop(ctx context.Context, appID string) {
	ticker := time.NewTicker(30 * time.Second) // Monitor every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.log.Debugw("Stopping monitor loop for Helm workload", "appId", appID)
			return
		case <-ticker.C:
			status, err := h.GetStatus(ctx, appID)
			if err != nil {
				h.log.Errorw("Failed to get workload status", "appId", appID, "error", err)
				continue
			}

			h.log.Debugw("Workload status check",
				"appId", appID,
				"status", status.Status,
				"health", status.Health)

			// TODO: Report status changes to database or external systems
		}
	}
}

func (h *HelmMonitor) determineHealth(status *workloads.ReleaseStatus) string {
	// Implement health determination logic based on Helm status
	switch status.Status {
	case "deployed":
		return "healthy"
	case "failed", "uninstalling", "pending-install", "pending-upgrade", "pending-rollback":
		return "unhealthy"
	default:
		return "unknown"
	}
}

// DockerComposeMonitor implements WorkloadMonitor for Docker Compose deployments
type DockerComposeMonitor struct {
	client *workloads.DockerComposeClient
	log    *zap.SugaredLogger
}

func NewDockerComposeMonitor(client *workloads.DockerComposeClient, log *zap.SugaredLogger) *DockerComposeMonitor {
	return &DockerComposeMonitor{
		client: client,
		log:    log,
	}
}

func (d *DockerComposeMonitor) GetType() string {
	return "compose"
}

func (d *DockerComposeMonitor) Watch(ctx context.Context, appID string) error {
	d.log.Infow("Starting to watch Docker Compose workload", "appId", appID)

	// TODO: Implement Docker Compose monitoring
	return fmt.Errorf("docker compose monitoring not yet implemented")
}

func (d *DockerComposeMonitor) StopWatching(ctx context.Context, appID string) error {
	d.log.Infow("Stopping watch for Docker Compose workload", "appId", appID)
	// TODO: Implement Docker Compose stop watching
	return fmt.Errorf("docker compose stop watching not yet implemented")
}

func (d *DockerComposeMonitor) GetStatus(ctx context.Context, appID string) (WorkloadStatus, error) {
	d.log.Debugw("Getting Docker Compose workload status", "appId", appID)
	// TODO: Implement Docker Compose status checking
	return WorkloadStatus{
		WorkloadId: appID,
		Status:     "unknown",
		Health:     "unknown",
		Message:    "Docker Compose monitoring not implemented",
		Timestamp:  time.Now(),
	}, nil
}

// NewWorkloadWatcher creates a new WorkloadWatcher instance
func NewWorkloadWatcher(log *zap.SugaredLogger, database database.AgentDatabase, helmClient *workloads.HelmClient, dockerComposeClient *workloads.DockerComposeClient) WorkloadWatcher {
	monitors := make(map[string]WorkloadMonitor)

	// Add available monitors
	if helmClient != nil {
		monitors["helm.v3"] = NewHelmMonitor(helmClient, database, log)
	}
	if dockerComposeClient != nil {
		monitors["compose"] = NewDockerComposeMonitor(dockerComposeClient, log)
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
func (ww *workloadWatcher) OnDatabaseEvent(event database.WorkloadDatabaseEvent) error {
	ww.log.Debugw("WorkloadWatcher-Received database event",
		"type", event.Type,
		"appId", event.AppID,
		"timestamp", event.Timestamp)

	// Create context with timeout for database event handling
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	switch event.Type {
	case database.EventAppAdded:
		if event.NewState != nil {
			ww.log.Infow("Starting monitoring for new app", "appId", event.AppID)
			return ww.StartWatching(ctx, *event.NewState)
		}
	case database.EventAppUpdated:
		// For updates, we might need to restart monitoring with new configuration
		if event.NewState != nil {
			ww.log.Infow("Restarting monitoring for updated app", "appId", event.AppID)
			// Stop existing watch
			_ = ww.StopWatching(ctx, event.AppID)
			// Start new watch
			return ww.StartWatching(ctx, *event.NewState)
		}
	case database.EventAppDeleted:
		ww.log.Infow("Stopping monitoring for deleted app", "appId", event.AppID)
		return ww.StopWatching(ctx, event.AppID)
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

// GetWorkloadStatus retrieves the current status of a workload
func (ww *workloadWatcher) GetWorkloadStatus(ctx context.Context, workloadId string) (WorkloadStatus, error) {
	if workloadId == "" {
		return WorkloadStatus{}, fmt.Errorf("app ID is required")
	}

	ww.log.Debugw("Getting workload status", "appId", workloadId)

	// Get appState from database to determine deployment type
	appState, err := ww.database.GetWorkload(workloadId)
	if err != nil {
		return WorkloadStatus{}, fmt.Errorf("failed to get app from database: %w", err)
	}

	// TODO: Extract deployment profile type from app
	appDeployment, _ := pkg.ConvertAppStateToAppDeployment(appState) // "helm.v3" // Default for now

	monitor, exists := ww.monitors[string(appDeployment.Spec.DeploymentProfile.Type)]
	if !exists {
		return WorkloadStatus{}, fmt.Errorf("unsupported deployment profile type: %s", appDeployment.Spec.DeploymentProfile.Type)
	}

	return monitor.GetStatus(ctx, workloadId)
}

// getAvailableMonitorTypes returns list of available monitor types
func (ww *workloadWatcher) getAvailableMonitorTypes() []string {
	types := make([]string, 0, len(ww.monitors))
	for monitorType := range ww.monitors {
		types = append(types, monitorType)
	}
	return types
}
