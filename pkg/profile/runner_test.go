package profile

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRunCmdWithSignals_ContextCancel(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profileName := "test-runner-profile"
	profileDir, err := Create(profileName)
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	// Run command with mock profile directory
	// Note: 'agy' might fail to find binary, but context cancellation logic should execute
	_ = RunCmdWithSignals(ctx, profileDir, "version")
	duration := time.Since(start)

	if duration > 3*time.Second {
		t.Errorf("Expected command execution to cancel within 3s, took %v", duration)
	}
}
