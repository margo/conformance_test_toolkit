package workloads

import (
	"context"
	"fmt"
	"log"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
)

// HelmClient represents a Helm client with common settings
type HelmClient struct {
	settings *cli.EnvSettings
	config   *action.Configuration
}

// NewHelmClient creates a new Helm client
func NewHelmClient(kubeconfigPath string) (*HelmClient, error) {
	settings := cli.New()
	settings.KubeConfig = kubeconfigPath
	config := new(action.Configuration)

	// Initialize action configuration
	if err := config.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), log.Printf); err != nil {
		return nil, fmt.Errorf("failed to initialize helm configuration: %w", err)
	}

	return &HelmClient{
		settings: settings,
		config:   config,
	}, nil
}

type HelmRepoAuth struct {
	CertAuth              *HelmRepoCertAuthentication
	BasicAuth             *HelmRepoBasicAuthentication
	InsecureSkipTLSverify *bool `json:"insecure_skip_tls_verify"`
}

type HelmRepoBasicAuthentication struct {
	Username string
	Password string
}

type HelmRepoCertAuthentication struct {
	CertFile           string `json:"certFile"`
	KeyFile            string `json:"keyFile"`
	CAFile             string `json:"caFile"`
	PassCredentialsAll bool   `json:"pass_credentials_all"`
}

// AddRepository adds a Helm repository
func (c *HelmClient) AddRepository(name, url string, auth HelmRepoAuth) error {
	repoEntry := repo.Entry{
		Name: name,
		URL:  url,
	}

	if auth.BasicAuth != nil {
		repoEntry.Username = auth.BasicAuth.Username
		repoEntry.Password = auth.BasicAuth.Password
	}

	if auth.InsecureSkipTLSverify != nil {
		repoEntry.InsecureSkipTLSverify = *auth.InsecureSkipTLSverify
	}

	if auth.CertAuth != nil {
		repoEntry.CAFile = auth.CertAuth.CAFile
		repoEntry.CertFile = auth.CertAuth.CertFile
		repoEntry.KeyFile = auth.CertAuth.KeyFile
		repoEntry.PassCredentialsAll = auth.CertAuth.PassCredentialsAll
	}

	repository, err := repo.NewChartRepository(&repoEntry, getter.All(c.settings))
	if err != nil {
		return fmt.Errorf("failed to create chart repository: %w", err)
	}

	if _, err := repository.DownloadIndexFile(); err != nil {
		return fmt.Errorf("failed to download repository index: %w", err)
	}

	return nil
}

// InstallChart installs a Helm chart
func (c *HelmClient) InstallChart(ctx context.Context, releaseName, chart, namespace, revision string, wait bool, values map[string]interface{}) error {
	install := action.NewInstall(c.config)
	install.ReleaseName = releaseName
	install.Namespace = namespace
	install.Version = revision
	install.Wait = wait

	chartPath, err := install.ChartPathOptions.LocateChart(chart, c.settings)
	if err != nil {
		return fmt.Errorf("failed to locate chart: %w", err)
	}

	chartReq, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

	_, err = install.RunWithContext(ctx, chartReq, values)
	if err != nil {
		return fmt.Errorf("failed to install chart: %w", err)
	}

	return nil
}

// UninstallChart uninstalls a Helm release
func (c *HelmClient) UninstallChart(ctx context.Context, name, namespace string) error {
	uninstall := action.NewUninstall(c.config)

	_, err := uninstall.Run(name)
	if err != nil {
		return fmt.Errorf("failed to uninstall release %s: %w", name, err)
	}

	return nil
}

// UpdateChart upgrades a Helm release
func (c *HelmClient) UpdateChart(ctx context.Context, name, chart, namespace string, values map[string]interface{}) error {
	upgrade := action.NewUpgrade(c.config)
	upgrade.Namespace = namespace

	chartPath, err := upgrade.ChartPathOptions.LocateChart(chart, c.settings)
	if err != nil {
		return fmt.Errorf("failed to locate chart: %w", err)
	}

	chartReq, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

	_, err = upgrade.RunWithContext(ctx, name, chartReq, values)
	if err != nil {
		return fmt.Errorf("failed to upgrade release %s: %w", name, err)
	}

	return nil
}

// ReleaseStatus represents the status of a Helm release
type ReleaseStatus struct {
	Name        string                 `json:"name"`
	Namespace   string                 `json:"namespace"`
	Status      string                 `json:"status"`
	Revision    int                    `json:"revision"`
	Updated     string                 `json:"updated"`
	Chart       string                 `json:"chart"`
	AppVersion  string                 `json:"app_version"`
	Description string                 `json:"description"`
	Notes       string                 `json:"notes"`
	Values      map[string]interface{} `json:"values"`
}

// GetReleaseStatus retrieves the status of a Helm release
func (c *HelmClient) GetReleaseStatus(ctx context.Context, releaseName, namespace string) (*ReleaseStatus, error) {
	// Create status action
	status := action.NewStatus(c.config)

	// Get release status
	release, err := status.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get status for release %s: %w", releaseName, err)
	}

	if release == nil {
		return nil, fmt.Errorf("release %s not found", releaseName)
	}

	// Convert Helm release to our ReleaseStatus struct
	releaseStatus := &ReleaseStatus{
		Name:        release.Name,
		Namespace:   release.Namespace,
		Status:      release.Info.Status.String(),
		Revision:    release.Version,
		Description: release.Info.Description,
		Notes:       release.Info.Notes,
	}

	// Set updated time if available
	releaseStatus.Updated = release.Info.LastDeployed.Format("2006-01-02 15:04:05")

	// Set chart information if available
	if release.Chart != nil && release.Chart.Metadata != nil {
		releaseStatus.Chart = fmt.Sprintf("%s-%s", release.Chart.Metadata.Name, release.Chart.Metadata.Version)
		releaseStatus.AppVersion = release.Chart.Metadata.AppVersion
	}

	// Set values if available
	if release.Config != nil {
		releaseStatus.Values = release.Config
	} else {
		releaseStatus.Values = make(map[string]interface{})
	}

	return releaseStatus, nil
}

// ListReleases lists all Helm releases in the specified namespace
func (c *HelmClient) ListReleases(ctx context.Context, namespace string) ([]*ReleaseStatus, error) {
	list := action.NewList(c.config)

	// Set namespace if provided, otherwise list from all namespaces
	list.AllNamespaces = true

	// Get all releases
	releases, err := list.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	// Convert to our ReleaseStatus format
	var releaseStatuses []*ReleaseStatus
	for _, release := range releases {
		status := &ReleaseStatus{
			Name:        release.Name,
			Namespace:   release.Namespace,
			Status:      release.Info.Status.String(),
			Revision:    release.Version,
			Description: release.Info.Description,
		}

		status.Updated = release.Info.LastDeployed.Format("2006-01-02 15:04:05")

		if release.Chart != nil && release.Chart.Metadata != nil {
			status.Chart = fmt.Sprintf("%s-%s", release.Chart.Metadata.Name, release.Chart.Metadata.Version)
			status.AppVersion = release.Chart.Metadata.AppVersion
		}

		releaseStatuses = append(releaseStatuses, status)
	}

	return releaseStatuses, nil
}

// GetReleaseHistory gets the revision history for a release
func (c *HelmClient) GetReleaseHistory(ctx context.Context, releaseName, namespace string) ([]*ReleaseStatus, error) {
	history := action.NewHistory(c.config)

	releases, err := history.Run(releaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get history for release %s: %w", releaseName, err)
	}

	var releaseHistory []*ReleaseStatus
	for _, release := range releases {
		status := &ReleaseStatus{
			Name:        release.Name,
			Namespace:   release.Namespace,
			Status:      release.Info.Status.String(),
			Revision:    release.Version,
			Description: release.Info.Description,
		}

		status.Updated = release.Info.LastDeployed.Format("2006-01-02 15:04:05")

		if release.Chart != nil && release.Chart.Metadata != nil {
			status.Chart = fmt.Sprintf("%s-%s", release.Chart.Metadata.Name, release.Chart.Metadata.Version)
			status.AppVersion = release.Chart.Metadata.AppVersion
		}

		releaseHistory = append(releaseHistory, status)
	}

	return releaseHistory, nil
}

// ReleaseExists checks if a release exists
func (c *HelmClient) ReleaseExists(ctx context.Context, releaseName, namespace string) (bool, error) {
	_, err := c.GetReleaseStatus(ctx, releaseName, namespace)
	if err != nil {
		// If the error is "not found", return false without error
		if fmt.Sprintf("%v", err) == fmt.Sprintf("failed to get status for release %s: release: not found", releaseName) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
