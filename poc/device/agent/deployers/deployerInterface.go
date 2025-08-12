package deployers

import (
	"context"

	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
)

// WorkloadDeployer interface for different deployment types
type WorkloadDeployer interface {
	Deploy(ctx context.Context, deployment sbi.AppDeployment) error
	Update(ctx context.Context, deployment sbi.AppDeployment) error
	Remove(ctx context.Context, appID string) error
	GetType() string
}
