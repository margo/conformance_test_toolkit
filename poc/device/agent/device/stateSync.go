package device

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kr/pretty"
	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/poc/device/agent/types"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"github.com/margo/dev-repo/standard/pkg"
	"go.uber.org/zap"
)

const (
	appStateSeekingInterval = time.Second * 10
)

// AppStateSyncer interface - all methods should accept context where needed
type AppStateSyncer interface {
	Start() error
	Stop() error
	ExplicitlyTriggerSync(ctx context.Context) error
	// DatabaseSubscriber interface methods for event-driven operations
	database.DeploymentDatabaseSubscriber
}

// appStateSyncer struct - no context storage
type appStateSyncer struct {
	config           *types.Config
	database         database.AgentDatabase
	log              *zap.SugaredLogger
	apiClientFactory types.APIClientInterface

	// Lifecycle management
	started  bool
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewAppStateSyncer creates a new StateSyncer
func NewAppStateSyncer(log *zap.SugaredLogger, config *types.Config, database database.AgentDatabase, apiClientFactory types.APIClientInterface) AppStateSyncer {
	return &appStateSyncer{
		config:           config,
		database:         database,
		log:              log,
		apiClientFactory: apiClientFactory,
		stopChan:         make(chan struct{}),
	}
}

func (ss *appStateSyncer) Start() error {
	ss.log.Info("Starting AppStateSyncer")

	if ss.started {
		ss.log.Warn("AppStateSyncer already started")
		return nil
	}

	// Subscribe to database events for reactive deployment management
	if err := ss.database.Subscribe(ss); err != nil {
		return fmt.Errorf("failed to subscribe to database events: %w", err)
	}

	ss.started = true

	// Start sync loop in goroutine with proper lifecycle management
	ss.wg.Add(1)
	go func() {
		defer ss.wg.Done()
		ss.syncAppStatesLoop()
	}()

	ss.log.Info("AppStateSyncer started successfully")
	return nil
}

func (ss *appStateSyncer) Stop() error {
	ss.log.Info("Stopping AppStateSyncer")

	if !ss.started {
		ss.log.Warn("AppStateSyncer not started")
		return nil
	}

	// Unsubscribe from database events
	if err := ss.database.Unsubscribe(ss.GetSubscriberID()); err != nil {
		ss.log.Warnw("Failed to unsubscribe from database events", "error", err)
	}

	// Signal stop
	close(ss.stopChan)

	// Wait for goroutines to finish
	ss.wg.Wait()

	ss.started = false
	ss.log.Info("AppStateSyncer stopped successfully")
	return nil
}

// OnDatabaseEvent handles database events and triggers appropriate deployment operations
func (ss *appStateSyncer) OnDatabaseEvent(event database.DeploymentDatabaseEvent) error {
	ss.log.Debugw("Received database event",
		"type", event.Type,
		"appId", event.Deployment.DesiredState.AppId,
		"timestamp", event.Timestamp)

	// Create context with timeout for database event handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	switch event.Type {
	case database.EventDeploymentStatusUpdate:
		if event.Deployment.CurrentState != nil {
			ss.log.Infow("Sending app status update to the orchestrator", "appId", event.Deployment.CurrentState.AppId)
			return ss.sendAppStatusUpdate(ctx, event.Deployment)
		}
	}

	return nil
}

// GetSubscriberID returns a unique identifier for this database subscriber
func (ss *appStateSyncer) GetSubscriberID() string {
	return "app-state-syncer"
}

// ExplicitlyTriggerSync manually triggers a sync operation
func (ss *appStateSyncer) ExplicitlyTriggerSync(ctx context.Context) error {
	ss.log.Info("Explicitly triggering state sync")

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return ss.performStateSync(ctx)
}

func (ss *appStateSyncer) sendAppStatusUpdate(ctx context.Context, deployment database.AppDeployment) error {
	ss.log.Debug("Sending app status update to orchestrator...")

	// Create API client for communication with orchestration service
	client, err := ss.apiClientFactory.NewSBIClient()
	if err != nil {
		ss.log.Errorw("Failed to create API client for status update", "error", err)
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Parse deployment ID as UUID
	appUUID, err := uuid.Parse(deployment.CurrentState.AppId)
	if err != nil {
		ss.log.Errorw("Failed to parse deployment ID as UUID", "appId", deployment.CurrentState.AppId, "error", err)
		return fmt.Errorf("failed to parse deployment ID as UUID: %w", err)
	}

	componentStatus := make([]sbi.ComponentStatus, 0, len(deployment.CurrentComponentsTrack))

	for _, component := range deployment.CurrentComponentsTrack {
		var compStatus sbi.ComponentStatus
		compStatus = sbi.ComponentStatus{
			Name:  component.Name,
			State: component.State, // Use actual current state
			Error: component.Error,
		}

		componentStatus = append(componentStatus, compStatus)
	}

	// Create deployment status payload
	deploymentStatus := sbi.DeploymentStatus{
		ApiVersion:   "deployment.margo/v1",
		Kind:         "DeploymentStatus",
		Components:   componentStatus,
		DeploymentId: appUUID,
		Status: sbi.OverallStatus{
			State: sbi.OverallStatusState(deployment.CurrentState.AppState),
		},
	}

	// Send status update to orchestrator
	resp, err := client.PostDeviceDeviceIdDeploymentDeploymentIdStatus(ctx, ss.config.DeviceID, appUUID, deploymentStatus)
	if err != nil {
		ss.log.Errorw("Failed to send app status update", "appId", deployment.CurrentState.AppId, "error", err)
		return fmt.Errorf("failed to send app status update: %w", err)
	}
	defer resp.Body.Close()

	// Parse the status update response from orchestration service
	statusUpdateResp, err := sbi.ParsePostDeviceDeviceIdDeploymentDeploymentIdStatusResponse(resp)
	if err != nil {
		ss.log.Errorw("Failed to parse status update response", "appId", deployment.CurrentState.AppId, "error", err)
		return fmt.Errorf("failed to parse status update response: %w", err)
	}

	// Handle different response status codes
	switch statusUpdateResp.StatusCode() {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		ss.log.Debugw("Successfully sent app status update",
			"appId", deployment.CurrentState.AppId,
			"statusCode", statusUpdateResp.StatusCode())

		ss.log.Infow("App status update completed successfully", "appId", deployment.CurrentState.AppId)
		return nil

	default:
		ss.log.Errorw("Received error response from orchestrator for status update",
			"appId", deployment.CurrentState.AppId,
			"statusCode", statusUpdateResp.StatusCode())
		return ss.handleErrorResponse(statusUpdateResp.Body, statusUpdateResp.StatusCode(), "send app status update")
	}
}

// syncAppStatesLoop runs the continuous sync loop
func (ss *appStateSyncer) syncAppStatesLoop() {
	ss.log.Info("Starting app state sync loop")

	// Create ticker for consistent intervals
	ticker := time.NewTicker(appStateSeekingInterval)
	defer ticker.Stop()

	// Perform initial sync before starting the loop
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := ss.performStateSync(ctx); err != nil {
		ss.log.Errorw("Initial state sync failed", "error", err)
	}
	cancel()

	for {
		select {
		case <-ticker.C:
			// Create context with timeout for each sync operation
			syncCtx, syncCancel := context.WithTimeout(context.Background(), 30*time.Second)

			if err := ss.performStateSync(syncCtx); err != nil {
				ss.log.Errorw("Failed to sync app states", "error", err)
			}

			syncCancel()

		case <-ss.stopChan:
			ss.log.Info("App state sync loop shutting down")
			return
		}
	}
}

// performStateSync handles the actual state synchronization logic
func (ss *appStateSyncer) performStateSync(ctx context.Context) error {
	ss.log.Debug("Syncing app states...")

	// Fetch current deployments from local database
	currentDeployments, err := ss.database.GetAllDeployments()
	if err != nil {
		return fmt.Errorf("failed to fetch deployments from database: %w", err)
	}

	// Convert deployments map to slice for API request
	currentAppStates := make(sbi.StateJSONRequestBody, 0, len(currentDeployments))
	for _, appState := range currentDeployments {
		if appState.CurrentState != nil {
			currentAppStates = append(currentAppStates, *appState.CurrentState)
		}
	}

	ss.log.Debugw("Sending current states to orchestrator", "count", len(currentAppStates))

	// Create API client for communication with orchestration service
	client, err := ss.apiClientFactory.NewSBIClient()
	if err != nil {
		ss.log.Errorw("Failed to create API client", "error", err)
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Send current states and receive desired states
	resp, err := client.State(ctx, currentAppStates)
	if err != nil {
		ss.log.Errorw("Failed to send state sync request", "error", err)
		return fmt.Errorf("failed to send state sync request: %w", err)
	}
	defer resp.Body.Close()

	// Parse the response from orchestration service
	desiredStateResp, err := sbi.ParseStateResponse(resp)
	if err != nil {
		ss.log.Errorw("Failed to parse app state response", "error", err)
		return fmt.Errorf("failed to parse app state response: %w", err)
	}

	// Handle different response status codes
	switch desiredStateResp.StatusCode() {
	case http.StatusOK, http.StatusAccepted:
		ss.log.Debugw("Received desired state API response",
			"statusCode", desiredStateResp.StatusCode(),
			"hasData", desiredStateResp.JSON200 != nil)

		// Process desired states if provided
		if desiredStateResp.JSON200 != nil {
			ss.log.Debugw("Processing desired states", "response", pretty.Sprint(desiredStateResp.JSON200))

			if err := ss.mergeAppStates(ctx, *desiredStateResp.JSON200); err != nil {
				ss.log.Errorw("Failed to merge app states", "error", err)
				return fmt.Errorf("failed to merge app states: %w", err)
			}
		} else {
			ss.log.Debug("No desired state changes received")
		}

		ss.log.Debug("App states synced successfully")
		return nil

	default:
		ss.log.Errorw("Received error response from orchestrator",
			"statusCode", desiredStateResp.StatusCode())
		return ss.handleErrorResponse(desiredStateResp.Body, desiredStateResp.StatusCode(), "sync app state")
	}
}

// handleErrorResponse processes error responses consistently
func (ss *appStateSyncer) handleErrorResponse(errBody []byte, statusCode int, operation string) error {
	body, err := io.ReadAll(bytes.NewReader(errBody))
	if err != nil {
		ss.log.Errorw("Failed to read response body", "operation", operation, "statusCode", statusCode, "error", err)
		return fmt.Errorf("%s request failed with status %d (could not read response body): %w", operation, statusCode, err)
	}

	ss.log.Errorw("Request failed", "operation", operation, "statusCode", statusCode, "body", string(body))
	return fmt.Errorf("%s request failed with status %d: %s", operation, statusCode, string(body))
}

// mergeAppStates merges the desired app states with the current app states
func (ss *appStateSyncer) mergeAppStates(ctx context.Context, states sbi.DesiredAppStates) error {
	ss.log.Debugw("Merging desired app states", "desiredStates", pretty.Sprint(states))

	for _, state := range states {
		if state.AppDeploymentYAML == nil {
			ss.log.Warnw("Received state with nil AppDeploymentYAML, skipping", "appId", state.AppId)
			continue
		}

		appDeployment, err := pkg.ParseAppDeploymentFromBase64(*state.AppDeploymentYAML)
		if err != nil {
			return fmt.Errorf("failed to parse the deployment yaml from the base64 encoded string, err: %s", err.Error())
		}

		if appDeployment.Metadata.Id == nil {
			ss.log.Warnw("Received AppDeployment with nil ID in the metadata field, skipping", "appId", state.AppId)
			continue
		}
		ss.log.Debugw("Successfully processed AppDeployment", "name", appDeployment.Metadata.Name, "id", *appDeployment.Metadata.Id)

		switch state.AppState {
		case sbi.REMOVING:
			ss.log.Infow("Removing app", "appId", *appDeployment.Metadata.Id)
			ss.database.RemoveDeployment(*appDeployment.Metadata.Id)

		case sbi.RUNNING, sbi.UPDATING:
			ss.log.Infow("Adding/Updating app", "appId", *appDeployment.Metadata.Id, "state", state.AppState)
			newAppState, err := pkg.ConvertAppDeploymentToAppState(appDeployment, *appDeployment.Metadata.Id, "v1", "RUNNING")
			if err != nil {
				ss.log.Errorw("Failed to convert AppDeployment to AppState", "appId", *appDeployment.Metadata.Id, "error", err)
				return err
			}
			ss.database.UpsertDeploymentDesiredState(newAppState)

		default:
			ss.log.Warnw("Unknown app state, skipping", "appId", *appDeployment.Metadata.Id, "state", state.AppState)
		}
	}

	ss.log.Debug("App states merged successfully")
	return nil
}
