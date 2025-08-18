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

	// FIX: Get the actual release name used during deployment
	deployment, err := h.database.GetDeployment(appID)
	if err != nil {
		return err
	}

	appDeployment, err := pkg.ConvertAppStateToAppDeployment(*deployment.DesiredState)
	if err != nil {
		return err
	}

	for _, component := range appDeployment.Spec.DeploymentProfile.Components {
		componentAsHelm, _ := component.AsHelmApplicationDeploymentProfileComponent()
		go h.monitorLoop(ctx, appID, componentAsHelm.Name)
	}

	return nil
}

func (h *HelmMonitor) StopWatching(ctx context.Context, appID string) error {
	h.log.Infow("Stopping watch for Helm workload", "appId", appID)
	// Context cancellation will stop the monitoring loop
	return nil
}

// In HelmMonitor.GetStatus() - FIXED
func (h *HelmMonitor) GetStatus(ctx context.Context, appID, componentName string) (sbi.ComponentStatus, error) {
	h.log.Debugw("Getting Helm workload status", "appId", appID)

	// FIX: Get the actual release name used during deployment
	deployment, err := h.database.GetDeployment(appID)
	if err != nil {
		return sbi.ComponentStatus{}, nil
	}

	var appState *sbi.AppState = deployment.DesiredState
	if deployment.CurrentState != nil {
		appState = deployment.CurrentState
	}
	appDeployment, err := pkg.ConvertAppStateToAppDeployment(*appState)
	if err != nil {
		return sbi.ComponentStatus{}, err
	}

	componentAsHelm, _ := appDeployment.Spec.DeploymentProfile.Components[0].AsHelmApplicationDeploymentProfileComponent()

	for _, componentInDb := range appDeployment.Spec.DeploymentProfile.Components {
		tempHelmComponent, _ := componentInDb.AsHelmApplicationDeploymentProfileComponent()
		if componentAsHelm.Name == tempHelmComponent.Name {
			componentAsHelm = tempHelmComponent
		}
	}
	if err != nil {
		return sbi.ComponentStatus{}, err
	}

	releaseName := generateReleaseName(appID, componentAsHelm.Name)
	namespace := ""

	// Get release status from Helm
	status, err := h.client.GetReleaseStatus(ctx, releaseName, namespace)
	if err != nil {
		return sbi.ComponentStatus{}, err
	}

	return sbi.ComponentStatus{
		State: sbi.ComponentStatusState(status.Status),
	}, nil
}

func (h *HelmMonitor) monitorLoop(ctx context.Context, deploymentId, componentName string) error {
	ticker := time.NewTicker(30 * time.Second) // Monitor every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.log.Debugw("Stopping monitor loop for Helm workload", "appId", deploymentId)
			return nil
		case <-ticker.C:
			status, err := h.GetStatus(ctx, deploymentId, componentName)
			if err != nil {
				h.log.Errorw("Failed to get workload status", "appId", deploymentId, "error", err)
				continue
			}

			h.log.Debugw("Workload status check",
				"appId", deploymentId,
				"status", status.State)

			if err := h.database.UpsertComponentStatus(deploymentId, componentName, status); err != nil {
				h.log.Errorw("Failed to update the status of the workload in database", "appId", deploymentId, "error", err.Error())
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
