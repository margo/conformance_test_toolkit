package monitoring

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/shared-lib/workloads"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"github.com/margo/dev-repo/standard/pkg"
	"go.uber.org/zap"
)

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

			if err := h.database.UpdateWorkloadStatus(appID, sbi.AppStateAppState(status.Status)); err != nil {
				h.log.Errorw("Failed to update the status of the workload in database", "appId", appID, "error", err.Error())
			}
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

func generateReleaseName(appID, componentName string) string {
	// Use a consistent naming strategy: appID-componentName (truncated if needed)
	releaseName := fmt.Sprintf("%s-%s", appID, componentName)

	// Helm release names must be <= 53 characters and DNS-1123 compliant
	if len(releaseName) > 53 {
		// Use first 8 chars of appID + component name
		shortAppID := appID[:8]
		maxComponentLen := 53 - len(shortAppID) - 1
		if len(componentName) > maxComponentLen {
			componentName = componentName[:maxComponentLen]
		}
		releaseName = fmt.Sprintf("%s-%s", shortAppID, componentName)
	}

	// Ensure DNS-1123 compliance
	releaseName = strings.ToLower(releaseName)
	releaseName = strings.ReplaceAll(releaseName, "_", "-")

	return releaseName
}
