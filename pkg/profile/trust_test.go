package profile

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSyncTrustedWorkspaces(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	p1Dir, err := Create("profile-1")
	if err != nil {
		t.Fatalf("Create profile-1 error: %v", err)
	}
	p2Dir, err := Create("profile-2")
	if err != nil {
		t.Fatalf("Create profile-2 error: %v", err)
	}

	p1Settings := filepath.Join(p1Dir, ".gemini", "antigravity-cli", "settings.json")
	p2Settings := filepath.Join(p2Dir, ".gemini", "antigravity-cli", "settings.json")

	_ = updateSettingsTrustedWorkspaces(p1Settings, []string{"/path/project-A", "/path/project-B"})
	_ = updateSettingsTrustedWorkspaces(p2Settings, []string{"/path/project-B", "/path/project-C"})

	err = SyncTrustedWorkspaces()
	if err != nil {
		t.Fatalf("SyncTrustedWorkspaces error: %v", err)
	}

	trustedMap1 := make(map[string]bool)
	readSettingsWorkspaces(p1Settings, trustedMap1)

	trustedMap2 := make(map[string]bool)
	readSettingsWorkspaces(p2Settings, trustedMap2)

	expected := map[string]bool{
		"/path/project-A": true,
		"/path/project-B": true,
		"/path/project-C": true,
	}

	if !reflect.DeepEqual(trustedMap1, expected) {
		t.Errorf("Expected profile-1 trusted workspaces %v, got %v", expected, trustedMap1)
	}
	if !reflect.DeepEqual(trustedMap2, expected) {
		t.Errorf("Expected profile-2 trusted workspaces %v, got %v", expected, trustedMap2)
	}
}
