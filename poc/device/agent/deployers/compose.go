package deployers

import (
	"context"
	"fmt"

	"github.com/margo/dev-repo/shared-lib/workloads"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"go.uber.org/zap"
)

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
