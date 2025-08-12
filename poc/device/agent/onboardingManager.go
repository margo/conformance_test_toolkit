package main

import (
	"context"
	"fmt"
	"time"

	"github.com/margo/dev-repo/non-standard/pkg/utils"
	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"go.uber.org/zap"
)

type OnboardingManager interface {
	// All methods should accept context for proper cancellation
	Onboard(ctx context.Context) (string, error)
	KeepTryingOnboardingIfFailed(ctx context.Context) (string, error)
	IsOnboarded(ctx context.Context) error
	GetDeviceId(ctx context.Context) string
	// Add lifecycle methods
	Start() error
	Stop() error
}

type oauthBasedOnboardingManager struct {
	config           *Config
	database         database.AgentDatabase
	log              *zap.SugaredLogger
	apiClientFactory APIClientInterface

	// Lifecycle management - separate from operation contexts
	started  bool
	stopChan chan struct{}
}

func NewOAuthBasedOnboardingManager(
	log *zap.SugaredLogger,
	config *Config,
	database database.AgentDatabase,
	apiClientFactory APIClientInterface,
) OnboardingManager {
	return &oauthBasedOnboardingManager{
		config:           config,
		database:         database,
		log:              log,
		apiClientFactory: apiClientFactory,
		stopChan:         make(chan struct{}),
	}
}

func (om *oauthBasedOnboardingManager) Start() error {
	om.log.Info("Starting OnboardingManager")
	om.started = true
	return nil
}

func (om *oauthBasedOnboardingManager) Stop() error {
	om.log.Info("Stopping OnboardingManager")
	close(om.stopChan)
	om.started = false
	return nil
}

func (om *oauthBasedOnboardingManager) KeepTryingOnboardingIfFailed(ctx context.Context) (string, error) {
	om.log.Info("Starting onboarding retry process...")

	retryCount := 0
	maxRetries := 10
	retryDelay := 5 * time.Second

	for retryCount < maxRetries {
		retryCount++
		om.log.Infow("Attempting device onboarding",
			"attempt", retryCount,
			"maxRetries", maxRetries)

		// Pass context to Onboard method
		deviceId, err := om.Onboard(ctx)
		if err == nil {
			om.log.Infow("Device onboarding completed successfully",
				"deviceId", deviceId,
				"attempts", retryCount)
			return deviceId, nil
		}

		om.log.Warnw("Device onboarding failed, will retry",
			"error", err.Error(),
			"attempt", retryCount,
			"maxRetries", maxRetries,
			"retryDelay", retryDelay)

		// Don't sleep after last failed attempt
		if retryCount >= maxRetries {
			break
		}

		// Interruptible sleep
		select {
		case <-time.After(retryDelay):
			// Continue to next attempt
		case <-ctx.Done():
			om.log.Info("Onboarding cancelled during retry delay")
			return "", ctx.Err()
		case <-om.stopChan:
			om.log.Info("Onboarding cancelled by manager stop during retry")
			return "", fmt.Errorf("onboarding manager stopped")
		}
	}

	return "", fmt.Errorf("device onboarding failed after %d attempts", maxRetries)
}

func (om *oauthBasedOnboardingManager) Onboard(ctx context.Context) (string, error) {
	om.log.Info("Attempting device onboarding...")

	// Fetch device info from database with context
	om.log.Debug("Fetching device information from database...")
	device, err := om.database.GetDevice() // TODO: Add context support to database
	if err != nil {
		om.log.Errorw("Failed to fetch device details from database", "error", err)
		return "", fmt.Errorf("failed to fetch device details from database: %w", err)
	}

	// Check if device is already onboarded
	if device != nil && device.DeviceProperties != nil && device.DeviceAuth != nil {
		om.log.Infow("Device already onboarded, skipping onboarding process",
			"deviceId", device.DeviceProperties.Id)
		return device.DeviceProperties.Id, nil
	}

	om.log.Info("Device not onboarded, proceeding with onboarding...")

	// Create API client
	om.log.Debug("Creating SBI API client...")
	client, err := om.apiClientFactory.NewSBIClient()
	if err != nil {
		om.log.Errorw("Failed to create SBI API client", "error", err)
		return "", fmt.Errorf("failed to create SBI API client: %w", err)
	}

	// Create onboarding request
	onboardingReq := sbi.PostOnboardingDeviceJSONRequestBody{}

	// Send onboarding request with context
	om.log.Info("Sending device onboarding request to orchestrator...")
	resp, err := client.PostOnboardingDevice(ctx, onboardingReq) // Use passed context
	if err != nil {
		om.log.Errorw("Failed to send onboarding request", "error", err)
		return "", fmt.Errorf("failed to send onboarding request: %w", err)
	}
	defer resp.Body.Close()

	om.log.Debugw("Received onboarding response", "statusCode", resp.StatusCode)

	// Parse onboarding response
	om.log.Debug("Parsing onboarding response...")
	onboardResp, err := sbi.ParsePostOnboardingDeviceResponse(resp)
	if err != nil {
		om.log.Errorw("Failed to parse onboarding response", "error", err)
		return "", fmt.Errorf("failed to parse onboarding response: %w", err)
	}

	if onboardResp.JSON200 == nil {
		om.log.Errorw("Invalid onboarding response: missing device auth data",
			"statusCode", resp.StatusCode)
		return "", fmt.Errorf("invalid onboarding response: missing device auth data")
	}

	// Store device auth information
	om.log.Debug("Storing device authentication information...")
	if err := om.database.UpsertDeviceAuth(onboardResp.JSON200); err != nil {
		om.log.Errorw("Failed to store device auth information", "error", err)
		return "", fmt.Errorf("failed to store device auth information: %w", err)
	}

	// Generate and return device ID
	deviceId := utils.GenerateDeviceId()
	om.log.Info("Device authentication information stored successfully")

	return deviceId, nil
}

func (om *oauthBasedOnboardingManager) IsOnboarded(ctx context.Context) error {
	device, err := om.database.GetDevice() // TODO: Add context support
	if err != nil {
		return fmt.Errorf("failed to check onboarding status: %w", err)
	}

	if device == nil || device.DeviceAuth == nil {
		return fmt.Errorf("device is not onboarded")
	}

	return nil
}

func (om *oauthBasedOnboardingManager) GetDeviceId(ctx context.Context) string {
	device, err := om.database.GetDevice() // TODO: Add context support
	if err != nil {
		om.log.Errorw("Failed to get device ID", "error", err)
		return ""
	}

	if device != nil && device.DeviceProperties != nil {
		return device.DeviceProperties.Id
	}

	return ""
}
