// shared-lib/workloads/compose.go
package workloads

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/margo/dev-repo/shared-lib/file"

	// "github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"gopkg.in/yaml.v3"
)

// DockerComposeClient handles Docker Compose operations using Go libraries
type DockerComposeClient struct {
	dockerClient *client.Client
	workingDir   string
}

// ComposeError represents typed Docker Compose errors
type ComposeError struct {
	Type    string
	Message string
	Err     error
}

func (e *ComposeError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *ComposeError) Unwrap() error {
	return e.Err
}

// Error types for Docker Compose
const (
	ComposeErrorTypeNotFound     = "NotFound"
	ComposeErrorTypeInvalidInput = "InvalidInput"
	ComposeErrorTypeExecution    = "Execution"
	ComposeErrorTypeFile         = "File"
	ComposeErrorTypeDocker       = "Docker"
	ComposeErrorTypeNetworkError = "Network"
)

// ComposeStatus represents the status of a Docker Compose deployment
type ComposeStatus struct {
	Name      string          `json:"name"`
	Status    string          `json:"status"` // running, stopped, failed, partial
	Services  []ServiceStatus `json:"services"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ServiceStatus represents the status of individual services
type ServiceStatus struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"` // running, stopped, failed, starting
	Image       string   `json:"image"`
	Ports       []string `json:"ports"`
	ContainerID string   `json:"container_id"`
	Health      string   `json:"health"`
}

// ComposeFile represents a docker-compose.yml structure
type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
	Networks map[string]ComposeNetwork `yaml:"networks,omitempty"`
	Volumes  map[string]ComposeVolume  `yaml:"volumes,omitempty"`
}

type ComposeService struct {
	Image         string            `yaml:"image"`
	ContainerName string            `yaml:"container_name,omitempty"`
	Ports         []string          `yaml:"ports,omitempty"`
	Environment   map[string]string `yaml:"environment,omitempty"`
	Volumes       []string          `yaml:"volumes,omitempty"`
	Networks      []string          `yaml:"networks,omitempty"`
	DependsOn     []string          `yaml:"depends_on,omitempty"`
	Restart       string            `yaml:"restart,omitempty"`
	Command       []string          `yaml:"command,omitempty"`
	WorkingDir    string            `yaml:"working_dir,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
}

type ComposeNetwork struct {
	Driver string            `yaml:"driver,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

type ComposeVolume struct {
	Driver string            `yaml:"driver,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

type DockerConnectionViaHttp struct {
	Protocol   string
	Host       string
	Port       uint16
	CaCertPath string
	CertPath   string
	KeyPath    string
}

type DockerConnectionViaSocket struct {
	SocketPath string
}

type DockerConnectivityParams struct {
	ViaHttp   *DockerConnectionViaHttp
	ViaSocket *DockerConnectionViaSocket
}

func NewDockerComposeClient(params DockerConnectivityParams) (*DockerComposeClient, error) {
	// Create Docker client
	var dockerClient *client.Client
	var err error
	if params.ViaHttp != nil {
		hostURL := fmt.Sprintf("%s://%s:%d", params.ViaHttp.Protocol, params.ViaHttp.Host, params.ViaHttp.Port)
		dockerClient, err = client.NewClientWithOpts(
			client.WithHost(hostURL),
			client.WithTLSClientConfig(params.ViaHttp.CaCertPath, params.ViaHttp.CertPath, params.ViaHttp.KeyPath),
			client.WithAPIVersionNegotiation(),
			// client.WithVersion("1.48"),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker client: %w", err)
		}
	}

	if params.ViaSocket != nil {
		dockerClient, err = client.NewClientWithOpts(client.WithHost(params.ViaSocket.SocketPath))
	}

	dockerClient.NegotiateAPIVersion(context.Background())

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = dockerClient.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docker daemon: %w", err)
	}

	workingDir := "/tmp/compose-deployments"
	if err := os.MkdirAll(workingDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create working directory: %w", err)
	}

	return &DockerComposeClient{
		dockerClient: dockerClient,
		workingDir:   workingDir,
	}, nil
}

// DeployCompose deploys a Docker Compose application using Go libraries
func (c *DockerComposeClient) DeployComposeFromURL(ctx context.Context, projectName string, composeFileURL string, envVars map[string]string) error {
	if strings.TrimSpace(projectName) == "" {
		return &ComposeError{
			Type:    ComposeErrorTypeInvalidInput,
			Message: "project name cannot be empty",
		}
	}

	if strings.TrimSpace(composeFileURL) == "" {
		return &ComposeError{
			Type:    ComposeErrorTypeInvalidInput,
			Message: "compose file URL cannot be empty",
		}
	}

	// Fetch compose file content from URL
	composeContent, err := c.FetchComposeFileFromURL(ctx, composeFileURL)
	if err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeNetworkError,
			Message: "failed to fetch compose file from URL",
			Err:     err,
		}
	}

	// Parse compose file
	var composeFile ComposeFile
	if err := yaml.Unmarshal(composeContent, &composeFile); err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeInvalidInput,
			Message: "failed to parse compose file",
			Err:     err,
		}
	}

	// Create project network
	networkName := fmt.Sprintf("%s_default", projectName)
	if err := c.createNetwork(ctx, networkName, projectName); err != nil {
		return err
	}

	// Deploy services in dependency order
	deployedServices := make(map[string]bool)

	for len(deployedServices) < len(composeFile.Services) {
		deployed := false

		for serviceName, service := range composeFile.Services {
			if deployedServices[serviceName] {
				continue
			}

			// Check if dependencies are deployed
			canDeploy := true
			for _, dep := range service.DependsOn {
				if !deployedServices[dep] {
					canDeploy = false
					break
				}
			}

			if canDeploy {
				if err := c.deployService(ctx, projectName, serviceName, service, networkName, envVars); err != nil {
					return err
				}
				deployedServices[serviceName] = true
				deployed = true
			}
		}

		if !deployed {
			return &ComposeError{
				Type:    ComposeErrorTypeExecution,
				Message: "circular dependency detected or unresolvable dependencies",
			}
		}
	}

	return nil
}

// DeployCompose deploys a Docker Compose application using Go libraries
func (c *DockerComposeClient) DeployCompose(ctx context.Context, projectName string, composeContent []byte, envVars map[string]string) error {
	if strings.TrimSpace(projectName) == "" {
		return &ComposeError{
			Type:    ComposeErrorTypeInvalidInput,
			Message: "project name cannot be empty",
		}
	}

	// Parse compose file
	var composeFile ComposeFile
	if err := yaml.Unmarshal(composeContent, &composeFile); err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeInvalidInput,
			Message: "failed to parse compose file",
			Err:     err,
		}
	}

	// Create project network
	networkName := fmt.Sprintf("%s_default", projectName)
	if err := c.createNetwork(ctx, networkName, projectName); err != nil {
		return err
	}

	// Deploy services in dependency order
	deployedServices := make(map[string]bool)

	for len(deployedServices) < len(composeFile.Services) {
		deployed := false

		for serviceName, service := range composeFile.Services {
			if deployedServices[serviceName] {
				continue
			}

			// Check if dependencies are deployed
			canDeploy := true
			for _, dep := range service.DependsOn {
				if !deployedServices[dep] {
					canDeploy = false
					break
				}
			}

			if canDeploy {
				if err := c.deployService(ctx, projectName, serviceName, service, networkName, envVars); err != nil {
					return err
				}
				deployedServices[serviceName] = true
				deployed = true
			}
		}

		if !deployed {
			return &ComposeError{
				Type:    ComposeErrorTypeExecution,
				Message: "circular dependency detected or unresolvable dependencies",
			}
		}
	}

	return nil
}

// deployService deploys a single service
func (c *DockerComposeClient) deployService(ctx context.Context, projectName, serviceName string, service ComposeService, networkName string, envVars map[string]string) error {
	containerName := service.ContainerName
	if containerName == "" {
		containerName = fmt.Sprintf("%s_%s_1", projectName, serviceName)
	}

	// Check if container already exists
	existingContainer, err := c.dockerClient.ContainerInspect(ctx, containerName)
	if err == nil {
		// Container exists, remove it first
		if err := c.dockerClient.ContainerRemove(ctx, existingContainer.ID, container.RemoveOptions{Force: true}); err != nil {
			return &ComposeError{
				Type:    ComposeErrorTypeDocker,
				Message: fmt.Sprintf("failed to remove existing container %s", containerName),
				Err:     err,
			}
		}
	}

	// Pull image
	reader, err := c.dockerClient.ImagePull(ctx, service.Image, image.PullOptions{})
	if err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeDocker,
			Message: fmt.Sprintf("failed to pull image %s", service.Image),
			Err:     err,
		}
	}
	io.Copy(io.Discard, reader)
	reader.Close()

	// Prepare environment variables
	env := make([]string, 0)
	for key, value := range envVars {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	for key, value := range service.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Prepare port bindings
	portBindings := nat.PortMap{}
	exposedPorts := nat.PortSet{}

	for _, portMapping := range service.Ports {
		parts := strings.Split(portMapping, ":")
		if len(parts) == 2 {
			containerPort := nat.Port(parts[1] + "/tcp")
			exposedPorts[containerPort] = struct{}{}
			portBindings[containerPort] = []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: parts[0],
				},
			}
		}
	}

	// Prepare volumes
	binds := make([]string, 0)
	for _, volume := range service.Volumes {
		binds = append(binds, volume)
	}

	// Prepare labels
	labels := make(map[string]string)
	labels["com.docker.compose.project"] = projectName
	labels["com.docker.compose.service"] = serviceName
	for key, value := range service.Labels {
		labels[key] = value
	}

	// Create container
	containerConfig := &container.Config{
		Image:        service.Image,
		Env:          env,
		ExposedPorts: exposedPorts,
		Labels:       labels,
		WorkingDir:   service.WorkingDir,
	}

	if len(service.Command) > 0 {
		containerConfig.Cmd = service.Command
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Binds:        binds,
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyAlways,
		},
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {},
		},
	}

	resp, err := c.dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, nil, containerName)
	if err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeDocker,
			Message: fmt.Sprintf("failed to create container %s", containerName),
			Err:     err,
		}
	}

	// Start container
	if err := c.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeDocker,
			Message: fmt.Sprintf("failed to start container %s", containerName),
			Err:     err,
		}
	}

	return nil
}

// createNetwork creates a Docker network for the project
func (c *DockerComposeClient) createNetwork(ctx context.Context, networkName, projectName string) error {
	// Check if network already exists
	networks, err := c.dockerClient.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeDocker,
			Message: "failed to list networks",
			Err:     err,
		}
	}

	for _, net := range networks {
		if net.Name == networkName {
			return nil // Network already exists
		}
	}

	// Create network
	_, err = c.dockerClient.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"com.docker.compose.project": projectName,
			"com.docker.compose.network": "default",
		},
	})

	if err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeDocker,
			Message: fmt.Sprintf("failed to create network %s", networkName),
			Err:     err,
		}
	}

	return nil
}

// UpdateCompose updates an existing Docker Compose deployment
func (c *DockerComposeClient) UpdateCompose(ctx context.Context, projectName, composeContent string, envVars map[string]string) error {
	// For Docker Compose, update is the same as deploy (recreate containers)
	return c.DeployCompose(ctx, projectName, []byte(composeContent), envVars)
}

// RemoveCompose removes a Docker Compose deployment
func (c *DockerComposeClient) RemoveCompose(ctx context.Context, projectName string) error {
	if strings.TrimSpace(projectName) == "" {
		return &ComposeError{
			Type:    ComposeErrorTypeInvalidInput,
			Message: "project name cannot be empty",
		}
	}

	// List containers with project label
	argFilters := filters.NewArgs()
	argFilters.Add("label", fmt.Sprintf("com.docker.compose.project=%s", projectName))
	containers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: argFilters,
	})
	if err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeDocker,
			Message: "failed to list containers",
			Err:     err,
		}
	}

	// Remove containers
	for _, containerObj := range containers {
		if err := c.dockerClient.ContainerRemove(ctx, containerObj.ID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		}); err != nil {
			return &ComposeError{
				Type:    ComposeErrorTypeDocker,
				Message: fmt.Sprintf("failed to remove container %s", containerObj.ID),
				Err:     err,
			}
		}
	}

	// Remove network
	networkName := fmt.Sprintf("%s_default", projectName)
	if err := c.dockerClient.NetworkRemove(ctx, networkName); err != nil {
		// Don't fail if network doesn't exist
		if !client.IsErrNotFound(err) {
			return &ComposeError{
				Type:    ComposeErrorTypeDocker,
				Message: fmt.Sprintf("failed to remove network %s", networkName),
				Err:     err,
			}
		}
	}

	return nil
}

// GetComposeStatus gets the status of a Docker Compose deployment
func (c *DockerComposeClient) GetComposeStatus(ctx context.Context, projectName string) (*ComposeStatus, error) {
	if strings.TrimSpace(projectName) == "" {
		return nil, &ComposeError{
			Type:    ComposeErrorTypeInvalidInput,
			Message: "project name cannot be empty",
		}
	}

	// List containers with project label
	argFilters := filters.NewArgs()
	argFilters.Add("label", fmt.Sprintf("com.docker.compose.project=%s", projectName))
	containers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: argFilters,
	})
	if err != nil {
		fmt.Println("getComposeStatus", "err", err.Error())
		return nil, &ComposeError{
			Type:    ComposeErrorTypeDocker,
			Message: "failed to list containers",
			Err:     err,
		}
	}

	if len(containers) == 0 {
		return nil, &ComposeError{
			Type:    ComposeErrorTypeNotFound,
			Message: fmt.Sprintf("compose project %s not found", projectName),
		}
	}

	var services []ServiceStatus
	runningCount := 0

	for _, container := range containers {
		serviceName := container.Labels["com.docker.compose.service"]
		if serviceName == "" {
			serviceName = "unknown"
		}

		status := "stopped"
		switch container.State {
		case "running":
			status = "running"
			runningCount++
		case "exited":
			status = "stopped"
		default:
			status = container.State
		}

		// Get port mappings
		ports := make([]string, 0)
		for _, port := range container.Ports {
			if port.PublicPort > 0 {
				ports = append(ports, fmt.Sprintf("%d:%d", port.PublicPort, port.PrivatePort))
			}
		}

		services = append(services, ServiceStatus{
			Name:        serviceName,
			Status:      status,
			Image:       container.Image,
			Ports:       ports,
			ContainerID: container.ID[:12],
			Health:      "unknown",
		})
	}

	// Determine overall status
	overallStatus := "stopped"
	if runningCount == len(services) {
		overallStatus = "running"
	} else if runningCount > 0 {
		overallStatus = "partial"
	}

	return &ComposeStatus{
		Name:      projectName,
		Status:    overallStatus,
		Services:  services,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// ComposeExists checks if a compose project exists
func (c *DockerComposeClient) ComposeExists(ctx context.Context, projectName string) (bool, error) {
	_, err := c.GetComposeStatus(ctx, projectName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RestartCompose restarts a Docker Compose deployment
func (c *DockerComposeClient) RestartCompose(ctx context.Context, projectName string) error {
	argFilters := filters.NewArgs()
	argFilters.Add("label", fmt.Sprintf("com.docker.compose.project=%s", projectName))
	containers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: argFilters,
	})
	if err != nil {
		return &ComposeError{
			Type:    ComposeErrorTypeDocker,
			Message: "failed to list containers",
			Err:     err,
		}
	}

	for _, containerObj := range containers {
		if err := c.dockerClient.ContainerRestart(ctx, containerObj.ID, container.StopOptions{}); err != nil {
			return &ComposeError{
				Type:    ComposeErrorTypeDocker,
				Message: fmt.Sprintf("failed to restart container %s", containerObj.ID),
				Err:     err,
			}
		}
	}

	return nil
}

// FetchComposeFileFromURL fetches the compose file content from a URL
func (c *DockerComposeClient) FetchComposeFileFromURL(ctx context.Context, url string) ([]byte, error) {
	// Create request with context
	downloadResult, err := file.DownloadFileUsingHttp("GET", url, nil, nil, nil, &file.DownloadOptions{
		OutputPath:     c.workingDir,
		CreateDirs:     true,
		OverwriteExist: true,
		ResumeDownload: true,
		ProgressCallback: func(downloaded, total int64) {
			fmt.Printf("\nTotal: %d, Downloaded: %d", total, downloaded)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}

	fileHandler, err := os.Open(downloadResult.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer fileHandler.Close()

	content, err := io.ReadAll(fileHandler)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(content) == 0 {
		return nil, fmt.Errorf("compose file is empty")
	}

	return content, nil
}
