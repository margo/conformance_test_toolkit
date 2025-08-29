package types

import (
	"fmt"

	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
)

// APIClient interface
type APIClientInterface interface {
	NewSBIClient() (sbi.ClientInterface, error)
}

// ApiClient implementation
type ApiClient struct {
	// You can add any dependencies needed for creating the client here, like auth info
	// clientId, clientSecret, tokenUrl string
	SBIUrl string
}

// NewClient creates a new sbi.Client
func (f *ApiClient) NewSBIClient() (sbi.ClientInterface, error) {
	client, err := sbi.NewClient(f.SBIUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to create API client: %w", err)
	}
	client.RequestEditors = []sbi.RequestEditorFn{}
	return client, nil
}
