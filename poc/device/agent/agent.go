package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/margo/dev-repo/poc/device/agent/database"
	"github.com/margo/dev-repo/poc/device/agent/device"
	"github.com/margo/dev-repo/poc/device/agent/types"
	"github.com/margo/dev-repo/poc/device/agent/workload"
	workloads "github.com/margo/dev-repo/shared-lib/workloads"
	"go.uber.org/zap"
)

// DeviceAgent represents the main device agent.
type DeviceAgent struct {
	log                 *zap.SugaredLogger
	ctx                 context.Context    // Context for managing agent lifecycle
	cancelFunc          context.CancelFunc // Function to cancel the context
	config              *types.Config
	apiClientFactory    types.APIClientInterface
	database            database.AgentDatabase
	workloadManager     workload.WorkloadManager
	workloadWatcher     workload.WorkloadWatcher
	stateSyncer         device.AppStateSyncer
	capabilitiesManager device.CapabilitiesManager
	onboardingManager   device.OnboardingManager
}

// NewDeviceAgent creates a new device agent instance.
func NewDeviceAgent(
	config *types.Config,
	logger *zap.SugaredLogger,
	apiClientFactory types.APIClientInterface,
) (*DeviceAgent, error) {
	logger.Debugw("Creating new DeviceAgent instance",
		"runtimeType", config.RuntimeInfo.Type)

	if config == nil {
		logger.Error("Configuration is nil, cannot create DeviceAgent")
		return nil, fmt.Errorf("config cannot be nil")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	if apiClientFactory == nil {
		logger.Error("API client factory is nil, cannot create DeviceAgent")
		return nil, fmt.Errorf("apiClient factory cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	var database database.AgentDatabase = database.NewAgentInMemoryDatabase(ctx, "./data")
	var onboardingManager device.OnboardingManager = device.NewOAuthBasedOnboardingManager(logger, config, database, apiClientFactory)
	var stateSyncer device.AppStateSyncer = device.NewAppStateSyncer(logger, config, database, apiClientFactory)
	var capabilityManager device.CapabilitiesManager = device.NewManualCapabilitiesManager(logger, config, apiClientFactory)
	var workloadManager workload.WorkloadManager
	var workloadWatcher workload.WorkloadWatcher

	switch strings.ToLower(config.RuntimeInfo.Type) {
	case "kubernetes":
		{
			logger.Debugw("Initializing Kubernetes runtime components",
				"kubeconfigPath", config.RuntimeInfo.Kubernetes.KubeconfigPath)
			helmClient, err := workloads.NewHelmClient(config.RuntimeInfo.Kubernetes.KubeconfigPath)
			if err != nil {
				logger.Errorw("Failed to create Helm client",
					"error", err,
					"kubeconfigPath", config.RuntimeInfo.Kubernetes.KubeconfigPath)
				cancel()
				return nil, err
			}
			workloadManager = workload.NewWorkloadManager(logger, database, helmClient, nil)
			workloadWatcher = workload.NewWorkloadWatcher(logger, database, helmClient, nil)
		}

	case "docker":
		{
			logger.Debugw("Initializing Docker Compose runtime components")
			dockerComposeClient, err := workloads.NewDockerComposeClient()
			if err != nil {
				logger.Errorw("Failed to create Docker Compose client", "error", err)
				cancel()
				return nil, err
			}
			workloadManager = workload.NewWorkloadManager(logger, database, nil, dockerComposeClient)
			workloadWatcher = workload.NewWorkloadWatcher(logger, database, nil, dockerComposeClient)
		}

	default:
		logger.Errorw("Unsupported runtime type specified",
			"runtimeType", config.RuntimeInfo.Type,
			"supportedTypes", []string{"kubernetes", "docker"})
		cancel()
		return nil, fmt.Errorf("unknown runtime type: %s", config.RuntimeInfo.Type)
	}

	logger.Debugw("DeviceAgent context and components initialized",
		"runtimeType", config.RuntimeInfo.Type,
		"dataPath", "./data")

	agent := &DeviceAgent{
		config:              config,
		ctx:                 ctx,
		cancelFunc:          cancel,
		apiClientFactory:    apiClientFactory,
		database:            database,
		stateSyncer:         stateSyncer,
		workloadManager:     workloadManager,
		capabilitiesManager: capabilityManager,
		workloadWatcher:     workloadWatcher,
		onboardingManager:   onboardingManager,
		log:                 logger,
	}

	logger.Infow("DeviceAgent instance created successfully",
		"runtimeType", config.RuntimeInfo.Type,
		"components", map[string]bool{
			"onboardingManager":     onboardingManager != nil,
			"appStateSeeker/Syncer": stateSyncer != nil,
			"workloadManager":       workloadManager != nil,
			"workloadWatcher":       workloadWatcher != nil,
			"capabilitiesManager":   capabilityManager != nil,
		})

	return agent, nil
}

// Start initializes and starts the device agent.
func (da *DeviceAgent) Start() error {
	startTime := time.Now()
	da.log.Infow("Starting device agent...", "startTime", startTime)

	// Start database first
	da.log.Debugw("Starting database component", "dataPath", "./data")
	if err := da.database.Start(); err != nil {
		da.log.Errorw("Failed to start database", "error", err)
		return fmt.Errorf("failed to start database: %w", err)
	}
	da.log.Infow("Database started successfully")

	// Onboard device with retry logic
	onboardingTimeout := 30 * time.Second
	da.log.Infow("Starting device onboarding process",
		"timeout", onboardingTimeout)
	onboardingCtx, cancel := context.WithTimeout(da.ctx, onboardingTimeout)
	defer cancel()

	deviceId, err := da.onboardingManager.KeepTryingOnboardingIfFailed(onboardingCtx)
	if err != nil {
		da.log.Errorw("Device onboarding failed",
			"error", err,
			"timeout", onboardingTimeout)
		return fmt.Errorf("failed to onboard the device: %w", err)
	}
	da.log.Infow("Device onboarding completed successfully", "deviceId", deviceId)

	da.log.Debugw("Starting capabilities manager component")
	if err := da.capabilitiesManager.Start(); err != nil {
		da.log.Errorw("Failed to start capabilities manager", "error", err)
		return fmt.Errorf("failed to start device capabilities manager: %w", err)
	}
	da.log.Infow("Capabilities manager started successfully")

	// Report Device Capabilities
	da.log.Debugw("Reporting device capabilities to orchestrator")
	if err := da.capabilitiesManager.ReportCapabilities(context.Background()); err != nil {
		da.log.Errorw("Failed to report device capabilities", "error", err)
		// Note: Not returning error here as capabilities reporting is non-critical
	}

	// Start all components with context checking
	da.log.Infow("Starting core agent components")

	da.log.Debugw("Starting state syncer component")
	if err := da.stateSyncer.Start(); err != nil {
		da.log.Errorw("Failed to start state syncer", "error", err)
		return fmt.Errorf("failed to start state syncer: %w", err)
	}
	da.log.Infow("State syncer started successfully")

	da.log.Debugw("Starting workload manager component")
	if err := da.workloadManager.Start(); err != nil {
		da.log.Errorw("Failed to start workload manager", "error", err)
		return fmt.Errorf("failed to start workload manager: %w", err)
	}
	da.log.Infow("Workload manager started successfully")

	da.log.Debugw("Starting workload watcher component")
	if err := da.workloadWatcher.Start(); err != nil {
		da.log.Errorw("Failed to start workload watcher", "error", err)
		return fmt.Errorf("failed to start workload watcher: %w", err)
	}
	da.log.Infow("Workload watcher started successfully")

	startupDuration := time.Since(startTime)
	da.log.Infow("Device agent startup completed successfully",
		"deviceId", deviceId,
		"startupDuration", startupDuration,
		"runtimeType", da.config.RuntimeInfo.Type,
		"components", []string{"database", "stateSyncer", "workloadManager", "workloadWatcher"})

	return nil
}

// Stop gracefully shuts down the device agent.
func (da *DeviceAgent) Stop() error {
	shutdownStart := time.Now()
	da.log.Infow("Initiating device agent shutdown", "shutdownStart", shutdownStart)

	var shutdownErrors []error

	if da.stateSyncer != nil {
		da.log.Debugw("Stopping state syncer component")
		if err := da.stateSyncer.Stop(); err != nil {
			da.log.Errorw("Failed to stop state syncer", "error", err)
			shutdownErrors = append(shutdownErrors, fmt.Errorf("state syncer: %w", err))
		} else {
			da.log.Infow("State syncer stopped successfully")
		}
	}

	if da.workloadManager != nil {
		da.log.Debugw("Stopping workload manager component")
		if err := da.workloadManager.Stop(); err != nil {
			da.log.Errorw("Failed to stop workload manager", "error", err)
			shutdownErrors = append(shutdownErrors, fmt.Errorf("workload manager: %w", err))
		} else {
			da.log.Infow("Workload manager stopped successfully")
		}
	}

	// Stop components in reverse order of startup
	if da.workloadWatcher != nil {
		da.log.Debugw("Stopping workload watcher component")
		if err := da.workloadWatcher.Stop(); err != nil {
			da.log.Errorw("Failed to stop workload watcher", "error", err)
			shutdownErrors = append(shutdownErrors, fmt.Errorf("workload watcher: %w", err))
		} else {
			da.log.Infow("Workload watcher stopped successfully")
		}
	}

	da.log.Debugw("Stopping database component")
	if da.database != nil {
		if err := da.database.Stop(); err != nil {
			da.log.Errorw("Failed to stop database", "error", err)
			shutdownErrors = append(shutdownErrors, fmt.Errorf("database: %w", err))
		} else {
			da.log.Infow("Database stopped successfully")
		}
	}

	// Cancel context
	da.log.Debugw("Cancelling agent context and cleaning up resources")
	da.cancelFunc()

	shutdownDuration := time.Since(shutdownStart)

	// Report shutdown status
	if len(shutdownErrors) > 0 {
		da.log.Errorw("Device agent shutdown completed with errors",
			"errorCount", len(shutdownErrors),
			"shutdownDuration", shutdownDuration,
			"errors", shutdownErrors)
		return fmt.Errorf("shutdown completed with %d errors: %v", len(shutdownErrors), shutdownErrors)
	}

	da.log.Infow("Device agent shutdown completed successfully",
		"shutdownDuration", shutdownDuration)
	return nil
}
