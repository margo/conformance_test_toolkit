package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kr/pretty"
	"github.com/margo/dev-repo/poc/device/agent/database"
	workloads "github.com/margo/dev-repo/shared-lib/workloads"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"github.com/margo/dev-repo/standard/pkg"
	"go.uber.org/zap"
)

// WorkloadDeployer interface for different deployment types
type WorkloadDeployer interface {
	Deploy(ctx context.Context, deployment sbi.AppDeployment) error
	Update(ctx context.Context, deployment sbi.AppDeployment) error
	Remove(ctx context.Context, appID string) error
	GetType() string
}

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
	log       *zap.SugaredLogger
	database  database.AgentDatabase
	deployers map[string]WorkloadDeployer

	// Lifecycle management
	started  bool
	stopChan chan struct{}
}

// HelmDeployer implements WorkloadDeployer for Helm deployments
type HelmDeployer struct {
	client *workloads.HelmClient
	log    *zap.SugaredLogger
}

func NewHelmDeployer(client *workloads.HelmClient, log *zap.SugaredLogger) *HelmDeployer {
	return &HelmDeployer{
		client: client,
		log:    log,
	}
}

func (h *HelmDeployer) GetType() string {
	return "helm.v3"
}

func (h *HelmDeployer) Deploy(ctx context.Context, deployment sbi.AppDeployment) error {
	h.log.Infow("Deploying Helm workload", "appId", deployment.Metadata.Id)

	// Convert parameters to values
	componentValues, err := pkg.ConvertAllAppDeploymentParamsToValues(*deployment.Spec.Parameters)
	if err != nil {
		return fmt.Errorf("failed to parse parameters for value override: %w", err)
	}

	// Validate components
	if len(deployment.Spec.DeploymentProfile.Components) == 0 {
		return fmt.Errorf("no components specified in deployment profile")
	}

	// Get first component (assuming single chart per profile)
	component := deployment.Spec.DeploymentProfile.Components[0]
	componentAsHelm, err := component.AsHelmApplicationDeploymentProfileComponent()
	if err != nil {
		return fmt.Errorf("invalid helm deployment profile: %w", err)
	}

	// Validate required fields
	if componentAsHelm.Properties.Repository == "" {
		return fmt.Errorf("repository is required for helm deployment")
	}
	if componentAsHelm.Properties.Revision == nil {
		return fmt.Errorf("revision is required for helm deployment")
	}

	// Add repository
	h.log.Info("helm component", pretty.Sprint(componentAsHelm))
	repository := componentAsHelm.Properties.Repository
	// if err := h.client.AddRepository(componentAsHelm.Name, repository, workloads.HelmRepoAuth{}); err != nil {
	// 	return fmt.Errorf("failed to add repository: %w", err)
	// }

	// Install chart
	namespace := "" // Default namespace
	releaseName := componentAsHelm.Name
	revision := *componentAsHelm.Properties.Revision
	wait := componentAsHelm.Properties.Wait != nil && *componentAsHelm.Properties.Wait
	overrides := componentValues[componentAsHelm.Name]

	if err := h.client.InstallChart(ctx, releaseName, repository, namespace, revision, wait, overrides); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	h.log.Infow("Successfully deployed Helm workload", "appId", deployment.Metadata.Id, "releaseName", releaseName)
	return nil
}

func (h *HelmDeployer) Update(ctx context.Context, deployment sbi.AppDeployment) error {
	h.log.Infow("Updating Helm workload", "appId", deployment.Metadata.Id)

	// Convert parameters to values
	componentValues, err := pkg.ConvertAllAppDeploymentParamsToValues(*deployment.Spec.Parameters)
	if err != nil {
		return fmt.Errorf("failed to parse parameters for value override: %w", err)
	}

	// Get component
	component := deployment.Spec.DeploymentProfile.Components[0]
	componentAsHelm, err := component.AsHelmApplicationDeploymentProfileComponent()
	if err != nil {
		return fmt.Errorf("invalid helm deployment profile: %w", err)
	}

	// Update chart
	releaseName := componentAsHelm.Name
	repository := componentAsHelm.Properties.Repository
	namespace := ""
	// revision := *componentAsHelm.Properties.Revision
	// wait := componentAsHelm.Properties.Wait != nil && *componentAsHelm.Properties.Wait
	overrides := componentValues[componentAsHelm.Name]

	if err := h.client.UpdateChart(ctx, releaseName, repository, namespace, overrides); err != nil {
		return fmt.Errorf("failed to upgrade helm chart: %w", err)
	}

	h.log.Infow("Successfully updated Helm workload", "appId", deployment.Metadata.Id, "releaseName", releaseName)
	return nil
}

func (h *HelmDeployer) Remove(ctx context.Context, appID string) error {
	h.log.Infow("Removing Helm workload", "appId", appID)

	// Use appID as release name (this might need adjustment based on your naming strategy)
	releaseName := appID
	namespace := ""

	if err := h.client.UninstallChart(ctx, releaseName, namespace); err != nil {
		return fmt.Errorf("failed to uninstall helm chart: %w", err)
	}

	h.log.Infow("Successfully removed Helm workload", "appId", appID, "releaseName", releaseName)
	return nil
}

// DockerComposeDeployer implements WorkloadDeployer for Docker Compose deployments
type DockerComposeDeployer struct {
	client *workloads.DockerComposeClient
	log    *zap.SugaredLogger
}

func NewDockerComposeDeployer(client *workloads.DockerComposeClient, log *zap.SugaredLogger) *DockerComposeDeployer {
	return &DockerComposeDeployer{
		client: client,
		log:    log,
	}
}

func (d *DockerComposeDeployer) GetType() string {
	return "compose"
}

func (d *DockerComposeDeployer) Deploy(ctx context.Context, deployment sbi.AppDeployment) error {
	d.log.Infow("Deploying Docker Compose workload", "appId", deployment.Metadata.Id)

	// TODO: Implement Docker Compose deployment logic
	return fmt.Errorf("docker compose deployment not yet implemented")
}

func (d *DockerComposeDeployer) Update(ctx context.Context, deployment sbi.AppDeployment) error {
	d.log.Infow("Updating Docker Compose workload", "appId", deployment.Metadata.Id)

	// TODO: Implement Docker Compose update logic
	return fmt.Errorf("docker compose update not yet implemented")
}

func (d *DockerComposeDeployer) Remove(ctx context.Context, appID string) error {
	d.log.Infow("Removing Docker Compose workload", "appId", appID)

	// TODO: Implement Docker Compose removal logic
	return fmt.Errorf("docker compose removal not yet implemented")
}

// NewWorkloadManager creates a new WorkloadManager instance
func NewWorkloadManager(log *zap.SugaredLogger, database database.AgentDatabase, helmClient *workloads.HelmClient, dockerComposeClient *workloads.DockerComposeClient) WorkloadManager {
	deployers := make(map[string]WorkloadDeployer)

	// Add available deployers
	if helmClient != nil {
		deployers["helm.v3"] = NewHelmDeployer(helmClient, log)
	}
	if dockerComposeClient != nil {
		deployers["compose"] = NewDockerComposeDeployer(dockerComposeClient, log)
	}

	return &workloadManager{
		log:       log,
		database:  database,
		deployers: deployers,
		stopChan:  make(chan struct{}),
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
	deployer, exists := wm.deployers[string(deploymentType)]
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
	deployer, exists := wm.deployers[string(deploymentType)]
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
	deployer, exists := wm.deployers[string(deploymentType)]
	if !exists {
		return fmt.Errorf("unsupported deployment profile type: %s", deploymentType)
	}

	return deployer.Remove(ctx, appID)
}

// getAvailableDeployerTypes returns list of available deployer types
func (wm *workloadManager) getAvailableDeployerTypes() []string {
	types := make([]string, 0, len(wm.deployers))
	for deployerType := range wm.deployers {
		types = append(types, deployerType)
	}
	return types
}
