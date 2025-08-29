package main

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kr/pretty"
	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"go.uber.org/zap"
)

type StatusReporterIfc interface {
	Start()
	Stop()
}

type StatusReporter struct {
	database  database.DatabaseIfc
	apiClient sbi.ClientInterface
	deviceID  string
	log       *zap.SugaredLogger
	stopChan  chan struct{}
}

func NewStatusReporter(db database.DatabaseIfc, client sbi.ClientInterface, deviceID string, log *zap.SugaredLogger) *StatusReporter {
	return &StatusReporter{
		database:  db,
		apiClient: client,
		deviceID:  deviceID,
		log:       log,
		stopChan:  make(chan struct{}),
	}
}

func (sr *StatusReporter) Start() {
	// Subscribe to database changes for status updates
	sr.database.Subscribe(sr.onDeploymentChange)
}

func (sr *StatusReporter) Stop() {
	close(sr.stopChan)
}

func (sr *StatusReporter) onDeploymentChange(appID string, record *database.DeploymentRecord, changeType database.DeploymentChangeType) {
	sr.log.Info("onDeploymentChange", "msg", "event received", "record", pretty.Sprint(record))
	// Report status when phase changes
	if changeType == database.DeploymentChangeTypeDesiredStateAdded ||
		changeType == database.DeploymentChangeTypeComponentPhaseChanged {
		go sr.reportStatus(appID, record)
	}
}

func (sr *StatusReporter) reportStatus(appID string, record *database.DeploymentRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// deploymentId := record.DeploymentId

	appUUID, err := uuid.Parse(appID)
	if err != nil {
		sr.log.Errorw("Invalid app ID format", "appId", appID)
		return
	}

	// Convert component status
	components := make([]sbi.ComponentStatus, 0, len(record.ComponentStatus))
	for _, status := range record.ComponentStatus {
		components = append(components, status)
	}

	deploymentStatus := sbi.DeploymentStatus{
		ApiVersion:   "deployment.margo/v1",
		Kind:         "DeploymentStatus",
		Components:   components,
		DeploymentId: appUUID,
		Status: sbi.OverallStatus{
			State: sbi.OverallStatusState(record.Phase),
		},
	}

	resp, err := sr.apiClient.PostDeviceDeviceIdDeploymentDeploymentIdStatus(ctx, sr.deviceID, appUUID, deploymentStatus)
	if err != nil {
		sr.log.Errorw("Failed to report status", "appId", appID, "error", err)
		return
	}
	defer resp.Body.Close()

	sr.log.Debugw("Status reported", "appId", appID, "phase", record.Phase)
}
