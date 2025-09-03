// auth/onboarding.go
package main

import (
	"context"
	"fmt"

	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"go.uber.org/zap"
)

type DeviceAuth struct {
	deviceID string
	// a temporary solution to simulate oem based device, later on once the onboarding story is clear
	// we can go ahead and implement something better over here
	deviceSignature                  []byte
	clientId, clientSecret, tokenUrl string
	apiClient                        sbi.ClientInterface
	log                              *zap.SugaredLogger
}

type Option = func(auth *DeviceAuth)

func WithDeviceID(deviceID string) Option {
	return func(auth *DeviceAuth) {
		auth.deviceID = deviceID
	}
}

func WithDeviceSignature(sign []byte) Option {
	return func(auth *DeviceAuth) {
		auth.deviceSignature = sign
	}
}

func WithDeviceClientSecret(clientId, clientSecret, tokenUrl string) Option {
	return func(auth *DeviceAuth) {
		auth.clientId = clientId
		auth.clientSecret = clientSecret
		auth.tokenUrl = tokenUrl
	}
}

func NewDeviceAuth(client sbi.ClientInterface, log *zap.SugaredLogger, opts ...Option) *DeviceAuth {
	deviceAuth := &DeviceAuth{
		deviceID:        "",
		deviceSignature: []byte(""),
		apiClient:       client,
		log:             log,
	}

	for _, opt := range opts {
		opt(deviceAuth)
	}
	return deviceAuth
}

func (da *DeviceAuth) Onboard(ctx context.Context, deviceSign []byte) (deviceId string, err error) {
	da.log.Infow("Starting device onboarding", "hasValidDeviceSignature", len(deviceSign) != 0)

	onboardingReq := sbi.OnboardingRequest{
		"ApiVersion":      "device.margo/v1",
		"Kind":            "OnboardingRequest",
		"DeviceSignature": deviceSign,
	}

	resp, err := da.apiClient.PostOnboardingDevice(ctx, onboardingReq)
	if err != nil {
		return "", fmt.Errorf("onboarding failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("onboarding failed with status: %d", resp.StatusCode)
	}

	onboardingResp, err := sbi.ParsePostOnboardingDeviceResponse(resp)
	if err != nil {
		return "", fmt.Errorf("onboarding device response parsing failed: %w", err)
	}

	if onboardingResp.JSON200.ClientId == "" ||
		onboardingResp.JSON200.ClientSecret == "" ||
		onboardingResp.JSON200.TokenEndpointUrl == "" {
		return "", fmt.Errorf("one of the clientid, secret or tokenendpoint url is missing from the onboarding response")
	}

	da.deviceID = onboardingResp.JSON200.ClientId
	da.deviceSignature = deviceSign
	da.clientId = onboardingResp.JSON200.ClientId
	da.clientSecret = onboardingResp.JSON200.ClientSecret
	da.tokenUrl = onboardingResp.JSON200.TokenEndpointUrl
	da.log.Infow("Device onboarding successful", "deviceId", da.deviceID)
	return da.deviceID, nil
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
