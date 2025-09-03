// agent.go - Updated version
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/poc/device/agent/types"
	"github.com/margo/dev-repo/shared-lib/workloads"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"go.uber.org/zap"
)

type Agent struct {
	log            *zap.SugaredLogger
	auth           *DeviceAuth
	config         types.Config
	database       database.DatabaseIfc
	syncer         StateSyncerIfc
	deployer       DeploymentManagerIfc
	monitor        DeploymentMonitorIfc
	statusReporter StatusReporterIfc
}

func NewAgent(configPath string) (*Agent, error) {
	logger, _ := zap.NewDevelopment()
	log := logger.Sugar()

	// Load configuration
	cfg, err := types.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	// Create database
	db := database.NewDatabase("data/")

	// Create API client
	apiClient, err := sbi.NewClient(cfg.Wfm.SbiURL)
	if err != nil {
		return nil, err
	}

	var helmClient *workloads.HelmClient
	var composeClient *workloads.DockerComposeCliClient
	for _, runtime := range cfg.Runtimes {
		if runtime.Kubernetes != nil {
			// Create Helm client
			helmClient, err = workloads.NewHelmClient(runtime.Kubernetes.KubeconfigPath)
			if err != nil {
				return nil, err
			}
		}

		if runtime.Docker != nil {
			// Create docker compose client
			composeClient, err = workloads.NewDockerComposeCliClient(workloads.DockerConnectivityParams{
				ViaSocket: &workloads.DockerConnectionViaSocket{
					SocketPath: runtime.Docker.Url,
				},
			}, "data/composeFiles")
			if err != nil {
				return nil, err
			}
		}
	}

	var deviceAuth *DeviceAuth
	settings, isOnboarded, err := db.IsDeviceOnboarded()
	if err != nil {
		return nil, err
	}
	if !isOnboarded {
		deviceAuth = NewDeviceAuth(apiClient, log)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		deviceId, err := deviceAuth.Onboard(ctx, findDeviceSignature(log))
		defer cancel()
		if err != nil {
			return nil, err
		}
		log.Infow("device onboarded", "deviceId", deviceId)

		db.SetDeviceSettings(database.DeviceSettingsRecord{
			DeviceID:         deviceId,
			DeviceSignature:  deviceAuth.deviceSignature,
			State:            database.DeviceOnboardStateOnboarded,
			ClientId:         deviceAuth.clientId,
			ClientSecret:     deviceAuth.clientSecret,
			TokenEndpointUrl: deviceAuth.tokenUrl,
		})
	} else {
		deviceAuth = NewDeviceAuth(
			apiClient,
			log,
			WithDeviceID(settings.DeviceID),
			WithDeviceSignature(settings.DeviceSignature),
			WithDeviceClientSecret(settings.ClientId, settings.ClientSecret, settings.TokenEndpointUrl),
		)
	}

	log.Infow("device details",
		"deviceId", deviceAuth.deviceID,
		"deviceSignature", string(deviceAuth.deviceSignature),
		"hasClientId", len(deviceAuth.clientId) != 0,
		"hasClientSecret", len(deviceAuth.clientSecret) != 0,
		"hasTokenUrl", len(deviceAuth.tokenUrl) != 0,
		"tokenBasedAuthDetails", (len(deviceAuth.clientId) != 0) && (len(deviceAuth.clientSecret) != 0) && (len(deviceAuth.tokenUrl) != 0),
	)

	// Create components
	deployer := NewDeploymentManager(db, helmClient, composeClient, log)
	monitor := NewDeploymentMonitor(db, helmClient, composeClient, log)
	syncer := NewStateSyncer(db, apiClient, deviceAuth.deviceID, cfg.StateSeeking.Interval, log)
	statusReporter := NewStatusReporter(db, apiClient, deviceAuth.deviceID, log)

	return &Agent{
		database:       db,
		syncer:         syncer,
		deployer:       deployer,
		monitor:        monitor,
		auth:           deviceAuth,
		statusReporter: statusReporter,
		log:            log,
		config:         *cfg,
	}, nil
}

func (a *Agent) Start() error {
	a.log.Info("Starting Agent")

	var deviceId string
	var err error

	// 1. Onboard device
	deviceDetails, _ := a.database.GetDeviceSettings()
	deviceId = deviceDetails.DeviceID

	// 2. Report capabilities
	capabilities, err := types.LoadCapabilities("config/capabilities.json")
	if err == nil {
		capabilities.Properties.Id = deviceId
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		a.auth.ReportCapabilities(ctx, *capabilities)
		cancel()
	}

	// 3. Start all components
	a.statusReporter.Start()
	a.deployer.Start()
	a.monitor.Start()
	a.syncer.Start()

	a.log.Infow("Agent started successfully",
		"capabilitiesFile", a.config.Capabilities.ReadFromFile,
		"hasDeviceSignature", a.config.DeviceSignature != "",
		"stateSeekingInterval", a.config.StateSeeking.Interval,
		"sbiUrl", a.config.Wfm.SbiURL,
	)
	return nil
}

func (a *Agent) Stop() error {
	a.log.Info("Stopping Agent")

	a.syncer.Stop()
	a.deployer.Stop()
	a.monitor.Stop()
	a.statusReporter.Stop()
	a.database.TriggerDataPersist()

	a.log.Info("Agent stopped")
	return nil
}

func main() {
	// Define command-line flags
	configPath := flag.String(
		"config",
		"poc/device/agent/config/config.yaml", // default value
		"Path to the YAML configuration file for the Margo device agent",
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nMargo Device Agent\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if configPath == nil {
		log.Fatal("--config is mandatory command line argument")
	}

	agent, err := NewAgent(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	if err := agent.Start(); err != nil {
		log.Fatal(err)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	agent.Stop()
}

func findDeviceSignature(logger *zap.SugaredLogger) []byte {
	sign := "test-device-signature"
	logger.Infow("find device signature", "signature", sign)
	return []byte(sign)
}
