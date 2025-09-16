// sync/state_syncer.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/shared-lib/http/auth"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"github.com/margo/dev-repo/standard/pkg"
	"go.uber.org/zap"
)

type StateSyncerIfc interface {
	Start()
	Stop()
}

type StateSyncer struct {
	database                  *database.Database
	apiClient                 sbi.ClientInterface
	deviceID                  string
	log                       *zap.SugaredLogger
	stopChan                  chan struct{}
	stateSyncingIntervalInSec uint16
}

func NewStateSyncer(
	db *database.Database,
	client sbi.ClientInterface,
	deviceID string,
	stateSeekingIntervalInSec uint16,
	log *zap.SugaredLogger) *StateSyncer {
	return &StateSyncer{
		database:                  db,
		apiClient:                 client,
		deviceID:                  deviceID,
		log:                       log,
		stopChan:                  make(chan struct{}),
		stateSyncingIntervalInSec: stateSeekingIntervalInSec,
	}
}

func (ss *StateSyncer) Start() {
	go ss.syncLoop()
}

func (ss *StateSyncer) Stop() {
	close(ss.stopChan)
}

func (ss *StateSyncer) syncLoop() {
	ticker := time.NewTicker(time.Duration(ss.stateSyncingIntervalInSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ss.performSync()
		case <-ss.stopChan:
			return
		}
	}
}

func (ss *StateSyncer) performSync() {
	ss.log.Debugf("performing sync....")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get current states from database
	deployments := ss.database.ListDeployments()
	currentStates := make([]sbi.AppState, 0, len(deployments))

	for _, deployment := range deployments {
		if deployment.CurrentState != nil {
			currentStates = append(currentStates, *deployment.CurrentState)
		}
	}

	// Send to orchestrator and get desired states
	device, _ := ss.database.GetDeviceSettings()
	var resp *http.Response
	var err error
	if device.AuthEnabled {
		resp, err = ss.apiClient.State(
			ctx,
			currentStates,
			auth.WithDeviceSignature(ctx, string(device.DeviceSignature)),
			auth.WithOAuth(ctx, device.OAuthClientId, device.OAuthClientSecret, device.OAuthTokenEndpointUrl),
		)
	} else {
		resp, err = ss.apiClient.State(
			ctx,
			currentStates,
			auth.WithDeviceSignature(ctx, string(device.DeviceSignature)),
		)
	}
	if err != nil {
		ss.log.Errorw("Failed to sync states", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		ss.log.Errorw("Sync failed", "statusCode", resp.StatusCode)
		return
	}

	// Parse response
	desiredStateResp, err := sbi.ParseStateResponse(resp)
	if err != nil || desiredStateResp.JSON200 == nil {
		ss.log.Errorw("Failed to parse response", "error", err)
		return
	}

	// Update desired states in database
	ss.log.Debugf("setting desired states....")
	for _, desiredState := range *desiredStateResp.JSON200 {
		appDeployment, err := pkg.ConvertAppStateToAppDeployment(desiredState)
		if err != nil {
			ss.database.SetPhase(desiredState.AppId, "FAILED", fmt.Sprintf("Conversion failed: %v", err))
			return
		}
		// if !ss.database.CanDeployAppProfile(string(appDeployment.Spec.DeploymentProfile.Type)) {
		// 	ss.log.Warnw("Received unsupported file type for this agent/runtime, will skip it", "profileType", appDeployment.Spec.DeploymentProfile.Type)
		// 	continue
		// }
		deploymentId := appDeployment.Metadata.Id
		ss.database.SetDesiredState(*deploymentId, desiredState)
	}

	ss.log.Debugw("Sync completed", "desiredStates", len(*desiredStateResp.JSON200))
}
