package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quaywin/agys/pkg/profile"
)

func TestAddCmd_Registration(t *testing.T) {
	if addCmd.Name() != "add" {
		t.Errorf("expected command name 'add', got %q", addCmd.Name())
	}
	if !strings.Contains(addCmd.Short, "authenticate") {
		t.Errorf("expected Short description to mention authenticate, got %q", addCmd.Short)
	}
}

func TestAddCmd_InvalidArgs(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	// Case 1: missing profile name
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"add"})
	if err := rootCmd.Execute(); err == nil {
		t.Errorf("expected error for missing profile argument")
	}

	// Case 2: reserved profile name "auto"
	rootCmd.SetArgs([]string{"add", "auto"})
	if err := rootCmd.Execute(); err == nil {
		t.Errorf("expected error when trying to add reserved name 'auto'")
	}

	// Case 3: invalid characters
	rootCmd.SetArgs([]string{"add", "invalid profile!"})
	if err := rootCmd.Execute(); err == nil {
		t.Errorf("expected error for profile name with spaces and special chars")
	}
}

func TestAddCmd_ExistingAuthenticatedProfile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profName := "existing-auth-profile"
	pDir, err := profile.Create(profName)
	if err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}

	// Plant mock token so profile is considered authenticated
	tokPath := filepath.Join(pDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	_ = os.MkdirAll(filepath.Dir(tokPath), 0700)
	_ = os.WriteFile(tokPath, []byte(`{"token":{"access_token":"mock-token"}}`), 0600)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"add", profName})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected error adding profile that already exists and has a token")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got %v", err)
	}
}

func TestAddCmd_ContextCancelCleanup(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled context

	addCmd.SetContext(ctx)
	defer addCmd.SetContext(context.Background())

	err := addCmd.RunE(addCmd, []string{"cancel-prof"})
	if err == nil {
		t.Errorf("expected error on canceled context")
	}

	// Profile directory should be cleaned up on failure when no token exists
	pDir, _ := profile.GetProfileDir("cancel-prof")
	if _, statErr := os.Stat(pDir); !os.IsNotExist(statErr) {
		t.Errorf("expected incomplete profile directory %s to be cleaned up", pDir)
	}
}

func TestAddCmd_ExistingUnauthenticatedProfile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profName := "existing-unauth"
	pDir, err := profile.Create(profName)
	if err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}

	// Cancel context so it doesn't hang launching agy, but verify it recognizes existing unauthenticated profile
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	addCmd.SetContext(ctx)
	defer addCmd.SetContext(context.Background())

	err = addCmd.RunE(addCmd, []string{profName})
	if err == nil {
		t.Errorf("expected error on timeout")
	}
	// For pre-existing profile, it should NOT delete the directory
	if _, statErr := os.Stat(pDir); os.IsNotExist(statErr) {
		t.Errorf("expected pre-existing unauthenticated profile directory to be preserved, but it was removed")
	}
}
