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

func TestToSuperscriptPercent(t *testing.T) {
	if got := ToSuperscriptPercent(87); got != "⁸⁷%" {
		t.Errorf("expected ⁸⁷%%, got %s", got)
	}
	if got := ToSuperscriptPercent(100); got != "¹⁰⁰%" {
		t.Errorf("expected ¹⁰⁰%%, got %s", got)
	}
	if got := ToSuperscriptPercent(0); got != "⁰%" {
		t.Errorf("expected ⁰%%, got %s", got)
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
