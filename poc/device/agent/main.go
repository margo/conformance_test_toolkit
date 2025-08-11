package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	var configFile = flag.String("config", "config/config.yaml", "Path to the configuration file")
	flag.Parse()

	// Initialize the logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()
	sugar := logger.Sugar()

	// Create ConfigManager
	configManager := NewConfigManager(*configFile)

	// Load Config
	config, err := configManager.LoadAndValidateConfig()
	if err != nil {
		sugar.Fatalf("Failed to load/validate config: %v", err)
	}

	// Create APIClientFactory
	sbiClient := &apiClient{
		SBIUrl: config.WfmSbiUrl,
	}

	// Create DeviceAgent with context
	agent, err := NewDeviceAgent(config, sugar, sbiClient)
	if err != nil {
		sugar.Fatalf("Failed to create device agent: %v", err)
	}

	go func() {
		// Start the agent
		if err := agent.Start(); err != nil {
			sugar.Fatalf("Failed to start device agent: %v", err)
		}
		sugar.Info("Device agent started successfully")
	}()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	sugar.Infow("Received shutdown signal, initiating graceful shutdown", "signal", sig)

	// Give the agent time to shutdown gracefully
	shutdownTimer := time.NewTimer(30 * time.Second)
	defer shutdownTimer.Stop()

	done := make(chan error, 1)
	go func() {
		done <- agent.Stop()
	}()

	select {
	case err := <-done:
		if err != nil {
			sugar.Errorw("Error during shutdown", "error", err)
		} else {
			sugar.Info("Graceful shutdown completed successfully")
		}
	case <-shutdownTimer.C:
		sugar.Warn("Shutdown timeout exceeded, forcing exit")
	}
}
