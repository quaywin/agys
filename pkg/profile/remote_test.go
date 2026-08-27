package profile

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsRemoteControlRequested(t *testing.T) {
	tests := []struct {
		args     []string
		expected bool
	}{
		{[]string{"-p", "hello"}, false},
		{[]string{"--remote-control"}, true},
		{[]string{"--dangerously-skip-permissions", "--remote-control"}, true},
		{[]string{"--remote-control=true"}, true},
	}

	for _, tt := range tests {
		got := IsRemoteControlRequested(tt.args)
		if got != tt.expected {
			t.Errorf("IsRemoteControlRequested(%v) = %v, want %v", tt.args, got, tt.expected)
		}
	}
}

func TestEnsureAvailableHubPort(t *testing.T) {
	t.Run("Non-remote control args are unmodified", func(t *testing.T) {
		args := []string{"-p", "hello"}
		res, port, err := EnsureAvailableHubPort(args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port != 0 || len(res) != len(args) {
			t.Errorf("expected unmodified args, got %v with port %d", res, port)
		}
	})

	t.Run("Default port 4400 when available", func(t *testing.T) {
		if !IsPortAvailable(DefaultHubPort) {
			t.Skip("Port 4400 in use on test machine, skipping default port test")
		}
		args := []string{"--remote-control"}
		res, port, err := EnsureAvailableHubPort(args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port != DefaultHubPort {
			t.Errorf("expected port %d, got %d", DefaultHubPort, port)
		}
		if len(res) != 1 || res[0] != "--remote-control" {
			t.Errorf("expected original args when port is available, got %v", res)
		}
	})

	t.Run("Auto-allocates next port when port is in use", func(t *testing.T) {
		// Occupy a port temporarily
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to bind test listener: %v", err)
		}
		defer ln.Close()

		occupiedPort := ln.Addr().(*net.TCPAddr).Port

		args := []string{"--remote-control", "--hub-port", fmt.Sprintf("%d", occupiedPort)}
		res, newPort, err := EnsureAvailableHubPort(args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newPort == occupiedPort {
			t.Errorf("expected different port from occupied port %d, got %d", occupiedPort, newPort)
		}

		// Verify --hub-port in res was updated
		found := false
		for i, arg := range res {
			if arg == "--hub-port" && i+1 < len(res) && res[i+1] == fmt.Sprintf("%d", newPort) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected --hub-port %d in args, got %v", newPort, res)
		}
	})
}

func TestEnsureOnboardingCompleted(t *testing.T) {
	tempDir := t.TempDir()

	err := EnsureOnboardingCompleted(tempDir)
	if err != nil {
		t.Fatalf("EnsureOnboardingCompleted failed: %v", err)
	}

	stateFile := filepath.Join(tempDir, ".gemini", "antigravity-cli", "antigravity_state.pbtxt")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	if !strings.Contains(string(data), "AGENT_ONBOARDING_STATE_COMPLETED") {
		t.Errorf("expected AGENT_ONBOARDING_STATE_COMPLETED in state file, got: %s", string(data))
	}
}

func TestSyncAllTokenLocations(t *testing.T) {
	tempDir := t.TempDir()

	// Write token in only one location
	cliTokenPath := filepath.Join(tempDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	_ = os.MkdirAll(filepath.Dir(cliTokenPath), 0700)
	dummyToken := `{"token":{"access_token":"test-token","refresh_token":"test-refresh"}}`
	_ = os.WriteFile(cliTokenPath, []byte(dummyToken), 0600)

	// Run sync
	err := SyncAllTokenLocations(tempDir)
	if err != nil {
		t.Fatalf("SyncAllTokenLocations failed: %v", err)
	}

	// Verify all target files are created with token content
	checkPaths := []string{
		filepath.Join(tempDir, ".gemini", "antigravity", "antigravity-oauth-token"),
		filepath.Join(tempDir, ".gemini", "jetski-standalone-oauth-token"),
		filepath.Join(tempDir, ".gemini", "oauth_creds.json"),
	}

	for _, p := range checkPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("expected file %s to exist: %v", p, err)
		}
		if !strings.Contains(string(data), "test-token") {
			t.Errorf("expected token in %s, got: %s", p, string(data))
		}
	}
}

func TestRemoteDaemonMetadata(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profileName := "remote-test"
	profileDir, err := Create(profileName)
	if err != nil {
		t.Fatalf("failed to create test profile: %v", err)
	}

	// 1. Initially no daemon running
	info, running, err := GetRemoteDaemonInfo(profileName)
	if err != nil {
		t.Fatalf("GetRemoteDaemonInfo failed: %v", err)
	}
	if running || info != nil {
		t.Errorf("expected no daemon running, got %v", info)
	}

	// 2. Save daemon metadata
	testInfo := &RemoteDaemonInfo{
		Profile: profileName,
		PID:     os.Getpid(), // current process is guaranteed alive
		Port:    4400,
		Name:    "test-machine",
	}
	err = SaveRemoteDaemonInfo(profileDir, testInfo)
	if err != nil {
		t.Fatalf("SaveRemoteDaemonInfo failed: %v", err)
	}

	// 3. Read back active info
	info, running, err = GetRemoteDaemonInfo(profileName)
	if err != nil {
		t.Fatalf("GetRemoteDaemonInfo failed: %v", err)
	}
	if !running || info == nil {
		t.Fatalf("expected running daemon info, got nil")
	}
	if info.Profile != profileName || info.Port != 4400 || info.Name != "test-machine" {
		t.Errorf("unexpected daemon info: %+v", info)
	}

	// 4. List running daemons
	list, err := ListRunningRemoteDaemons()
	if err != nil {
		t.Fatalf("ListRunningRemoteDaemons failed: %v", err)
	}
	if len(list) != 1 || list[0].Profile != profileName {
		t.Errorf("expected 1 running daemon, got %v", list)
	}

	// 5. Remove daemon info
	err = RemoveRemoteDaemonInfo(profileDir)
	if err != nil {
		t.Fatalf("RemoveRemoteDaemonInfo failed: %v", err)
	}

	info, running, err = GetRemoteDaemonInfo(profileName)
	if err != nil {
		t.Fatalf("GetRemoteDaemonInfo failed: %v", err)
	}
	if running || info != nil {
		t.Errorf("expected no daemon running after removal, got %v", info)
	}
}
