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
		"conversation_title": "Investigate memory leak",
		"cost": 0.0042,
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

	state, okState := GetSessionContextState(pDir)
	if !okState || state == nil {
		t.Fatalf("expected GetSessionContextState to succeed")
	}
	if state.ConversationTitle != "Investigate memory leak" {
		t.Errorf("expected ConversationTitle 'Investigate memory leak', got %q", state.ConversationTitle)
	}
	if state.Cost != 0.0042 {
		t.Errorf("expected Cost 0.0042, got %f", state.Cost)
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

func TestFormatStatusLineText(t *testing.T) {
	// 1. Full data without colors, with effort and cost
	quota := &ModelQuotaDetails{
		Fraction5H:         0.95,
		CompactReset5H:     "1h26m",
		FractionWeekly:     0.79,
		CompactResetWeekly: "6h35m",
	}
	s := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", "high", 0.0042, 5, true, quota, false)
	expected := "[davidnguyen] · 5% ctx · gemini-3.7-flash (high) · $0.0042 · 95% (1h26m) · 79% (6h35m)"
	if s != expected {
		t.Errorf("FormatStatusLineText() = %q, expected %q", s, expected)
	}

	// 2. Full data with colors
	sColored := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", "high", 0.0042, 5, true, quota, true)
	if !strings.Contains(sColored, "[davidnguyen]") || !strings.Contains(sColored, "\033[") {
		t.Errorf("expected colored string to contain ANSI escapes, got: %q", sColored)
	}

	// 3. No quota data (offline or error), zero cost
	sNoQuota := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", "", 0.0, 10, true, nil, false)
	expectedNoQuota := "[davidnguyen] · 10% ctx · gemini-3.7-flash · $0.00"
	if sNoQuota != expectedNoQuota {
		t.Errorf("FormatStatusLineText() = %q, expected %q", sNoQuota, expectedNoQuota)
	}

	// 4. Critical quota (< 5%) with colors
	quotaCrit := &ModelQuotaDetails{
		Fraction5H:     0.03,
		CompactReset5H: "45m",
	}
	sCrit := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", "high", 0.05, 85, true, quotaCrit, true)
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

func TestStatusLineBackwardsCompatibilityAndTitlePersistence(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pDir, err := Create("test-compat-profile")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}
	_ = SetCurrent("test-compat-profile")
	t.Setenv("HOME", pDir)
	t.Setenv("AGYS_PROFILE", "test-compat-profile")

	// Step 1: Initial payload with title and cost
	firstPayload := `{
		"conversation_title": "Original Title",
		"cost": 0.005,
		"model": {"id": "gemini-3.8-flash", "display_name": "Gemini 3.8 Flash"},
		"context_window": {"used_percentage": 15.0}
	}`
	var out1 bytes.Buffer
	if err := HandleStatusLine(context.Background(), strings.NewReader(firstPayload), &out1, nil); err != nil {
		t.Fatalf("First HandleStatusLine failed: %v", err)
	}

	state, ok := GetSessionContextState(pDir)
	if !ok || state == nil {
		t.Fatalf("expected state to exist")
	}
	if state.ConversationTitle != "Original Title" {
		t.Errorf("expected 'Original Title', got %q", state.ConversationTitle)
	}
	if state.Cost != 0.005 {
		t.Errorf("expected cost 0.005, got %f", state.Cost)
	}

	// Step 2: Subsequent legacy turn without title/cost in payload (e.g. streaming update without title)
	secondPayload := `{
		"model": {"id": "gemini-3.8-flash", "display_name": "Gemini 3.8 Flash"},
		"context_window": {"used_percentage": 25.0}
	}`
	var out2 bytes.Buffer
	if err := HandleStatusLine(context.Background(), strings.NewReader(secondPayload), &out2, nil); err != nil {
		t.Fatalf("Second HandleStatusLine failed: %v", err)
	}

	state2, ok2 := GetSessionContextState(pDir)
	if !ok2 || state2 == nil {
		t.Fatalf("expected state2 to exist")
	}
	// Title and Cost must be preserved across turns!
	if state2.ConversationTitle != "Original Title" {
		t.Errorf("expected title to persist as 'Original Title', got %q", state2.ConversationTitle)
	}
	if state2.Cost != 0.005 {
		t.Errorf("expected cost to persist as 0.005, got %f", state2.Cost)
	}
	if state2.UsedPercentage != 25.0 {
		t.Errorf("expected updated percentage 25.0, got %f", state2.UsedPercentage)
	}
	pct, okPct := GetSessionContext(pDir)
	if !okPct || pct != 25 {
		t.Errorf("expected GetSessionContext pct=25, got %d (ok=%v)", pct, okPct)
	}
}

func TestParsePayloadQuotaDisambiguation(t *testing.T) {
	payload := &StatusLinePayload{
		Quota: map[string]struct {
			RemainingFraction    float64 `json:"remaining_fraction"`
			RemainingFractionAlt float64 `json:"remainingFraction"`
			ResetTime            string  `json:"reset_time"`
			ResetTimeAlt         string  `json:"resetTime"`
			ResetInSeconds       uint64  `json:"reset_in_seconds"`
			ResetInSecondsAlt    uint64  `json:"resetInSeconds"`
		}{
			"gemini-5h": {
				RemainingFraction: 0.85,
				ResetTime:         "2026-09-06T15:00:00Z",
			},
			"gemini-weekly": {
				RemainingFraction: 0.65,
				ResetInSeconds:    7200,
			},
		},
	}
	details := parsePayloadQuota(payload.Quota)
	if details == nil {
		t.Fatalf("expected details to be parsed")
	}
	if details.Fraction5H != 0.85 {
		t.Errorf("expected Fraction5H=0.85, got %f", details.Fraction5H)
	}
	if details.FractionWeekly != 0.65 {
		t.Errorf("expected FractionWeekly=0.65, got %f", details.FractionWeekly)
	}
	if details.CompactResetWeekly == "" || details.CompactResetWeekly == "-" {
		t.Errorf("expected CompactResetWeekly to be populated via ResetInSeconds, got %q", details.CompactResetWeekly)
	}
}

func TestFormatCostEdgeCases(t *testing.T) {
	testCases := []struct {
		cost     float64
		expected string
	}{
		{0.0, "$0.00"},
		{-1.0, "$0.00"},
		{0.00001, "$0.00"},
		{0.000049, "$0.00"},
		{0.0001, "$0.0001"},
		{0.0042, "$0.0042"},
		{0.05, "$0.05"},
		{0.5, "$0.50"},
		{1.0, "$1.00"},
		{1.25, "$1.25"},
		{1.2543, "$1.2543"},
		{10.5, "$10.50"},
	}
	for _, tc := range testCases {
		res := FormatCost(tc.cost)
		if res != tc.expected {
			t.Errorf("FormatCost(%v) = %q, expected %q", tc.cost, res, tc.expected)
		}
	}
}

