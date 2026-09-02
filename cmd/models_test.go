package cmd

import (
	"bytes"
	"testing"

	"github.com/quaywin/agys/pkg/profile"
)

func TestModelsCommand(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGYS_DIR", tempDir)

	// Populate cache with mock models
	dm := &profile.DiscoveredModels{
		LatestFlash: "gemini-3.8-flash",
		LatestPro:   "gemini-3.1-pro",
		AllModels:   []string{"gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.1-pro"},
	}
	_ = profile.SaveCachedDiscoveredModels(dm)

	var buf bytes.Buffer
	modelsCmd.SetOut(&buf)
	modelsCmd.SetErr(&buf)
	refreshModelsFlag = false

	err := modelsCmd.RunE(modelsCmd, []string{})
	if err != nil {
		t.Fatalf("modelsCmd failed: %v", err)
	}

	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("gemini-3.8-flash")) {
		t.Errorf("expected output to contain 'gemini-3.8-flash', got: %s", out)
	}
}
