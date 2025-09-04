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
	auth           *DeviceSettings
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

	opts := []Option{}
	var helmClient *workloads.HelmClient
	var composeClient *workloads.DockerComposeCliClient
	for _, runtime := range cfg.Runtimes {
		if runtime.Kubernetes != nil {
			// Create Helm client
			helmClient, err = workloads.NewHelmClient(runtime.Kubernetes.KubeconfigPath)
			if err != nil {
				return nil, err
			}
			opts = append(opts, WithEnableHelmDeployment())
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
			opts = append(opts, WithEnableComposeDeployment())
		}
	}

	opts = append(opts, WithDeviceSignature(findDeviceSignature(*cfg, log)))

	var deviceSettings *DeviceSettings
	deviceSettings, _ = NewDeviceSettings(apiClient, db, log, opts...)
	isOnboarded, err := deviceSettings.IsOnboarded()
	if err != nil {
		log.Errorw("failed to check onboarding status", "error", err)
		return nil, err
	}

	if !isOnboarded {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		deviceId, err := deviceSettings.OnboardWithRetries(ctx, 10)
		defer cancel()
		if err != nil {
			log.Errorw("device onboarding failed", "error", err)
		}
		log.Infow("device onboarded", "deviceId", deviceId)
	}

	log.Infow("device details",
		"deviceId", deviceSettings.deviceID,
		"deviceSignature", string(deviceSettings.deviceSignature),
		"canDeployHelm", deviceSettings.canDeployHelm,
		"canDeployCompose", deviceSettings.canDeployCompose,
		"isAuthEnabled", deviceSettings.authEnabled,
		"hasClientId", len(deviceSettings.oauthClientId) != 0,
		"hasClientSecret", len(deviceSettings.oAuthClientSecret) != 0,
		"hasTokenUrl", len(deviceSettings.oauthTokenUrl) != 0,
		"tokenBasedAuthDetails", (len(deviceSettings.oauthClientId) != 0) && (len(deviceSettings.oAuthClientSecret) != 0) && (len(deviceSettings.oauthTokenUrl) != 0),
	)

	// Create components
	deployer := NewDeploymentManager(db, helmClient, composeClient, log)
	monitor := NewDeploymentMonitor(db, helmClient, composeClient, log)
	syncer := NewStateSyncer(db, apiClient, deviceSettings.deviceID, cfg.StateSeeking.Interval, log)
	statusReporter := NewStatusReporter(db, apiClient, deviceSettings.deviceID, log)

	return &Agent{
		database:       db,
		syncer:         syncer,
		deployer:       deployer,
		monitor:        monitor,
		auth:           deviceSettings,
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
	capabilities, err := types.LoadCapabilities(a.config.Capabilities.ReadFromFile)
	if err != nil {
		a.log.Errorw(
			"failed to load the capabilities file, please resolve the issue as the capabilities will not be reported until next restart",
			"err",
			err.Error(),
		)
	} else {
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

func findDeviceSignature(cfg types.Config, logger *zap.SugaredLogger) []byte {
	sign := cfg.DeviceSignature
	logger.Infow("find device signature", "signature", sign)
	return []byte(sign)
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
