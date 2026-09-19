package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionContextSaveAndGet(t *testing.T) {
	t.Setenv("HERDR_PANE_ID", "")
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

	// 3. No quota data (offline or error), zero cost (omitted)
	sNoQuota := FormatStatusLineText("davidnguyen", "gemini-3.7-flash", "", 0.0, 10, true, nil, false)
	expectedNoQuota := "[davidnguyen] · 10% ctx · gemini-3.7-flash"
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

	// Step 1: Initial payload with conversation_id, title and cost
	firstPayload := `{
		"conversation_id": "conv-persistent-123",
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
	if state.ConversationID != "conv-persistent-123" {
		t.Errorf("expected 'conv-persistent-123', got %q", state.ConversationID)
	}
	if state.ConversationTitle != "Original Title" {
		t.Errorf("expected 'Original Title', got %q", state.ConversationTitle)
	}
	if state.Cost != 0.005 {
		t.Errorf("expected cost 0.005, got %f", state.Cost)
	}

	// Step 2: Subsequent turn without conversation_id or title/cost in payload (e.g. streaming update)
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
	// Title, ID and Cost must be preserved across turns when conversation_id is omitted!
	if state2.ConversationID != "conv-persistent-123" {
		t.Errorf("expected conversation_id to persist as 'conv-persistent-123', got %q", state2.ConversationID)
	}
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

	// Step 3: Switched conversation ID should cleanly reset old title and cost
	thirdPayload := `{
		"conversation_id": "conv-switched-456",
		"model": {"id": "gemini-3.8-flash", "display_name": "Gemini 3.8 Flash"},
		"context_window": {"used_percentage": 10.0}
	}`
	var out3 bytes.Buffer
	if err := HandleStatusLine(context.Background(), strings.NewReader(thirdPayload), &out3, nil); err != nil {
		t.Fatalf("Third HandleStatusLine failed: %v", err)
	}

	state3, ok3 := GetSessionContextState(pDir)
	if !ok3 || state3 == nil {
		t.Fatalf("expected state3 to exist")
	}
	if state3.ConversationID != "conv-switched-456" {
		t.Errorf("expected 'conv-switched-456', got %q", state3.ConversationID)
	}
	if state3.ConversationTitle != "" {
		t.Errorf("expected title to reset on switched conversation, got %q", state3.ConversationTitle)
	}
	if state3.Cost != 0.0 {
		t.Errorf("expected cost to reset on switched conversation, got %f", state3.Cost)
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

func TestResolveConversationTitle(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pDir, err := Create("title-test-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	convID := "test-conv-abc"
	logsDir := filepath.Join(pDir, ".gemini", "antigravity-cli", "brain", convID, ".system_generated", "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}
	transcriptPath := filepath.Join(logsDir, "transcript.jsonl")
	transcriptContent := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-09-07T09:00:00Z","content":"<USER_REQUEST>\nRefactor Herdr sidebar row to show conversation title\n</USER_REQUEST>"}`
	if err := os.WriteFile(transcriptPath, []byte(transcriptContent), 0644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	// 1. Resolve directly from transcript
	title := ResolveConversationTitle(pDir, convID)
	if title != "Refactor Herdr sidebar row to show conversation title" {
		t.Errorf("expected resolved title from transcript, got: %q", title)
	}

	// 2. Resolve from history.jsonl when transcript not found
	historyPath := filepath.Join(pDir, ".gemini", "antigravity-cli", "history.jsonl")
	historyContent := `{"display":"Fix auth login bug","timestamp":1788747000000,"workspace":"/app","conversationId":"other-conv-xyz"}` + "\n"
	if err := os.WriteFile(historyPath, []byte(historyContent), 0644); err != nil {
		t.Fatalf("failed to write history: %v", err)
	}

	title2 := ResolveConversationTitle(pDir, "other-conv-xyz")
	if title2 != "Fix auth login bug" {
		t.Errorf("expected resolved title from history, got: %q", title2)
	}

	// 3. Unknown convID returns empty string without bleeding other sessions
	title3 := ResolveConversationTitle(pDir, "non-existent-conv")
	if title3 != "" {
		t.Errorf("expected empty string for non-existent convID, got: %q", title3)
	}

	// 4. Empty convID returns empty string without bleeding other sessions
	title4 := ResolveConversationTitle(pDir, "")
	if title4 != "" {
		t.Errorf("expected empty string for empty convID, got: %q", title4)
	}
}

func TestSessionContext_PaneIsolation(t *testing.T) {
	tempDir := t.TempDir()

	// Seed Pane 1
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	state1 := &SessionContextState{
		UsedPercentage:    50.0,
		ConversationTitle: "Task in Pane 1",
		ConversationID:    "conv-p1",
	}
	if err := SaveSessionContext(tempDir, state1); err != nil {
		t.Fatalf("SaveSessionContext for p1 failed: %v", err)
	}

	// Verify Pane 1 reads its own context
	loaded1, ok1 := GetSessionContextState(tempDir)
	if !ok1 || loaded1.ConversationTitle != "Task in Pane 1" {
		t.Fatalf("Pane 1 failed to load its own context: ok=%v, title=%v", ok1, loaded1)
	}

	// Switch to Pane 2 (brand new pane)
	t.Setenv("HERDR_PANE_ID", "w1:p2")
	loaded2, ok2 := GetSessionContextState(tempDir)
	if ok2 || loaded2 != nil {
		t.Fatalf("Pane 2 must NOT inherit Pane 1's context or fall back to any other file, got: %+v", loaded2)
	}

	// Seed Pane 2 with its own context
	state2 := &SessionContextState{
		UsedPercentage:    10.0,
		ConversationTitle: "Task in Pane 2",
		ConversationID:    "conv-p2",
	}
	if err := SaveSessionContext(tempDir, state2); err != nil {
		t.Fatalf("SaveSessionContext for p2 failed: %v", err)
	}

	// Verify Pane 2 loads its own context
	loaded2, ok2 = GetSessionContextState(tempDir)
	if !ok2 || loaded2.ConversationTitle != "Task in Pane 2" {
		t.Fatalf("Pane 2 failed to load its own context: ok=%v, title=%v", ok2, loaded2)
	}

	// Reset Pane 2 and verify Pane 1 is unaffected
	if err := ResetSessionContext(tempDir); err != nil {
		t.Fatalf("ResetSessionContext failed: %v", err)
	}
	if _, ok2AfterReset := GetSessionContextState(tempDir); ok2AfterReset {
		t.Fatalf("Pane 2 should be empty after reset")
	}

	// Switch back to Pane 1 and ensure it is still intact
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	loaded1After, ok1After := GetSessionContextState(tempDir)
	if !ok1After || loaded1After.ConversationTitle != "Task in Pane 1" {
		t.Fatalf("Pane 1 context was corrupted by Pane 2 reset: ok=%v, state=%+v", ok1After, loaded1After)
	}
}

func TestRemoveStatusLineSettings(t *testing.T) {
	tempHome := t.TempDir()
	pDir := filepath.Join(tempHome, "test-prof")
	cliSettings := filepath.Join(pDir, ".gemini", "antigravity-cli", "settings.json")
	_ = os.MkdirAll(filepath.Dir(cliSettings), 0700)
	_ = os.WriteFile(cliSettings, []byte(`{"model":"gemini-flash","statusLine":{"command":"agys statusline-hook"}}`), 0600)

	if err := RemoveStatusLineSettings(pDir); err != nil {
		t.Fatalf("RemoveStatusLineSettings failed: %v", err)
	}

	data, err := os.ReadFile(cliSettings)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if strings.Contains(string(data), "statusLine") {
		t.Errorf("Expected statusLine to be removed, got: %s", string(data))
	}
	if !strings.Contains(string(data), "gemini-flash") {
		t.Errorf("Expected model to be preserved, got: %s", string(data))
	}
}

func TestIsStatusLineDisabled(t *testing.T) {
	t.Setenv("AGYS_NO_STATUSLINE", "")
	if IsStatusLineDisabled() {
		t.Errorf("Expected false when empty")
	}

	t.Setenv("AGYS_NO_STATUSLINE", "1")
	if !IsStatusLineDisabled() {
		t.Errorf("Expected true when 1")
	}

	t.Setenv("AGYS_NO_STATUSLINE", "true")
	if !IsStatusLineDisabled() {
		t.Errorf("Expected true when true")
	}
}

func TestStatusLine_FreshSessionDoesNotAdoptOldConversationFromDisk(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pDir, err := Create("test-no-adopt-profile")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}
	_ = SetCurrent("test-no-adopt-profile")
	t.Setenv("AGYS_PROFILE", "test-no-adopt-profile")

	// Create an old historical conversation on disk
	oldConvDir := filepath.Join(pDir, ".gemini", "antigravity-cli", "brain", "old-conv-999", ".system_generated", "logs")
	_ = os.MkdirAll(oldConvDir, 0700)
	transcriptPath := filepath.Join(oldConvDir, "transcript.jsonl")
	_ = os.WriteFile(transcriptPath, []byte(`{"type":"USER_INPUT","content":"Ancient conversation prompt"}`+"\n"), 0600)

	// Live statusline payload from a newly opened session without conversation_id yet
	freshPayload := `{
		"model": {"id": "gemini-3.8-flash", "display_name": "Gemini 3.8 Flash"},
		"context_window": {"used_percentage": 5.0}
	}`

	var stdout, stderr bytes.Buffer
	if err := HandleStatusLine(context.Background(), strings.NewReader(freshPayload), &stdout, &stderr); err != nil {
		t.Fatalf("HandleStatusLine error: %v", err)
	}

	state, ok := GetSessionContextState(pDir)
	if !ok || state == nil {
		t.Fatalf("expected session context state to exist")
	}

	if state.ConversationID == "old-conv-999" {
		t.Errorf("Fresh session improperly adopted old conversation ID 'old-conv-999'")
	}
	if state.ConversationTitle == "Ancient conversation prompt" {
		t.Errorf("Fresh session improperly adopted old conversation title 'Ancient conversation prompt'")
	}
}

func TestSessionContextEquality_IgnoresTokenCounters(t *testing.T) {
	state1 := &SessionContextState{
		UsedPercentage:      35.0,
		InputTokens:         1000,
		CacheReadTokens:     2000,
		CacheCreationTokens: 500,
		ModelID:             "gemini-3.8-flash",
		ModelDisplayName:    "Gemini 3.8 Flash",
		ConversationTitle:   "Fixing bugs",
		ConversationID:      "conv-123",
		Cost:                0.05,
		Effort:              "high",
	}

	// State 2 has identical visible metadata but different token counters (e.g. after prompt interaction)
	state2 := &SessionContextState{
		UsedPercentage:      35.0,
		InputTokens:         4500,
		CacheReadTokens:     8000,
		CacheCreationTokens: 1200,
		ModelID:             "gemini-3.8-flash",
		ModelDisplayName:    "Gemini 3.8 Flash",
		ConversationTitle:   "Fixing bugs",
		ConversationID:      "conv-123",
		Cost:                0.05,
		Effort:              "high",
	}

	if !isSessionContextStateEqual(state1, state2) {
		t.Errorf("expected isSessionContextStateEqual to return true when only token counts differ")
	}

	// Changing UsedPercentage or Model should mark it dirty
	state2.UsedPercentage = 36.0
	if isSessionContextStateEqual(state1, state2) {
		t.Errorf("expected isSessionContextStateEqual to return false when UsedPercentage changes")
	}
}

func TestParsePayloadQuota_ModelDisambiguation(t *testing.T) {
	payload := &StatusLinePayload{
		Quota: map[string]struct {
			RemainingFraction    float64 `json:"remaining_fraction"`
			RemainingFractionAlt float64 `json:"remainingFraction"`
			ResetTime            string  `json:"reset_time"`
			ResetTimeAlt         string  `json:"resetTime"`
			ResetInSeconds       uint64  `json:"reset_in_seconds"`
			ResetInSecondsAlt    uint64  `json:"resetInSeconds"`
		}{
			"3p-5h": {
				RemainingFraction: 1.0,
				ResetTime:         "2026-09-06T18:00:00Z",
			},
			"3p-weekly": {
				RemainingFraction: 1.0,
				ResetTime:         "2026-09-13T18:00:00Z",
			},
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

	// 1. When Gemini model is active, must strictly pick gemini-5h (0.85) and gemini-weekly (0.65)
	detailsGemini := parsePayloadQuota(payload.Quota, "gemini-3.8-flash")
	if detailsGemini == nil {
		t.Fatalf("expected detailsGemini to be parsed")
	}
	if detailsGemini.Fraction5H != 0.85 {
		t.Errorf("expected Fraction5H=0.85 for Gemini, got %f", detailsGemini.Fraction5H)
	}
	if detailsGemini.FractionWeekly != 0.65 {
		t.Errorf("expected FractionWeekly=0.65 for Gemini, got %f", detailsGemini.FractionWeekly)
	}
	if detailsGemini.GroupName != "Gemini Models" {
		t.Errorf("expected GroupName 'Gemini Models', got: %s", detailsGemini.GroupName)
	}

	// 2. When Claude / 3P model is active, must strictly pick 3p-5h (1.0) and 3p-weekly (1.0)
	detailsClaude := parsePayloadQuota(payload.Quota, "claude-3-7-sonnet")
	if detailsClaude == nil {
		t.Fatalf("expected detailsClaude to be parsed")
	}
	if detailsClaude.Fraction5H != 1.0 {
		t.Errorf("expected Fraction5H=1.0 for Claude, got %f", detailsClaude.Fraction5H)
	}
	if detailsClaude.FractionWeekly != 1.0 {
		t.Errorf("expected FractionWeekly=1.0 for Claude, got %f", detailsClaude.FractionWeekly)
	}
	if detailsClaude.GroupName != "Claude and GPT models" {
		t.Errorf("expected GroupName 'Claude and GPT models', got: %s", detailsClaude.GroupName)
	}
}

func TestUpdateCachedQuotaFractions(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pName := "test-cache-sync"
	_, err := Create(pName)
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	initialSummary := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []QuotaBucket{
					{
						BucketID:          "gemini-5h",
						Window:            "5h",
						RemainingFraction: 0.99,
					},
					{
						BucketID:          "gemini-weekly",
						Window:            "weekly",
						RemainingFraction: 0.80,
					},
				},
			},
			{
				DisplayName: "Claude and GPT models",
				Buckets: []QuotaBucket{
					{
						BucketID:          "3p-5h",
						Window:            "5h",
						RemainingFraction: 1.0,
					},
					{
						BucketID:          "3p-weekly",
						Window:            "weekly",
						RemainingFraction: 1.0,
					},
				},
			},
		},
	}
	if err := SaveCachedQuota(pName, initialSummary); err != nil {
		t.Fatalf("SaveCachedQuota error: %v", err)
	}

	// Update Gemini fractions
	newGeminiDetails := &ModelQuotaDetails{
		Fraction5H:     0.94,
		FractionWeekly: 0.75,
	}
	if err := UpdateCachedQuotaFractions(pName, "gemini-3.8-flash", newGeminiDetails); err != nil {
		t.Fatalf("UpdateCachedQuotaFractions error: %v", err)
	}

	updated, ok := GetCachedQuota(pName, 0)
	if !ok || updated == nil {
		t.Fatalf("failed to read updated cache")
	}

	var foundGemini5H, foundGeminiWk float64
	for _, g := range updated.Groups {
		if g.DisplayName == "Gemini Models" {
			for _, b := range g.Buckets {
				if b.BucketID == "gemini-5h" {
					foundGemini5H = b.RemainingFraction
				}
				if b.BucketID == "gemini-weekly" {
					foundGeminiWk = b.RemainingFraction
				}
			}
		}
	}

	if foundGemini5H != 0.94 {
		t.Errorf("expected updated gemini-5h fraction 0.94, got: %f", foundGemini5H)
	}
	if foundGeminiWk != 0.75 {
		t.Errorf("expected updated gemini-weekly fraction 0.75, got: %f", foundGeminiWk)
	}

	// Test updating brand new profile without existing cache
	pBrandNew := "test-cache-brand-new"
	_, _ = Create(pBrandNew)
	new3PDetails := &ModelQuotaDetails{
		Fraction5H:     0.88,
		FractionWeekly: 0.92,
	}
	if err := UpdateCachedQuotaFractions(pBrandNew, "claude-3-7-sonnet", new3PDetails); err != nil {
		t.Fatalf("UpdateCachedQuotaFractions on uninitialized cache failed: %v", err)
	}
	newCached, ok := GetCachedQuota(pBrandNew, 0)
	if !ok || newCached == nil {
		t.Fatalf("expected cache to be auto-created for brand new profile")
	}
	var found3P5H float64
	for _, g := range newCached.Groups {
		if g.DisplayName == "Claude and GPT models" {
			for _, b := range g.Buckets {
				if b.BucketID == "3p-5h" {
					found3P5H = b.RemainingFraction
				}
			}
		}
	}
	if found3P5H != 0.88 {
		t.Errorf("expected auto-initialized 3p-5h to be 0.88, got: %f", found3P5H)
	}
}

func TestHandleStatusLine_HerdrSync(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_sl_sync_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	receivedMetadata := make(chan string, 10)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 4096)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				resp := `{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"agy","title":"agys: test","tokens":{"profile":"sync-prof","model":"gemini-3.8-flash"}}]}}`
				_, _ = conn.Write([]byte(resp + "\n"))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					receivedMetadata <- reqStr
				}
			}
			_ = conn.Close()
		}
	}()

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	t.Setenv("HERDR_SOCKET_PATH", sockPath)

	pName := "sync-prof"
	pDir, err := Create(pName)
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}
	_ = SetCurrent(pName)
	t.Setenv("AGYS_PROFILE", pName)

	_ = SaveCachedQuota(pName, &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []QuotaBucket{
					{
						BucketID:          "gemini-5h",
						Window:            "5h",
						RemainingFraction: 0.92,
					},
					{
						BucketID:          "gemini-weekly",
						Window:            "weekly",
						RemainingFraction: 0.78,
					},
				},
			},
		},
	})

	_ = SaveSessionContext(pDir, &SessionContextState{
		ConversationTitle: "Live Task",
		ModelID:           "gemini-3.8-flash",
	})

	payload := `{
		"model": {"id": "gemini-3.8-flash", "display_name": "Gemini 3.8 Flash"},
		"context_window": {"used_percentage": 12.0},
		"quota": {
			"gemini-5h": {"remaining_fraction": 0.91, "reset_time": "2026-09-19T10:00:00Z"},
			"gemini-weekly": {"remaining_fraction": 0.77, "reset_time": "2026-09-26T10:00:00Z"}
		}
	}`

	var stdout, stderr bytes.Buffer
	if err := HandleStatusLine(context.Background(), strings.NewReader(payload), &stdout, &stderr); err != nil {
		t.Fatalf("HandleStatusLine error: %v", err)
	}

	footerOutput := stdout.String()
	if !strings.Contains(footerOutput, "91%") {
		t.Errorf("Expected footer to contain live 91%% quota from payload, got: %s", footerOutput)
	}

	// Verify Herdr sidebar receives the synchronized metadata
	select {
	case metaPayload := <-receivedMetadata:
		if !strings.Contains(metaPayload, `"quota_5h_normal":"91%`) {
			t.Errorf("Expected Herdr metadata payload to contain '91%%', got: %s", metaPayload)
		}
		if !strings.Contains(metaPayload, `"quota_week_normal":"77%`) {
			t.Errorf("Expected Herdr metadata payload to contain '77%%', got: %s", metaPayload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timeout waiting for Herdr metadata report from HandleStatusLine")
	}
}



