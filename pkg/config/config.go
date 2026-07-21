package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/quaywin/agys/pkg/profile"
)

// Config represents global configuration settings for agys.
type Config struct {
	AutoFailover bool `json:"auto_failover"`
	MaxRetries   int  `json:"max_retries"`
}

var configMu sync.RWMutex

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		AutoFailover: false,
		MaxRetries:   3,
	}
}

// GetConfigPath returns the absolute path to ~/.agys/config.json.
func GetConfigPath() (string, error) {
	agysDir, err := profile.GetAgysDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(agysDir, "config.json"), nil
}

// Load reads and parses the configuration file, returning default values if absent.
func Load() (*Config, error) {
	configMu.RLock()
	defer configMu.RUnlock()

	cfgPath, err := GetConfigPath()
	if err != nil {
		return DefaultConfig(), err
	}

	data, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return DefaultConfig(), fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	return cfg, nil
}

func Save(cfg *Config) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfgPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	agysDir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(agysDir, 0700); err != nil {
		return fmt.Errorf("failed to create agys directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	return os.WriteFile(cfgPath, data, 0600)
}
