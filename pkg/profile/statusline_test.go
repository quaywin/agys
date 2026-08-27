package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionContextSaveAndGet(t *testing.T) {
	tempDir := t.TempDir()

	// Initial check on empty
	if _, ok := GetSessionContext(tempDir); ok {
		t.Errorf("expected GetSessionContext to return false on empty dir")
	}

	// Save context
	state := &SessionContextState{
		UsedPercentage:      42.4,
		InputTokens:         1500,
		CacheReadTokens:     3500,
		CacheCreationTokens: 500,
		ModelID:             "claude-3-7-sonnet",
		ModelDisplayName:    "Claude 3.7 Sonnet",
	}
	if err := SaveSessionContext(tempDir, state); err != nil {
		t.Fatalf("SaveSessionContext failed: %v", err)
	}

	pct, ok := GetSessionContext(tempDir)
	if !ok {
		t.Fatalf("expected GetSessionContext to return true")
	}
	if pct != 42 {
		t.Errorf("expected pct=42, got %d", pct)
	}

	// Test expired context (> 2 hours)
	state.UpdatedAt = time.Now().Add(-3 * time.Hour)
	data, _ := json.Marshal(state)
	_ = os.WriteFile(filepath.Join(tempDir, sessionContextFilename), data, 0600)

	if _, ok := GetSessionContext(tempDir); ok {
		t.Errorf("expected expired context to return false")
	}
}

func TestHandleStatusLine(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pDir, err := Create("test-statusline-profile")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}
	_ = SetCurrent("test-statusline-profile")
	t.Setenv("HOME", pDir)
	t.Setenv("AGYS_PROFILE", "test-statusline-profile")

	payloadJSON := `{
		"model": {
			"id": "claude-3-7-sonnet",
			"display_name": "Claude 3.7 Sonnet"
		},
		"context_window": {
			"used_percentage": 33.2,
			"current_usage": {
				"input_tokens": 120,
				"cache_read_input_tokens": 800,
				"cache_creation_input_tokens": 0
			}
		},
		"quota": {
			"gemini-5h": {
				"remaining_fraction": 0.85,
				"reset_time": "2026-08-27T12:00:00Z"
			}
		}
	}`

	var stdout, stderr bytes.Buffer
	err = HandleStatusLine(context.Background(), strings.NewReader(payloadJSON), &stdout, &stderr)
	if err != nil {
		t.Fatalf("HandleStatusLine returned error: %v", err)
	}

	pct, ok := GetSessionContext(pDir)
	if !ok {
		t.Fatalf("expected context to be saved for profile")
	}
	if pct != 33 {
		t.Errorf("expected pct=33, got %d", pct)
	}

	// Verify .active_model was updated
	activeData, err := os.ReadFile(filepath.Join(pDir, ".active_model"))
	if err != nil || strings.TrimSpace(string(activeData)) != "claude-3-7-sonnet" {
		t.Errorf("expected .active_model to be claude-3-7-sonnet, got: %s", string(activeData))
	}
	// Verify stdout contains formatted statusline text
	outStr := stdout.String()
	if !strings.Contains(outStr, "test-statusline-profile") {
		t.Errorf("expected stdout to contain profile name, got: %q", outStr)
	}
	if !strings.Contains(outStr, "33% ctx") {
		t.Errorf("expected stdout to contain '33%%%% ctx', got: %q", outStr)
	}
	if !strings.Contains(outStr, "claude-3-7-sonnet") {
		t.Errorf("expected stdout to contain 'claude-3-7-sonnet', got: %q", outStr)
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		tokens   int64
		expected string
	}{
		{0, ""},
		{500, "500"},
		{1000, "1.0k"},
		{14500, "14.5k"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
	}

	for _, tt := range tests {
		got := FormatTokens(tt.tokens)
		if got != tt.expected {
			t.Errorf("FormatTokens(%d) = %q, expected %q", tt.tokens, got, tt.expected)
		}
	}
}

func TestFormatStatusLineText(t *testing.T) {
	// 1. Full data without colors
	quota := &ModelQuotaDetails{
		Fraction5H:         0.95,
		CompactReset5H:     "1h26m",
		FractionWeekly:     0.79,
		CompactResetWeekly: "6h35m",
	}
	s := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", 5, true, 14500, quota, false)
	expected := "[davidnguyen] · 5% ctx (14.5k) · gemini-3.7-flash · 5H: 95% (1h26m) · Wk: 79% (6h35m)"
	if s != expected {
		t.Errorf("FormatStatusLineText() = %q, expected %q", s, expected)
	}

	// 2. Full data with colors
	sColored := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", 5, true, 14500, quota, true)
	if !strings.Contains(sColored, "[davidnguyen]") || !strings.Contains(sColored, "\033[") {
		t.Errorf("expected colored string to contain ANSI escapes, got: %q", sColored)
	}

	// 3. No quota data (offline or error)
	sNoQuota := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", 10, true, 0, nil, false)
	expectedNoQuota := "[davidnguyen] · 10% ctx · gemini-3.7-flash"
	if sNoQuota != expectedNoQuota {
		t.Errorf("FormatStatusLineText() = %q, expected %q", sNoQuota, expectedNoQuota)
	}

	// 4. Critical quota (< 5%) with colors
	quotaCrit := &ModelQuotaDetails{
		Fraction5H:     0.03,
		CompactReset5H: "45m",
	}
	sCrit := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", 85, true, 50000, quotaCrit, true)
	// Should contain red ANSI for 85% ctx and red ANSI for 3% quota
	if !strings.Contains(sCrit, "\033[1;31m") {
		t.Errorf("expected critical alert color ANSI code in output, got: %q", sCrit)
	}
}

func TestSyncStatusLineSettings(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pDir, err := Create("test-sync-sl-profile")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Seed existing custom statusLine
	cliSettings := filepath.Join(pDir, ".gemini", "antigravity-cli", "settings.json")
	_ = os.MkdirAll(filepath.Dir(cliSettings), 0700)
	_ = os.WriteFile(cliSettings, []byte(`{"statusLine":{"type":"command","command":"echo custom"}}`), 0600)

	if err := SyncStatusLineSettings(pDir); err != nil {
		t.Fatalf("SyncStatusLineSettings failed: %v", err)
	}

	// Check updated settings
	data, _ := os.ReadFile(cliSettings)
	if !strings.Contains(string(data), "agys statusline-hook") {
		t.Errorf("expected settings.json to contain 'agys statusline-hook', got: %s", string(data))
	}

	// Check backup was created
	backupPath := filepath.Join(pDir, ".gemini", "config", statuslineBackupFile)
	bData, err := os.ReadFile(backupPath)
	if err != nil || !strings.Contains(string(bData), "echo custom") {
		t.Errorf("expected backup file to contain 'echo custom', got: %s", string(bData))
	}
}
