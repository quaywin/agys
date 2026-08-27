package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestHerdrCommands(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	t.Setenv("HERDR_CONFIG_FILE", configPath)
	t.Setenv("HERDR_CONFIG_PATH", configPath)

	// 1. herdr status before configure
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"herdr", "status"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("herdr status failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Default 1-Row") {
		t.Errorf("expected output to mention Default 1-Row, got: %s", buf.String())
	}

	// 2. herdr configure
	buf.Reset()
	rootCmd.SetArgs([]string{"herdr", "configure"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("herdr configure failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully installed") {
		t.Errorf("expected success message, got: %s", buf.String())
	}

	// 3. herdr status after configure
	buf.Reset()
	rootCmd.SetArgs([]string{"herdr", "status"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("herdr status failed: %v", err)
	}
	if !strings.Contains(buf.String(), "2-Row Compact (Configured ✓)") {
		t.Errorf("expected Configured message, got: %s", buf.String())
	}

	// 4. herdr uninstall
	buf.Reset()
	rootCmd.SetArgs([]string{"herdr", "uninstall"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("herdr uninstall failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully restored") {
		t.Errorf("expected restored message, got: %s", buf.String())
	}
}
