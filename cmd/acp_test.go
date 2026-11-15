package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcpCommandFlags(t *testing.T) {
	if acpCmd.Use != "acp [profile_name] [flags]" {
		t.Errorf("Expected acpCmd.Use to be 'acp [profile_name] [flags]', got %s", acpCmd.Use)
	}
	if !acpCmd.DisableFlagParsing {
		t.Errorf("Expected acpCmd.DisableFlagParsing to be true")
	}
}

func TestAcpCommand_HelpFlags(t *testing.T) {
	var buf bytes.Buffer
	acpCmd.SetOut(&buf)
	acpCmd.SetErr(&buf)

	if err := acpCmd.RunE(acpCmd, []string{"-h"}); err != nil {
		t.Errorf("Expected -h to return nil, got %v", err)
	}
	if err := acpCmd.RunE(acpCmd, []string{"--help"}); err != nil {
		t.Errorf("Expected --help to return nil, got %v", err)
	}
}

func TestAcpCommand_NonExistentProfile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	err := acpCmd.RunE(acpCmd, []string{"nonexistent-test-profile"})
	if err == nil {
		t.Fatalf("Expected error for non-existent profile, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected error to mention 'does not exist', got: %v", err)
	}
}

func TestAcpCommand_NoProfilesAutoFallback(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	// With no profiles, attempting auto selection should return an error
	err := acpCmd.RunE(acpCmd, []string{"auto"})
	if err == nil {
		t.Fatalf("Expected error when no profiles exist for auto selection, got nil")
	}
	if !strings.Contains(err.Error(), "no profiles found") && !strings.Contains(err.Error(), "auto profile selection failed") {
		t.Errorf("Expected error to mention no profiles found, got: %v", err)
	}
}
