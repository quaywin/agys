package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.AutoFailover != false {
		t.Errorf("expected default AutoFailover to be false, got %v", cfg.AutoFailover)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected default MaxRetries to be 3, got %d", cfg.MaxRetries)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)

	cfg := &Config{
		AutoFailover: true,
		MaxRetries:   5,
	}

	err := Save(cfg)
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	cfgPath, err := GetConfigPath()
	if err != nil {
		t.Fatalf("failed to get config path: %v", err)
	}

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Fatalf("config file does not exist at %s", cfgPath)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.AutoFailover != true {
		t.Errorf("expected AutoFailover true, got %v", loaded.AutoFailover)
	}
	if loaded.MaxRetries != 5 {
		t.Errorf("expected MaxRetries 5, got %d", loaded.MaxRetries)
	}
}
