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
		SchemaVersion:  profile.ModelCacheSchemaVersion,
		LatestFlash:    "gemini-3.8-flash",
		LatestHydrogen: "gemini-4-hydrogen",
		LatestPro:      "gemini-3.1-pro",
		AllModels:      []string{"gemini-4-hydrogen", "gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.1-pro"},
		ModelEfforts: map[string][]string{
			"gemini-4-hydrogen": {"low", "medium", "high", "xhigh"},
			"gemini-3.8-flash":  {"low", "medium", "high"},
		},
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
	if !bytes.Contains(buf.Bytes(), []byte("gemini-4-hydrogen")) {
		t.Errorf("expected output to contain 'gemini-4-hydrogen', got: %s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("xhigh")) {
		t.Errorf("expected output to contain 'xhigh' effort, got: %s", out)
	}
}
