// auth/onboarding.go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"go.uber.org/zap"
)

type DeviceSettings struct {
	deviceID string
	// a temporary solution to simulate oem based device, later on once the onboarding story is clear
	// we can go ahead and implement something better over here
	deviceSignature                                 []byte
	authEnabled                                     bool
	oauthClientId, oAuthClientSecret, oauthTokenUrl string
	log                                             *zap.SugaredLogger
	apiClient                                       sbi.ClientInterface
	db                                              database.DatabaseIfc
	canDeployHelm, canDeployCompose                 bool
}

type Option = func(auth *DeviceSettings)

func WithDeviceID(deviceID string) Option {
	return func(auth *DeviceSettings) {
		auth.deviceID = deviceID
	}
}

func WithDeviceSignature(sign []byte) Option {
	return func(auth *DeviceSettings) {
		auth.deviceSignature = sign
	}
}

func WithEnableAuth(clientId, clientSecret, tokenUrl string) Option {
	return func(auth *DeviceSettings) {
		auth.authEnabled = true
		auth.oauthClientId = clientId
		auth.oAuthClientSecret = clientSecret
		auth.oauthTokenUrl = tokenUrl
	}
}

func WithEnableComposeDeployment() Option {
	return func(auth *DeviceSettings) {
		auth.canDeployCompose = true
	}
}

func WithEnableHelmDeployment() Option {
	return func(auth *DeviceSettings) {
		auth.canDeployHelm = true
	}
}

func NewDeviceSettings(client sbi.ClientInterface, db database.DatabaseIfc, log *zap.SugaredLogger, opts ...Option) (*DeviceSettings, error) {
	s, _ := db.GetDeviceSettings()

	deviceId, signature := "", ""
	authEnabled, clientId, clientSecret, tokenUrl := false, "", "", ""
	if s != nil {
		deviceId = s.DeviceID
		signature = string(s.DeviceSignature)
		authEnabled = s.AuthEnabled
		clientId = s.OAuthClientId
		clientSecret = s.OAuthClientSecret
		tokenUrl = s.OAuthTokenEndpointUrl
	}

	settings := &DeviceSettings{
		deviceID:          deviceId,
		deviceSignature:   []byte(signature),
		apiClient:         client,
		log:               log,
		db:                db,
		authEnabled:       authEnabled,
		oauthClientId:     clientId,
		oAuthClientSecret: clientSecret,
		oauthTokenUrl:     tokenUrl,
	}

	for _, opt := range opts {
		opt(settings)
	}
	return settings, nil
}

func (da *DeviceSettings) Onboard(ctx context.Context) (deviceId string, err error) {
	deviceSign := da.deviceSignature
	da.log.Infow("Starting device onboarding", "hasValidDeviceSignature", len(deviceSign) != 0)

	onboardingReq := sbi.OnboardingRequest{
		"ApiVersion":      "device.margo/v1",
		"Kind":            "OnboardingRequest",
		"DeviceSignature": string(deviceSign),
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
	da.oauthClientId = onboardingResp.JSON200.ClientId
	da.oAuthClientSecret = onboardingResp.JSON200.ClientSecret
	da.oauthTokenUrl = onboardingResp.JSON200.TokenEndpointUrl
	da.log.Infow("Device onboarding successful", "deviceId", da.deviceID)

	da.db.SetDeviceSettings(database.DeviceSettingsRecord{
		DeviceID:              deviceId,
		DeviceSignature:       da.deviceSignature,
		State:                 database.DeviceOnboardStateOnboarded,
		OAuthClientId:         da.oauthClientId,
		OAuthClientSecret:     da.oAuthClientSecret,
		OAuthTokenEndpointUrl: da.oauthTokenUrl,
		AuthEnabled:           da.authEnabled,
	})

	return da.deviceID, nil
}

func (da *DeviceSettings) OnboardWithRetries(ctx context.Context, retries uint8) (deviceId string, err error) {
	totalRetries := retries
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if retries == 0 {
			break
		}
		retries--

		// Wait for next tick or overall timeout
		<-ticker.C

		deviceId, err := da.Onboard(ctx)
		if err != nil {
			da.log.Infow("onboard operation failed", "tryCount", totalRetries-retries, "totalRetriesAllowed", totalRetries, "err", err.Error())
			continue
		}
		return deviceId, err
	}

	return "", fmt.Errorf("unable to onboard the device")
}

func (da *DeviceSettings) ReportCapabilities(ctx context.Context, capabilities sbi.DeviceCapabilities) error {
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

func (da *DeviceSettings) IsOnboarded() (bool, error) {
	_, isOnboarded, err := da.db.IsDeviceOnboarded()
	return isOnboarded, err
}
