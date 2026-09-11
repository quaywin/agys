package cmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/quaywin/agys/pkg/profile"
)

func TestDoctorCmdRegistration(t *testing.T) {
	if doctorCmd.Name() != "doctor" {
		t.Errorf("expected command name 'doctor', got %q", doctorCmd.Name())
	}
	hasHealthAlias := false
	for _, a := range doctorCmd.Aliases {
		if a == "health" {
			hasHealthAlias = true
			break
		}
	}
	if !hasHealthAlias {
		t.Errorf("expected doctorCmd to have alias 'health', got aliases: %v", doctorCmd.Aliases)
	}
}

func TestRunDoctorExecution(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	// Create test profile
	_, err := profile.Create("test-prof")
	if err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}

	ctx := context.Background()
	err = runDoctor(ctx)
	if err != nil {
		t.Errorf("runDoctor returned unexpected error: %v", err)
	}
}
