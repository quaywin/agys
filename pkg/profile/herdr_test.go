package profile

import (
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
				_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","tokens":{"profile":"my-test-profile"}}]}}` + "\n"))
			} else {
				_, _ = conn.Write([]byte(`{"id":"agys:metadata:1","result":"ok"}` + "\n"))
				if strings.Contains(reqStr, "pane.report_metadata") {
					received <- reqStr
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

	// 4. Fallback to default "gemini"
	tmpDir3 := t.TempDir()
	got = ResolveActiveModel(tmpDir3, "")
	if got != "gemini" {
		t.Errorf("Expected default 'gemini', got %q", got)
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
				_, _ = conn.Write([]byte(`{"id":"agys:panes:1","result":{"panes":[{"pane_id":"w1:p1","tokens":{"profile":"compact-profile"}}]}}` + "\n"))
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

	// Seed session context (35%)
	_ = SaveSessionContext(pDir, &SessionContextState{
		UsedPercentage: 35.0,
		ModelID:        "claude-3-7-sonnet",
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
		// Line 2: % ctx + Full Model ID
		if !strings.Contains(payload, "35% ctx · claude-3-7-sonnet") {
			t.Errorf("Expected payload to contain '35%% ctx · claude-3-7-sonnet', got: %s", payload)
		}
		if !strings.Contains(payload, "quota_model_context") {
			t.Errorf("Expected payload to contain quota_model_context token, got: %s", payload)
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

	_, err = Create("quota-profile")
	if err != nil {
		t.Fatalf("Create profile error: %v", err)
	}

	// Call ReportHerdrQuotaOnly (simulating 60s background watcher)
	err = ReportHerdrQuotaOnly(context.Background(), "quota-profile", "claude-3-7-sonnet")
	if err != nil {
		t.Fatalf("ReportHerdrQuotaOnly error: %v", err)
	}

	select {
	case payload := <-received:
		// Verify that existing 42% context window was strictly preserved
		if !strings.Contains(payload, "42% ctx · claude-3-7-sonnet") {
			t.Errorf("Expected ReportHerdrQuotaOnly to preserve '42%% ctx · claude-3-7-sonnet', got: %s", payload)
		}
		if !strings.Contains(payload, "Ctx: 42%") {
			t.Errorf("Expected title in payload to preserve 'Ctx: 42%%', got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
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
		{"antigravity", true},
		{"Antigravity", true},
		{"herdr:antigravity_cli", true},
		{"antigravity-cli", true},
		{"claude", false},
		{"codex", false},
		{"droid", false},
		{"opencode", false},
		{"python", false},
		{"node", false},
		{"fish", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsAgysAgent(tt.agent)
		if got != tt.want {
			t.Errorf("IsAgysAgent(%q) = %v, want %v", tt.agent, got, tt.want)
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
		{"herdr:antigravity_cli", "", "zsh", "", true},
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
		if !strings.Contains(payload, `"display_agent":""`) {
			t.Errorf("Expected display_agent to be cleared, got: %s", payload)
		}
		if !strings.Contains(payload, `"profile":""`) {
			t.Errorf("Expected profile token to be cleared, got: %s", payload)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("No payload received on mock socket within timeout")
	}
}


