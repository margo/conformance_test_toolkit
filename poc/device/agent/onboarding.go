// auth/onboarding.go
package main

import (
	"context"
	"fmt"

	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"go.uber.org/zap"
)

type DeviceAuth struct {
	deviceID  string
	apiClient sbi.ClientInterface
	log       *zap.SugaredLogger
}

func NewDeviceAuth(deviceID string, client sbi.ClientInterface, log *zap.SugaredLogger) *DeviceAuth {
	return &DeviceAuth{
		deviceID:  deviceID,
		apiClient: client,
		log:       log,
	}
}

func (da *DeviceAuth) Onboard(ctx context.Context) error {
	da.log.Infow("Starting device onboarding", "deviceId", da.deviceID)

	onboardingReq := sbi.OnboardingRequest{
		"ApiVersion": "device.margo/v1",
		"Kind":       "OnboardingRequest",
		"DeviceId":   da.deviceID,
	}

	resp, err := da.apiClient.PostOnboardingDevice(ctx, onboardingReq)
	if err != nil {
		return fmt.Errorf("onboarding failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("onboarding failed with status: %d", resp.StatusCode)
	}

	da.log.Infow("Device onboarding successful", "deviceId", da.deviceID)
	return nil
}

func (da *DeviceAuth) ReportCapabilities(ctx context.Context, capabilities sbi.DeviceCapabilities) error {
	resp, err := da.apiClient.PostDeviceDeviceIdCapabilities(ctx, da.deviceID, capabilities)
	if err != nil {
		return fmt.Errorf("failed to report capabilities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("capabilities reporting failed with status: %d", resp.StatusCode)
	}

	da.log.Info("Capabilities reported successfully")
	return nil
}
