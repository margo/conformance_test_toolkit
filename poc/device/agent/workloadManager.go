package main

import (
	"context"
	"fmt"
	"time"

	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/poc/device/agent/deployers"
	workloads "github.com/margo/dev-repo/shared-lib/workloads"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"github.com/margo/dev-repo/standard/pkg"
	"go.uber.org/zap"
)

// WorkloadManager interface defines the contract for managing application workloads
type WorkloadManager interface {
	// Start initializes the workload manager and begins listening for database events
	Start() error

	// Stop gracefully shuts down the workload manager
	Stop() error

	// ExplicitlyTriggerDeploy manually triggers deployment of an application
	ExplicitlyTriggerDeploy(ctx context.Context, app sbi.AppState) error

	// ExplicitlyTriggerUpdate manually triggers update of an application
	ExplicitlyTriggerUpdate(ctx context.Context, app sbi.AppState) error

	// ExplicitlyTriggerRemove manually triggers removal of an application
	ExplicitlyTriggerRemove(ctx context.Context, appID string) error

	// DatabaseSubscriber interface methods for event-driven operations
	database.WorkloadDatabaseSubscriber
}

// workloadManager implements the WorkloadManager interface with event-driven architecture
type workloadManager struct {
	log          *zap.SugaredLogger
	database     database.AgentDatabase
	deployersMap map[string]deployers.WorkloadDeployer

	// Lifecycle management
	started  bool
	stopChan chan struct{}
}

// NewWorkloadManager creates a new WorkloadManager instance
func NewWorkloadManager(log *zap.SugaredLogger, database database.AgentDatabase, helmClient *workloads.HelmClient, dockerComposeClient *workloads.DockerComposeClient) WorkloadManager {
	deployersMap := make(map[string]deployers.WorkloadDeployer)

	// Add available deployers
	if helmClient != nil {
		deployersMap["helm.v3"] = deployers.NewHelmDeployer(helmClient, database, log)
	}
	if dockerComposeClient != nil {
		deployersMap["compose"] = deployers.NewDockerComposeDeployer(dockerComposeClient, log)
	}

	return &workloadManager{
		log:          log,
		database:     database,
		deployersMap: deployersMap,
		stopChan:     make(chan struct{}),
	}
}

func (wm *workloadManager) Start() error {
	wm.log.Info("Starting WorkloadManager")

	// Subscribe to database events for reactive workload management
	if err := wm.database.Subscribe(wm); err != nil {
		return fmt.Errorf("failed to subscribe to database events: %w", err)
	}

	wm.started = true
	wm.log.Infow("WorkloadManager started successfully", "availableDeployers", wm.getAvailableDeployerTypes())
	return nil
}

// Stop gracefully shuts down the workload manager
func (wm *workloadManager) Stop() error {
	wm.log.Info("Stopping WorkloadManager")

	if !wm.started {
		wm.log.Warn("WorkloadManager not started")
		return nil
	}

	// Unsubscribe from database events
	if err := wm.database.Unsubscribe(wm.GetSubscriberID()); err != nil {
		wm.log.Warnw("Failed to unsubscribe from database events", "error", err)
	}

	// Signal stop
	close(wm.stopChan)
	wm.started = false

	wm.log.Info("WorkloadManager stopped successfully")
	return nil
}

// GetSubscriberID returns a unique identifier for this database subscriber
func (wm *workloadManager) GetSubscriberID() string {
	return "workload-manager"
}

// OnDatabaseEvent handles database events and triggers appropriate workload operations
func (wm *workloadManager) OnDatabaseEvent(event database.WorkloadDatabaseEvent) error {
	wm.log.Debugw("Received database event",
		"type", event.Type,
		"appId", event.AppID,
		"timestamp", event.Timestamp)

	// Create context with timeout for database event handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	switch event.Type {
	case database.EventAppAdded:
		if event.NewState != nil {
			wm.log.Infow("Handling new app deployment", "appId", event.AppID)
			return wm.deploy(ctx, *event.NewState)
		}
	case database.EventAppUpdated:
		if event.NewState != nil {
			wm.log.Infow("Handling app update", "appId", event.AppID)
			return wm.update(ctx, *event.NewState)
		}
	case database.EventAppDeleted:
		wm.log.Infow("Handling app removal", "appId", event.AppID)
		return wm.remove(ctx, event.AppID)
	default:
		wm.log.Debugw("Ignoring unhandled event type", "type", event.Type)
	}

	return nil
}

// ExplicitlyTriggerDeploy manually triggers deployment of an application
func (wm *workloadManager) ExplicitlyTriggerDeploy(ctx context.Context, app sbi.AppState) error {
	if app.AppId == "" {
		return fmt.Errorf("app ID is required")
	}

	wm.log.Infow("Explicitly triggering deployment", "appId", app.AppId)
	return wm.deploy(ctx, app)
}

// ExplicitlyTriggerUpdate manually triggers update of an application
func (wm *workloadManager) ExplicitlyTriggerUpdate(ctx context.Context, app sbi.AppState) error {
	if app.AppId == "" {
		return fmt.Errorf("app ID is required")
	}

	wm.log.Infow("Explicitly triggering update", "appId", app.AppId)
	return wm.update(ctx, app)
}

// ExplicitlyTriggerRemove manually triggers removal of an application
func (wm *workloadManager) ExplicitlyTriggerRemove(ctx context.Context, appID string) error {
	if appID == "" {
		return fmt.Errorf("app ID is required")
	}

	wm.log.Infow("Explicitly triggering removal", "appId", appID)
	return wm.remove(ctx, appID)
}

// deploy performs the actual deployment of an application workload
func (wm *workloadManager) deploy(ctx context.Context, app sbi.AppState) error {
	wm.log.Infow("Deploying workload", "appId", app.AppId)

	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Convert AppState to AppDeployment
	appDeployment, err := pkg.ConvertAppStateToAppDeployment(app)
	if err != nil {
		return fmt.Errorf("failed to convert AppState to AppDeployment: %w", err)
	}

	// TODO: Validate deployment profile

	deploymentType := appDeployment.Spec.DeploymentProfile.Type
	deployer, exists := wm.deployersMap[string(deploymentType)]
	if !exists {
		return fmt.Errorf("unsupported deployment profile type: %s (available: %v)",
			deploymentType, wm.getAvailableDeployerTypes())
	}

	return deployer.Deploy(ctx, appDeployment)
}

// update performs the actual update of an application workload
func (wm *workloadManager) update(ctx context.Context, app sbi.AppState) error {
	wm.log.Infow("Updating workload", "appId", app.AppId)

	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Convert AppState to AppDeployment
	appDeployment, err := pkg.ConvertAppStateToAppDeployment(app)
	if err != nil {
		return fmt.Errorf("failed to convert AppState to AppDeployment: %w", err)
	}

	deploymentType := appDeployment.Spec.DeploymentProfile.Type
	deployer, exists := wm.deployersMap[string(deploymentType)]
	if !exists {
		return fmt.Errorf("unsupported deployment profile type: %s", deploymentType)
	}

	return deployer.Update(ctx, appDeployment)
}

// remove performs the actual removal of an application workload
func (wm *workloadManager) remove(ctx context.Context, appID string) error {
	wm.log.Infow("Removing workload", "appId", appID)

	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Get app from database to determine deployment type
	app, err := wm.database.GetWorkload(appID)
	if err != nil {
		return fmt.Errorf("failed to get app from database: %w", err)
	}

	// Convert to deployment to get type
	appDeployment, err := pkg.ConvertAppStateToAppDeployment(app)
	if err != nil {
		return fmt.Errorf("failed to convert AppState to AppDeployment: %w", err)
	}

	deploymentType := appDeployment.Spec.DeploymentProfile.Type
	deployer, exists := wm.deployersMap[string(deploymentType)]
	if !exists {
		return fmt.Errorf("unsupported deployment profile type: %s", deploymentType)
	}

	return deployer.Remove(ctx, appID)
}

// getAvailableDeployerTypes returns list of available deployer types
func (wm *workloadManager) getAvailableDeployerTypes() []string {
	types := make([]string, 0, len(wm.deployersMap))
	for deployerType := range wm.deployersMap {
		types = append(types, deployerType)
	}
	return types
}
