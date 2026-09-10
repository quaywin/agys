package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSyncHerdrIntegration(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))
	t.Setenv("HERDR_ENV", "1")

	profileName := "test-herdr-profile"
	pDir, err := Create(profileName)
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	if err := SyncHerdrIntegration(pDir); err != nil {
		t.Fatalf("SyncHerdrIntegration error: %v", err)
	}

	hookFile := filepath.Join(pDir, ".gemini", "config", "hooks", "herdr-agent-state.sh")
	info, err := os.Stat(hookFile)
	if err != nil {
		t.Fatalf("Expected hookFile to exist: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("Expected hookFile to be executable, got %v", info.Mode())
	}

	hooksJSON := filepath.Join(pDir, ".gemini", "config", "hooks.json")
	data, err := os.ReadFile(hooksJSON)
	if err != nil {
		t.Fatalf("Expected hooks.json to exist: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse hooks.json: %v", err)
	}

	herdrMap, ok := parsed["herdr"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected 'herdr' key in hooks.json")
	}

	preInv, ok := herdrMap["PreInvocation"].([]interface{})
	if !ok || len(preInv) == 0 {
		t.Fatalf("Expected 'PreInvocation' array in herdr config")
	}

	hookEntry, ok := preInv[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map in PreInvocation[0]")
	}

	cmdStr, ok := hookEntry["command"].(string)
	if !ok || !strings.Contains(cmdStr, "herdr-agent-state.sh") {
		t.Errorf("Expected command to reference herdr-agent-state.sh, got %q", cmdStr)
	}

	postInv, ok := herdrMap["PostInvocation"].([]interface{})
	if !ok || len(postInv) == 0 {
		t.Fatalf("Expected 'PostInvocation' array in herdr config")
	}

	stopInv, ok := herdrMap["Stop"].([]interface{})
	if !ok || len(stopInv) == 0 {
		t.Fatalf("Expected 'Stop' array in herdr config")
	}
}

func TestReportHerdrMetadata_MockSocket(t *testing.T) {
	// macOS limits UNIX domain socket paths to 104 chars, use short path
	sockPath := fmt.Sprintf("/tmp/herdr_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"Antigravity","tokens":{"profile":"my-test-profile"}}]}}` + "\n"))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") && strings.Contains(reqStr, "my-test-profile") {
					select {
					case received <- reqStr:
					default:
					}
				}
			}
			_ = conn.Close()
		}
	}()

	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	t.Setenv("HERDR_SOCKET_PATH", sockPath)

	err = ReportHerdrMetadata(context.Background(), "my-test-profile")
	if err != nil {
		t.Fatalf("ReportHerdrMetadata error: %v", err)
	}

	select {
	case payload := <-received:
		if !strings.Contains(payload, "my-test-profile") {
			t.Errorf("Expected payload to contain 'my-test-profile', got: %s", payload)
		}
		if !strings.Contains(payload, "pane.report_metadata") {
			t.Errorf("Expected payload method to be 'pane.report_metadata', got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
	}
}

func TestSetTerminalTitle(t *testing.T) {
	// Should not panic on empty or valid profile name
	SetTerminalTitle("")
	SetTerminalTitle("prod-profile")
}

func TestSanitizeTerminalTitle(t *testing.T) {
	// 1. Control character stripping (OSC injection prevention)
	malicious := "agys: \033]0;evil\007malicious\r\ncommand"
	sanitized := sanitizeTerminalTitle(malicious)
	if strings.ContainsAny(sanitized, "\033\007\r\n") {
		t.Errorf("expected control characters stripped, got: %q", sanitized)
	}
	if !strings.Contains(sanitized, "evil") || !strings.Contains(sanitized, "command") {
		t.Errorf("expected content preserved without control chars, got: %q", sanitized)
	}

	// 2. Whitespace collapsing
	spaced := "agys:    hello   world   "
	if got := sanitizeTerminalTitle(spaced); got != "agys: hello world" {
		t.Errorf("expected whitespace collapsed, got: %q", got)
	}

	// 3. Length capping
	veryLong := "agys: " + strings.Repeat("a", 150)
	gotLong := sanitizeTerminalTitle(veryLong)
	if len(gotLong) > 100 {
		t.Errorf("expected length capped at <= 100, got len=%d: %q", len(gotLong), gotLong)
	}
	if !strings.HasSuffix(gotLong, "...") {
		t.Errorf("expected ellipsis suffix, got: %q", gotLong)
	}
}

func TestStartHerdrQuotaWatcher(t *testing.T) {
	// Test when HERDR_ENV is not set
	t.Setenv("HERDR_ENV", "")
	cleanup := StartHerdrQuotaWatcher(context.Background(), "my-test-profile")
	cleanup()

	// Test when HERDR_ENV is set but cancelled immediately
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/nonexistent.sock")
	ctx, cancel := context.WithCancel(context.Background())
	cleanup2 := StartHerdrQuotaWatcher(ctx, "my-test-profile")
	// Second watcher on same profile should not collide or panic (leader election)
	cleanup3 := StartHerdrQuotaWatcher(ctx, "my-test-profile")
	cleanup3()
	cancel()
	cleanup2()
}

func TestHandleHerdrHook(t *testing.T) {
	// 1. HERDR_ENV not set
	t.Setenv("HERDR_ENV", "")
	if err := HandleHerdrHook(context.Background(), "session", nil); err != nil {
		t.Errorf("HandleHerdrHook session error: %v", err)
	}
	if err := HandleHerdrHook(context.Background(), "quota", nil); err != nil {
		t.Errorf("HandleHerdrHook quota error: %v", err)
	}

	// 2. HERDR_ENV set with mock socket
	sockPath := fmt.Sprintf("/tmp/herdr_hook_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte(`{"id":"test","result":"ok"}` + "\n"))
			_ = conn.Close()
		}
	}()

	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	t.Setenv("HERDR_SOCKET_PATH", sockPath)

	stdin := strings.NewReader(`{"conversationId":"test-conv-123","transcriptPath":"/tmp/test.jsonl"}`)
	if err := HandleHerdrHook(context.Background(), "session", stdin); err != nil {
		t.Errorf("HandleHerdrHook session with stdin error: %v", err)
	}
}

func TestHandleHerdrHookQuotaIgnoresCodexPane(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_hook_codex_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	cleared := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				_, _ = conn.Write([]byte(`{"id":"test","result":{"panes":[{"pane_id":"w1:p1","agent":"codex","title":"codex"}]}}` + "\n"))
			} else if strings.Contains(reqStr, "pane.report_metadata") || strings.Contains(reqStr, "pane.report_agent_session") {
				cleared <- reqStr
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

	if err := HandleHerdrHook(context.Background(), "quota", nil); err != nil {
		t.Fatalf("HandleHerdrHook quota error: %v", err)
	}

	select {
	case payload := <-cleared:
		if !strings.Contains(payload, `"pane_id":"w1:p1"`) {
			t.Errorf("expected clear payload to target Codex pane, got: %s", payload)
		}
		if !strings.Contains(payload, `"clear_display_agent":true`) {
			t.Errorf("expected clear payload to remove display_agent, got: %s", payload)
		}
		if !strings.Contains(payload, `"clear_title":true`) {
			t.Errorf("expected clear payload to remove title, got: %s", payload)
		}
		if !strings.Contains(payload, `"profile":null`) {
			t.Errorf("expected clear payload to remove profile token, got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("stale Codex pane metadata was not cleared")
	}
}

func TestFormatModelAbbreviation(t *testing.T) {
	tests := []struct {
		model string
		group string
		want  string
	}{
		{"gemini-2.5-pro", "Gemini Models", "gem"},
		{"gemini-2.5-flash", "Gemini Models", "gem"},
		{"claude-3-7-sonnet", "Claude and GPT models", "cld"},
		{"claude-opus-4", "Claude and GPT models", "cld"},
		{"gpt-4o", "Claude and GPT models", "gpt"},
		{"o3-mini", "Claude and GPT models", "gpt"},
		{"deepseek-r1", "DeepSeek", "dsk"},
		{"qwen-2.5-coder", "Qwen", "qwn"},
		{"", "Gemini Models", "gem"},
		{"", "Claude and GPT models", "cld"},
		{"auto", "Gemini Models", "gem"},
	}

	for _, tt := range tests {
		got := FormatModelAbbreviation(tt.model, tt.group)
		if got != tt.want {
			t.Errorf("FormatModelAbbreviation(%q, %q) = %q, want %q", tt.model, tt.group, got, tt.want)
		}
	}
}

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Display names from UI settings
		{"Claude Opus 4.6 (Thinking)", "claude-opus-4"},
		{"Claude Sonnet 4 (Thinking)", "claude-sonnet-4"},
		{"Gemini 2.5 Pro", "gemini-2.5-pro"},
		{"Gemini 2.5 Flash", "gemini-2.5-flash"},
		{"Gemini 3.7 Flash", "gemini-3.7-flash"},
		{"Gemini 3.8 Flash", "gemini-3.8-flash"},
		{"Gemini 3.8 Flash (High)", "gemini-3.8-flash"},
		{"GPT 4o", "gpt-4o"},
		// Already API-style IDs
		{"claude-opus-4", "claude-opus-4"},
		{"gemini-2.5-pro", "gemini-2.5-pro"},
		{"gpt-4o", "gpt-4o"},
		{"deepseek-r1", "deepseek-r1"},
		// Edge cases
		{"", ""},
		{"auto", "auto"},
		{"  Claude Opus 4.6 (Thinking)  ", "claude-opus-4"},
	}

	for _, tt := range tests {
		got := NormalizeModelName(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeModelName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReadSettingsModel(t *testing.T) {
	tmpDir := t.TempDir()

	// No settings file
	if got := ReadSettingsModel(tmpDir); got != "" {
		t.Errorf("Expected empty for missing settings.json, got %q", got)
	}

	// Create settings.json with model
	settingsDir := filepath.Join(tmpDir, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(settingsDir, 0700)
	settings := `{"model": "Claude Opus 4.6 (Thinking)", "enableTelemetry": false}`
	_ = os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0600)

	got := ReadSettingsModel(tmpDir)
	if got != "Claude Opus 4.6 (Thinking)" {
		t.Errorf("Expected 'Claude Opus 4.6 (Thinking)', got %q", got)
	}
}

func TestResolveActiveModel(t *testing.T) {
	tmpDir := t.TempDir()

	// Ensure in-memory cache is populated to avoid network query during test
	modelCacheLock.Lock()
	cachedModels = &DiscoveredModels{
		FetchedAt:   time.Now(),
		LatestFlash: DefaultGeminiModel,
		LatestPro:   "gemini-3.1-pro",
		AllModels:   []string{DefaultGeminiModel, "gemini-3.1-pro"},
	}
	modelCacheLock.Unlock()

	// 1. Explicit model takes highest priority
	if got := ResolveActiveModel(tmpDir, "claude-opus-4"); got != "claude-opus-4" {
		t.Errorf("Expected explicit model to take priority, got %q", got)
	}

	// 2. .active_model takes priority over settings.json
	_ = os.WriteFile(filepath.Join(tmpDir, ".active_model"), []byte("gemini-2.5-pro"), 0600)
	settingsDir := filepath.Join(tmpDir, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(settingsDir, 0700)
	_ = os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(`{"model": "Claude Opus 4.6 (Thinking)"}`), 0600)

	got := ResolveActiveModel(tmpDir, "")
	if got != "gemini-2.5-pro" {
		t.Errorf("Expected .active_model to take priority over settings.json, got %q", got)
	}

	// 3. Fallback to settings.json when no .active_model
	tmpDir2 := t.TempDir()
	settingsDir2 := filepath.Join(tmpDir2, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(settingsDir2, 0700)
	_ = os.WriteFile(filepath.Join(settingsDir2, "settings.json"), []byte(`{"model": "Claude Opus 4.6 (Thinking)"}`), 0600)

	got = ResolveActiveModel(tmpDir2, "")
	if got != "claude-opus-4" {
		t.Errorf("Expected settings.json model, got %q", got)
	}

	// 4. Fallback to default latest Gemini model when empty or literal "gemini"
	tmpDir3 := t.TempDir()
	got = ResolveActiveModel(tmpDir3, "")
	if got != DefaultGeminiModel {
		t.Errorf("Expected default latest Gemini model %q, got %q", DefaultGeminiModel, got)
	}
	_ = os.WriteFile(filepath.Join(tmpDir3, ".active_model"), []byte("gemini"), 0600)
	got = ResolveActiveModel(tmpDir3, "")
	if got != DefaultGeminiModel {
		t.Errorf("Expected literal 'gemini' cache to resolve to %q, got %q", DefaultGeminiModel, got)
	}

	// 5. "latest" and "auto" resolve to latest Gemini model
	got = ResolveActiveModel(tmpDir3, "latest")
	if got != DefaultGeminiModel {
		t.Errorf("Expected %q for 'latest', got %q", DefaultGeminiModel, got)
	}
	got = ResolveActiveModel(tmpDir3, "auto")
	if got != DefaultGeminiModel {
		t.Errorf("Expected %q for 'auto', got %q", DefaultGeminiModel, got)
	}

	// 6. Explicit "latest" overrides existing .active_model
	got = ResolveActiveModel(tmpDir, "latest")
	if got != DefaultGeminiModel {
		t.Errorf("Expected 'latest' to override .active_model %q, got %q", "gemini-2.5-pro", got)
	}

	// 7. Auto-upgrades older Gemini Flash model (e.g. gemini-3.7-flash) to latest Flash model
	tmpDir4 := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir4, ".active_model"), []byte("Gemini 3.7 Flash (High)"), 0600)
	got = ResolveActiveModel(tmpDir4, "")
	if got != DefaultGeminiModel {
		t.Errorf("Expected older Flash model to auto-upgrade to %q, got %q", DefaultGeminiModel, got)
	}
	upgradedData, _ := os.ReadFile(filepath.Join(tmpDir4, ".active_model"))
	if string(upgradedData) != DefaultGeminiModel {
		t.Errorf("Expected .active_model on disk to be upgraded to %q, got %q", DefaultGeminiModel, string(upgradedData))
	}
}

func TestReportHerdrMetadata_Compact2RowTokens(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_compact_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"Antigravity","tokens":{"profile":"compact-profile"}}]}}` + "\n"))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					received <- reqStr
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

	pDir, err := Create("compact-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	// Seed session context (35%, title, cost)
	_ = SaveSessionContext(pDir, &SessionContextState{
		UsedPercentage:    35.0,
		ModelID:           "claude-3-7-sonnet",
		ConversationTitle: "Optimize DB queries",
		Cost:              0.0042,
	})

	err = ReportHerdrMetadataWithModel(context.Background(), "compact-profile", "claude-3-7-sonnet")
	if err != nil {
		t.Fatalf("ReportHerdrMetadataWithModel error: %v", err)
	}

	select {
	case payload := <-received:
		// Line 1: Identity & Profile (without [ ])
		if !strings.Contains(payload, `"display_agent":"compact-profile"`) {
			t.Errorf("Expected display_agent to be 'compact-profile', got: %s", payload)
		}
		// Line 2: Conversation title on sidebar (replaces context window and cost)
		if !strings.Contains(payload, `"conversation_title":"Optimize DB queries"`) {
			t.Errorf("Expected tokens to contain conversation_title, got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_model_context":"Optimize DB queries"`) {
			t.Errorf("Expected payload to contain quota_model_context with conversation title, got: %s", payload)
		}
		// Title is 100% conversation title without ctx or quota
		if !strings.Contains(payload, `"title":"agys: Optimize DB queries"`) {
			t.Errorf("Expected title to be 100%% conversation title, got: %s", payload)
		}
		if !strings.Contains(payload, `"conversation_title":"Optimize DB queries"`) {
			t.Errorf("Expected tokens to contain conversation_title, got: %s", payload)
		}
		if !strings.Contains(payload, `"cost":"$0.0042"`) {
			t.Errorf("Expected tokens to contain cost, got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
	}
}

func TestReportHerdrQuotaOnly_PreservesExistingContext(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_quotaonly_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				// Pane already has a live 42% context token from inline statusline hook
				_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","title":"agys: quota-profile [cld] Ctx: 42%","tokens":{"profile":"quota-profile","model":"claude-3-7-sonnet","quota_model_context":"42% ctx · claude-3-7-sonnet","quota_context":"ctx 42%"}}]}}` + "\n"))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					received <- reqStr
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

	pDir, err := Create("quota-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	// Seed active session context (42% context and conversation title)
	_ = SaveSessionContext(pDir, &SessionContextState{
		UsedPercentage:    42.0,
		ConversationTitle: "Refactor auth middleware",
		ModelID:           "claude-3-7-sonnet",
	})

	// Call ReportHerdrQuotaOnly (simulating 60s background watcher)
	err = ReportHerdrQuotaOnly(context.Background(), "quota-profile", "claude-3-7-sonnet")
	if err != nil {
		t.Fatalf("ReportHerdrQuotaOnly error: %v", err)
	}

	select {
	case payload := <-received:
		// Verify that active session conversation title and 42% context window tokens were strictly preserved
		if !strings.Contains(payload, `"conversation_title":"Refactor auth middleware"`) {
			t.Errorf("Expected ReportHerdrQuotaOnly to preserve conversation_title, got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_model_context":"Refactor auth middleware"`) {
			t.Errorf("Expected ReportHerdrQuotaOnly to preserve quota_model_context, got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_context":"ctx 42%"`) {
			t.Errorf("Expected payload to preserve 'ctx 42%%' token, got: %s", payload)
		}
		if !strings.Contains(payload, `"title":"agys: Refactor auth middleware"`) {
			t.Errorf("Expected title in payload to be 'agys: Refactor auth middleware', got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
	}
}

func TestReportHerdrQuotaOnly_PreservesTitleWhenContextOldOrMissing(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_quotaonly_old_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 2)
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
				paneJSON := `{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","title":"agys: idle-profile","tokens":{"profile":"idle-profile","model":"gemini-2.5-pro","conversation_title":"Persistent Session Title","quota_model_context":"Persistent Session Title"}}]}}` + "\n"
				_, _ = conn.Write([]byte(paneJSON))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					received <- reqStr
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

	pDir, err := Create("idle-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	// 1. Session context state exists but is older than 2 hours (idle terminal)
	_ = SaveSessionContext(pDir, &SessionContextState{
		UsedPercentage:    25.0,
		ConversationTitle: "Persistent Session Title",
		ModelID:           "gemini-2.5-pro",
		UpdatedAt:         time.Now().Add(-3 * time.Hour),
	})

	err = ReportHerdrQuotaOnly(context.Background(), "idle-profile", "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("ReportHerdrQuotaOnly error: %v", err)
	}

	select {
	case payload := <-received:
		if !strings.Contains(payload, `"conversation_title":"Persistent Session Title"`) {
			t.Errorf("Expected conversation_title to be preserved after 2h idle, got: %s", payload)
		}
		if !strings.Contains(payload, `"title":"agys: Persistent Session Title"`) {
			t.Errorf("Expected window title to be preserved after 2h idle, got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received within timeout")
	}

	// 2. Session context file is completely missing (nil)
	_ = ResetSessionContext(pDir)

	err = ReportHerdrQuotaOnly(context.Background(), "idle-profile", "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("ReportHerdrQuotaOnly 2 error: %v", err)
	}

	select {
	case payload := <-received:
		if !strings.Contains(payload, `"conversation_title":"Persistent Session Title"`) {
			t.Errorf("Expected conversation_title from target.Tokens to be preserved when context missing, got: %s", payload)
		}
		if !strings.Contains(payload, `"title":"agys: Persistent Session Title"`) {
			t.Errorf("Expected window title from target.Title to be preserved when context missing, got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received within timeout")
	}
}

func TestReportHerdrMetadata_ClearsStaleQuotaTiers(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_tier_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
			if strings.Contains(reqStr, "pane.report_metadata") {
				received <- reqStr
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

	_, err = Create("tier-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	err = ReportHerdrMetadataWithModel(context.Background(), "tier-profile", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("ReportHerdrMetadataWithModel error: %v", err)
	}

	select {
	case payload := <-received:
		// Ensure warning and danger tokens are explicitly cleared to "" in payload to prevent Herdr from rendering stale tokens
		if !strings.Contains(payload, `"quota_5h_warning":""`) {
			t.Errorf("Expected payload to explicitly clear 'quota_5h_warning', got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_5h_danger":""`) {
			t.Errorf("Expected payload to explicitly clear 'quota_5h_danger', got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_week_warning":""`) {
			t.Errorf("Expected payload to explicitly clear 'quota_week_warning', got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_week_danger":""`) {
			t.Errorf("Expected payload to explicitly clear 'quota_week_danger', got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
	}
}

func TestSyncModelToSettings(t *testing.T) {
	tmpDir := t.TempDir()
	cliDir := filepath.Join(tmpDir, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(cliDir, 0700)
	sPath := filepath.Join(cliDir, "settings.json")
	_ = os.WriteFile(sPath, []byte(`{"model":"Claude Opus 4.6 (Thinking)","enableTelemetry":false}`), 0600)

	SyncModelToSettings(tmpDir, "gemini-3.7-flash")

	data, err := os.ReadFile(sPath)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}
	if !strings.Contains(string(data), `"model": "gemini-3.7-flash"`) {
		t.Errorf("Expected model in settings.json to be updated to gemini-3.7-flash, got: %s", string(data))
	}
}

func TestResolveProfileFromPane(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_prof_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w8:p1","terminal_title":"agys: my-active-profile [gem] Ctx: 15%","tokens":{"profile":"my-active-profile"}}]}}` + "\n"))
			}
			_ = conn.Close()
		}
	}()

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))
	got := resolveProfileFromPane(context.Background(), sockPath, "w8:p1")
	if got != "my-active-profile" {
		t.Errorf("Expected 'my-active-profile', got %q", got)
	}
}

func TestIsAgysAgent(t *testing.T) {
	tests := []struct {
		agent string
		want  bool
	}{
		{"agy", true},
		{"Agy", true},
		{"agys", true},
		{"Agys", true},
		{"antigravity", true},
		{"Antigravity", true},
		{"herdr:antigravity_cli", true},
		{"herdr:agys", true},
		{"herdr:agy", true},
		{"antigravity-cli", true},
		{"claude", false},
		{"codex", false},
		{"droid", false},
		{"opencode", false},
		{"python", false},
		{"node", false},
		{"fish", false},
		{"zsh", false},
		{"bash", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsAgysAgent(tt.agent)
		if got != tt.want {
			t.Errorf("IsAgysAgent(%q) = %v, want %v", tt.agent, got, tt.want)
		}
	}
}

func TestIsKnownNonAgysAgent(t *testing.T) {
	tests := []struct {
		agent string
		want  bool
	}{
		{"claude", true},
		{"Claude", true},
		{"codex", true},
		{"droid", true},
		{"aider", true},
		{"opencode", true},
		{"copilot", true},
		{"cursor", true},
		{"herdr:claude", true},
		{"herdr:codex", true},
		{"herdr:droid", true},
		{"zsh", false},
		{"bash", false},
		{"fish", false},
		{"sh", false},
		{"python", false},
		{"git", false},
		{"go", false},
		{"agys", false},
		{"agy", false},
		{"antigravity", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsKnownNonAgysAgent(tt.agent)
		if got != tt.want {
			t.Errorf("IsKnownNonAgysAgent(%q) = %v, want %v", tt.agent, got, tt.want)
		}
	}
}

func TestIsPaneActiveAgys(t *testing.T) {
	tests := []struct {
		agent    string
		title    string
		term     string
		stripped string
		want     bool
	}{
		// Active agys agents
		{"Antigravity", "", "fish", "", true},
		{"agy", "", "", "", true},
		{"agys", "", "", "", true},
		{"herdr:antigravity_cli", "", "zsh", "", true},
		{"zsh", "agys: khoinguyen [gem]", "", "", true},
		{"bash", "agys [khoinguyen] 5H: 100%", "", "", true},
		{"", "agys: khoinguyen [gem]", "", "", true},
		{"", "", "agys [khoinguyen] 5H: 100%", "", true},
		{"", "", "agys: khoinguyen [gem]", "", true},
		{"", "", "", "agys [prod]", true},
		// Non-agys agents & normal processes
		{"claude", "agys: khoinguyen", "agys [khoinguyen]", "", false}, // foreign agent running in dirty pane
		{"codex", "", "", "", false},
		{"droid", "", "fish", "", false},
		{"", "", "python main.py", "", false},
		{"", "", "node server.js", "", false},
		{"", "", "git status", "", false},
		{"", "", "vim README.md", "", false},
		{"", "fish", "fish", "", false},
		{"", "zsh", "zsh", "", false},
		{"", "bash", "bash", "", false},
		{"", "", "", "", false},
	}

	for _, tt := range tests {
		got := isPaneActiveAgys(tt.agent, tt.title, tt.term, tt.stripped)
		if got != tt.want {
			t.Errorf("isPaneActiveAgys(%q, %q, %q, %q) = %v, want %v", tt.agent, tt.title, tt.term, tt.stripped, got, tt.want)
		}
	}
}

func TestGetMatchingHerdrPanes_IgnoresOtherAgents(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_match_ignore_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	cleared := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				// Panes:
				// w1:p1 is running Claude (has stale agys token/title from prior session) -> MUST BE IGNORED
				// w1:p2 is running Antigravity -> MUST BE MATCHED
				// w1:p3 is clean shell with no agent and no tokens -> MUST BE IGNORED
				// w1:p4 is running python server (has stale agys token) -> MUST BE IGNORED
				// w1:p5 is running node.js app -> MUST BE IGNORED
				resp := `{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"claude","title":"agys: test-profile [gem]","tokens":{"profile":"test-profile"}},{"pane_id":"w1:p2","agent":"Antigravity","title":"agys: test-profile [gem]","tokens":{"profile":"test-profile"}},{"pane_id":"w1:p3","agent":"","title":"fish","tokens":{}},{"pane_id":"w1:p4","agent":"","terminal_title":"python app.py","tokens":{"profile":"test-profile"}},{"pane_id":"w1:p5","agent":"","terminal_title":"node server.js","tokens":{"profile":"test-profile"}}]}}`
				_, _ = conn.Write([]byte(resp + "\n"))
			} else if strings.Contains(reqStr, "pane.report_metadata") {
				_, _ = conn.Write([]byte(`{"id":"agys:clear_meta:1","result":"ok"}` + "\n"))
				cleared <- reqStr
			}
			_ = conn.Close()
		}
	}()

	matches := getMatchingHerdrPanes(context.Background(), sockPath, "", "test-profile", "gemini-2.5-pro")
	if len(matches) != 1 {
		t.Fatalf("Expected exactly 1 matched pane (Antigravity), got %d: %+v", len(matches), matches)
	}
	if matches[0].PaneID != "w1:p2" {
		t.Errorf("Expected matched pane to be 'w1:p2', got %q", matches[0].PaneID)
	}

	select {
	case payload := <-cleared:
		if !strings.Contains(payload, `"pane_id":"w1:p1"`) {
			t.Errorf("expected stale Claude metadata clear to target w1:p1, got: %s", payload)
		}
		if !strings.Contains(payload, `"clear_title":true`) {
			t.Errorf("expected stale Claude title to be cleared, got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("stale Claude metadata was not cleared")
	}
}

func TestResolveProfileFromPane_IgnoresOtherAgents(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_prof_ignore_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				// Pane w1:p1 has stale title & tokens, but agent is "claude"
				resp := `{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"claude","terminal_title":"agys: my-active-profile","tokens":{"profile":"my-active-profile"}}]}}`
				_, _ = conn.Write([]byte(resp + "\n"))
			}
			_ = conn.Close()
		}
	}()

	got := resolveProfileFromPane(context.Background(), sockPath, "w1:p1")
	if got != "" {
		t.Errorf("Expected empty profile for pane running claude, got %q", got)
	}
}

func TestClearHerdrMetadata(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_clear_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			_, _ = conn.Write([]byte(`{"id":"agys:clear_meta:1","result":"ok"}` + "\n"))
			if strings.Contains(reqStr, "pane.report_metadata") {
				received <- reqStr
			}
			_ = conn.Close()
		}
	}()

	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	t.Setenv("HERDR_SOCKET_PATH", sockPath)

	ResetTerminalTitle()

	err = ClearHerdrMetadata(context.Background())
	if err != nil {
		t.Fatalf("ClearHerdrMetadata error: %v", err)
	}

	select {
	case payload := <-received:
		if !strings.Contains(payload, `"clear_display_agent":true`) {
			t.Errorf("Expected display_agent to be cleared, got: %s", payload)
		}
		if !strings.Contains(payload, `"profile":null`) {
			t.Errorf("Expected profile token to be cleared, got: %s", payload)
		}
		if !strings.Contains(payload, `"conversation_title":null`) {
			t.Errorf("Expected conversation_title token to be cleared, got: %s", payload)
		}
		if !strings.Contains(payload, `"cost":null`) {
			t.Errorf("Expected cost token to be cleared, got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
	}
}

func TestReportHerdrMetadata_DeduplicatesUnchangedRPC(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_dedup_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	var rpcCount int
	var mu sync.Mutex

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				mu.Lock()
				count := rpcCount
				mu.Unlock()
				if count == 0 {
					// First call: initial state without full tokens
					_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"Antigravity","tokens":{"profile":"dedup-profile"}}]}}` + "\n"))
				} else {
					// Second call: pane already has the tokens that were reported in the first call
					_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"Antigravity","display_agent":"dedup-profile","title":"agys: dedup-profile","tokens":{"profile":"dedup-profile","model":"gemini-2.5-flash","conversation_title":"","quota_model_context":"","quota_5h_normal":"","quota_5h_warning":"","quota_5h_danger":"","quota_week_normal":"","quota_week_warning":"","quota_week_danger":""}}]}}` + "\n"))
				}
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					mu.Lock()
					rpcCount++
					mu.Unlock()
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

	_, err = Create("dedup-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	// First call: should send RPC
	err = ReportHerdrMetadataWithModel(context.Background(), "dedup-profile", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("ReportHerdrMetadataWithModel error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Second call with same data: must be deduplicated (no second RPC)
	err = ReportHerdrMetadataWithModel(context.Background(), "dedup-profile", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("ReportHerdrMetadataWithModel 2 error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	finalCount := rpcCount
	mu.Unlock()

	if finalCount != 1 {
		t.Errorf("Expected exactly 1 RPC sent due to deduplication, got %d", finalCount)
	}
}

func TestReportHerdrMetadata_PreservesExistingQuotaWhenDetailsUnavailable(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_preserve_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 2048)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])
			_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
			if strings.Contains(reqStr, "pane.list") {
				// Pane already has existing quota 85% 2h
				_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"Antigravity","display_agent":"pres-profile","title":"agys: pres-profile","tokens":{"profile":"pres-profile","quota_5h_normal":"85% 2h","quota_week_normal":"92% 3d"}}]}}` + "\n"))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					received <- reqStr
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

	pDir, err := Create("pres-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	// Seed session context (40%) so context window changes and triggers an update
	_ = SaveSessionContext(pDir, &SessionContextState{
		UsedPercentage: 40.0,
		ModelID:        "gemini-2.5-flash",
	})

	// Call without preloaded quota details (simulating statusline update before quota API returns)
	err = ReportHerdrMetadataWithModel(context.Background(), "pres-profile", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("ReportHerdrMetadataWithModel error: %v", err)
	}

	select {
	case payload := <-received:
		// Verify that existing quota 85% 2h was NOT erased to ""
		if !strings.Contains(payload, `"quota_5h_normal":"85% 2h"`) {
			t.Errorf("Expected payload to preserve '85%% 2h', got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_week_normal":"92% 3d"`) {
			t.Errorf("Expected payload to preserve '92%% 3d', got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
	}
}

func TestSetTerminalTitle_Deduplicates(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")

	// Redirect stderr to buffer
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	ResetTerminalTitle()

	// Call twice with identical title
	SetTerminalTitle("agys: my-title")
	SetTerminalTitle("agys: my-title")

	w.Close()
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	// Should contain escape sequence exactly once
	count := strings.Count(out, "\033]0;agys: my-title\007")
	if count != 1 {
		t.Errorf("Expected escape sequence exactly 1 time, got %d. Output: %q", count, out)
	}
}

func TestGetMatchingHerdrPanes_DoesNotReAddInactiveCurrentPane(t *testing.T) {
	panes := []HerdrRawPane{
		{
			PaneID: "w1:p1",
			Agent:  "codex",
			Title:  "codex",
		},
	}

	matches := getMatchingHerdrPanesFromList(context.Background(), panes, "", "w1:p1", "my-profile", "gemini-2.5-flash")
	if len(matches) != 0 {
		t.Errorf("Expected 0 matches for inactive current pane running codex, got %d: %+v", len(matches), matches)
	}
}

func TestReportHerdrMetadata_InitialSession_NoStaleTitleLeak(t *testing.T) {
	// macOS limits UNIX domain socket paths to 104 chars, use short path
	sockPath := fmt.Sprintf("/tmp/herdr_test_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)
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
				// Herdr pane still has stale title and tokens from a previous session
				paneJSON := `{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","title":"agys: Stale Old Prompt","tokens":{"profile":"fresh-profile","model":"claude-3-7-sonnet","conversation_title":"Stale Old Prompt","quota_model_context":"Stale Old Prompt","quota_context":"ctx 80%","cost":"$1.50"}}]}}` + "\n"
				_, _ = conn.Write([]byte(paneJSON))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					received <- reqStr
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

	_, err = Create("fresh-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	// No session context is created (fresh initial session before user enters prompt)
	err = ReportHerdrMetadataWithModel(context.Background(), "fresh-profile", "claude-3-7-sonnet")
	if err != nil {
		t.Fatalf("ReportHerdrMetadataWithModel error: %v", err)
	}

	select {
	case payload := <-received:
		// Stale title must NOT leak into the window title
		if strings.Contains(payload, "Stale Old Prompt") {
			t.Errorf("Stale title leaked into metadata payload: %s", payload)
		}
		if !strings.Contains(payload, `"title":"agys: fresh-profile"`) {
			t.Errorf("Expected window title to reset to 'agys: fresh-profile', got: %s", payload)
		}
		if !strings.Contains(payload, `"conversation_title":""`) {
			t.Errorf("Expected conversation_title token to be empty, got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_model_context":""`) {
			t.Errorf("Expected quota_model_context token to be empty, got: %s", payload)
		}
		if !strings.Contains(payload, `"quota_context":""`) {
			t.Errorf("Expected quota_context token to be empty, got: %s", payload)
		}
		if !strings.Contains(payload, `"cost":""`) {
			t.Errorf("Expected cost token to be empty, got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
	}
}

func TestReportHerdrMetadata_NeverOverwritesOtherAgentsOrPanes(t *testing.T) {
	sockPath := fmt.Sprintf("/tmp/herdr_isolate_%d.sock", time.Now().UnixNano())
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create mock unix listener: %v", err)
	}
	defer listener.Close()

	updatedPanes := make(chan string, 10)
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
				// Panes:
				// w1:p1 is current pane (agys with "Fix auth bug")
				// w1:p2 is another agent (Claude Code)
				// w1:p3 is another agys pane with "Build feature Y"
				resp := `{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","agent":"Antigravity","display_agent":"isolate-prof","title":"agys: isolate-prof","tokens":{"profile":"isolate-prof","conversation_title":"Fix auth bug"}},{"pane_id":"w1:p2","agent":"claude","title":"claude code","tokens":{"conversation_title":"Claude task"}},{"pane_id":"w1:p3","agent":"Antigravity","display_agent":"isolate-prof","title":"agys: Build feature Y","tokens":{"profile":"isolate-prof","conversation_title":"Build feature Y"}}]}}`
				_, _ = conn.Write([]byte(resp + "\n"))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					updatedPanes <- reqStr
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

	pDir, err := Create("isolate-prof")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	_ = SaveSessionContext(pDir, &SessionContextState{
		ConversationTitle: "Fix auth bug",
		ModelID:           "gemini-2.5-flash",
	})

	// Call ReportHerdrMetadataWithModel from w1:p1
	err = ReportHerdrMetadataWithModel(context.Background(), "isolate-prof", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("ReportHerdrMetadataWithModel error: %v", err)
	}

	// Verify that ONLY w1:p1 was updated, and NEVER w1:p2 (Claude) or w1:p3 (other agys pane)
	select {
	case payload := <-updatedPanes:
		if !strings.Contains(payload, `"pane_id":"w1:p1"`) {
			t.Errorf("Expected update for w1:p1, got: %s", payload)
		}
		if strings.Contains(payload, `"pane_id":"w1:p2"`) {
			t.Errorf("CRITICAL: Mistakenly updated w1:p2 (Claude agent)! Payload: %s", payload)
		}
		if strings.Contains(payload, `"pane_id":"w1:p3"`) {
			t.Errorf("CRITICAL: Mistakenly updated w1:p3 (other agys pane)! Payload: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("Timeout waiting for metadata update")
	}

	// Ensure no extra RPCs were sent to other panes
	select {
	case unexpected := <-updatedPanes:
		t.Errorf("CRITICAL: Unexpected extra RPC sent to another pane/agent: %s", unexpected)
	default:
		// No extra RPCs, perfect!
	}
}

func TestHandleHerdrSummarize_Validation(t *testing.T) {
	ctx := context.Background()
	// Should return nil on empty inputs
	if err := HandleHerdrSummarize(ctx, "", "p1", "c1", ""); err != nil {
		t.Errorf("expected nil error on empty profile, got: %v", err)
	}
	if err := HandleHerdrSummarize(ctx, "prof", "", "c1", ""); err != nil {
		t.Errorf("expected nil error on empty pane, got: %v", err)
	}
	if err := HandleHerdrSummarize(ctx, "prof", "p1", "", ""); err != nil {
		t.Errorf("expected nil error on empty conv, got: %v", err)
	}
	if err := HandleHerdrSummarize(ctx, "nonexistent-profile-xyz", "p1", "c1", ""); err != nil {
		t.Errorf("expected nil error on nonexistent profile, got: %v", err)
	}
}

func TestHandleHerdrSummarize_PaneSwitched(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w1:p1")

	pDir, err := Create("test-sum-prof")
	if err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}

	// Session is now on "conv-2"
	_ = SaveSessionContext(pDir, &SessionContextState{
		ConversationID:    "conv-2",
		ConversationTitle: "New topic",
	})

	// Try summarizing for "conv-1" which was previous conversation
	// HandleHerdrSummarize should abort and NOT overwrite the new conversation's title
	err = HandleHerdrSummarize(context.Background(), "test-sum-prof", "w1:p1", "conv-1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, ok := GetSessionContextState(pDir)
	if !ok || state.ConversationTitle != "New topic" {
		t.Errorf("expected ConversationTitle to remain 'New topic', got: %v", state.ConversationTitle)
	}
}

func TestHandleHerdrHook_InternalExecSuppression(t *testing.T) {
	t.Setenv("AGYS_INTERNAL_EXEC", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/nonexistent.sock")
	t.Setenv("HERDR_PANE_ID", "w1:p1")

	// Must return nil and not error or attempt socket operations
	err := HandleHerdrHook(context.Background(), "session", strings.NewReader(`{"conversationId":"test"}`))
	if err != nil {
		t.Errorf("expected nil error for internal exec, got %v", err)
	}

	err = HandleHerdrHook(context.Background(), "quota", nil)
	if err != nil {
		t.Errorf("expected nil error for internal exec quota, got %v", err)
	}
}

func TestCleanPromptSummary_InternalPromptsAndSlashCommands(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"[AGYS_INTERNAL_TITLE_GEN] Summarize into 3 to 5 words: fix bug", "(No prompt summary)"},
		{"[AGYS_INTERNAL_COMMIT_CHECK] You are an expert code reviewer", "(No prompt summary)"},
		{"/clear", "(No prompt summary)"},
		{"/changelog", "(No prompt summary)"},
		{"/help", "(No prompt summary)"},
		{"/ask help me deploy the application", "Help me deploy the application"},
		{"<USER_REQUEST>\nfix database deadlock in transaction\n</USER_REQUEST>", "Fix database deadlock in transaction"},
	}

	for _, tc := range tests {
		got := cleanPromptSummary(tc.input)
		if got != tc.expected {
			t.Errorf("cleanPromptSummary(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestResolveConversationTitleFromTranscript_LongLinesAndSlashCommands(t *testing.T) {
	tmpDir := t.TempDir()
	transcriptPath := filepath.Join(tmpDir, "transcript.jsonl")

	// Create transcript with:
	// Line 1: /changelog (standalone slash command, should be skipped)
	// Line 2: Giant tool output (> 600KB) which would break bufio.Scanner
	// Line 3: Real user prompt
	giantToolOutput := strings.Repeat("x", 700*1024)
	content := fmt.Sprintf(`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"/changelog"}`+"\n"+
		`{"step_index":1,"source":"MODEL","type":"GENERIC","status":"DONE","content":%q}`+"\n"+
		`{"step_index":2,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"<USER_REQUEST>\nImplement user authentication with JWT\n</USER_REQUEST>"}`+"\n", giantToolOutput)

	if err := os.WriteFile(transcriptPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test transcript: %v", err)
	}

	title := ResolveConversationTitleFromTranscript(transcriptPath)
	expected := "Implement user authentication with JWT"
	if title != expected {
		t.Errorf("expected title %q, got %q", expected, title)
	}
}



