package profile

import (
	"path/filepath"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"valid-name", false},
		{"valid_name_123", false},
		{"work", false},
		{"", true},
		{"invalid name", true},
		{"invalid/slash", true},
		{"invalid@symbol", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestProfileLifecycle(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	profileName := "test-profile"

	// Validate non-existent initially
	exists, dir, err := Exists(profileName)
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Fatalf("Expected profile to not exist")
	}

	expectedDir := filepath.Join(tempHome, ".antigravity-profiles", profileName)
	if dir != expectedDir {
		t.Errorf("Expected dir %s, got %s", expectedDir, dir)
	}

	// Create profile
	createdDir, err := Create(profileName)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if createdDir != expectedDir {
		t.Errorf("Expected created dir %s, got %s", expectedDir, createdDir)
	}

	// List profiles
	profiles, err := List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(profiles) != 1 || profiles[0] != profileName {
		t.Errorf("Expected profiles [%s], got %v", profileName, profiles)
	}

	// Delete profile
	err = Delete(profileName)
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// Re-check existence
	exists, _, err = Exists(profileName)
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("Expected profile to be deleted")
	}
}
