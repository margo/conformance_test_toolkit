package types

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-playground/validator/v10"
	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
	"gopkg.in/yaml.v2"
)

// Config struct
type Config struct {
	DeviceID     string                      `yaml:"deviceId" validate:"required"`
	Wfm          WFMConfig                   `yaml:"wfm" validate:"required"`
	StateSeeking StateSeekingConfig          `yaml:"stateSeeking" validate:"required"`
	Capabilities CapabilitiesDiscoveryConfig `yaml:"capabilities" validate:"required"`
	Runtimes     []RuntimeInfo               `yaml:"runtimes"`
}

type StateSeekingConfig struct {
	Interval uint16 `yaml:"interval" validate:"required"`
}

type WFMConfig struct {
	SbiURL string `yaml:"sbiUrl" validate:"required,url"`
	// certificates if needed
	// Certificates...
	Auth AuthConfig `yaml:"auth"`
}

type CapabilitiesDiscoveryConfig struct {
	ReadFromFile string `yaml:"readFromFile" validate:"required"`
}

type LoggingConfig struct {
	Level string `yaml:"level" validate:"required"`
}

type KubernetesConfig struct {
	KubeconfigPath string `yaml:"kubeconfigPath"`
}

type TLSConfig struct {
	CacertPath *string `yaml:"cacertPath"`
	CertPath   *string `yaml:"certPath"`
	KeyPath    *string `yaml:"keyPath"`
}

type DockerConfig struct {
	Url                 string     `yaml:"url" validator:"url"`
	TLS                 *TLSConfig `yaml:"tls"`
	TLSSkipVerification *bool      `yaml:"tlsSkipVerification"`
}

type RuntimeInfo struct {
	Type       string            `yaml:"type"`
	Kubernetes *KubernetesConfig `yaml:"kubernetes,omitempty"`
	Docker     *DockerConfig     `yaml:"docker,omitempty"`
}

type AuthConfig struct {
	ClientId     string `yaml:"clientId"`
	ClientSecret string `yaml:"clientSecret"`
	TokenUrl     string `yaml:"tokenUrl"`
}

func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	// fmt.Println("read config", string(data))

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// fmt.Println("parsed config", pretty.Sprint(config))
	return &config, validateConfig(&config)
}

func LoadCapabilities(capabilitiesPath string) (*sbi.DeviceCapabilities, error) {
	data, err := os.ReadFile(capabilitiesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read capabilities file: %w", err)
	}

	var capabilities sbi.DeviceCapabilities
	if err := json.Unmarshal(data, &capabilities); err != nil {
		return nil, fmt.Errorf("failed to parse capabilities: %w", err)
	}

	return &capabilities, nil
}

// validateConfig validates the configuration
func validateConfig(config *Config) error {
	err := validator.New().Struct(config)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	if len(config.Runtimes) == 0 {
		return fmt.Errorf("there are no runtimes defined in agent configuration")
	}

	return nil
}
