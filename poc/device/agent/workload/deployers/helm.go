package deployers

import (
	"context"
	"fmt"
	"strings"

	"github.com/kr/pretty"
	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/shared-lib/workloads"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"github.com/margo/dev-repo/standard/pkg"
	"go.uber.org/zap"
)

// HelmDeployer implements WorkloadDeployer for Helm deployments
type HelmDeployer struct {
	client   *workloads.HelmClient
	database database.AgentDatabase // Add this field
	log      *zap.SugaredLogger
}

// Update constructors to pass database reference
func NewHelmDeployer(client *workloads.HelmClient, database database.AgentDatabase, log *zap.SugaredLogger) *HelmDeployer {
	return &HelmDeployer{
		client:   client,
		database: database, // Add this field
		log:      log,
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
	releaseName := generateReleaseName(*deployment.Metadata.Id, componentAsHelm.Name)
	revision := *componentAsHelm.Properties.Revision
	wait := componentAsHelm.Properties.Wait != nil && *componentAsHelm.Properties.Wait
	overrides := componentValues[componentAsHelm.Name]

	if err := h.client.InstallChart(ctx, releaseName, repository, namespace, revision, wait, overrides); err != nil {
		return fmt.Errorf("failed to install helm chart: %s", err.Error())
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
	releaseName := generateReleaseName(*deployment.Metadata.Id, componentAsHelm.Name)
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

	// FIX: Need to get component name to generate correct release name
	// Get deployment from database to determine component name
	deployment, err := h.database.GetDeployment(appID) // You'll need to pass database to HelmDeployer
	if err != nil {
		return fmt.Errorf("failed to get app from database: %w", err)
	}

	appDeployment, err := pkg.ConvertAppStateToAppDeployment(*deployment.CurrentState)
	if err != nil {
		return fmt.Errorf("failed to convert AppState to AppDeployment: %w", err)
	}

	component := appDeployment.Spec.DeploymentProfile.Components[0]
	componentAsHelm, err := component.AsHelmApplicationDeploymentProfileComponent()
	if err != nil {
		return fmt.Errorf("invalid helm deployment profile: %w", err)
	}

	releaseName := generateReleaseName(appID, componentAsHelm.Name)
	namespace := ""

	if err := h.client.UninstallChart(ctx, releaseName, namespace); err != nil {
		return fmt.Errorf("failed to uninstall helm chart: %w", err)
	}

	h.log.Infow("Successfully removed Helm workload", "appId", appID, "releaseName", releaseName)
	return nil
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
