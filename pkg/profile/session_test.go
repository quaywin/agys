package profile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindProjectRoot(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "agys-test-proj-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	subDir := filepath.Join(tempDir, "src", "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create sub dir: %v", err)
	}

	gitDir := filepath.Join(tempDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	testFile := filepath.Join(subDir, "main.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	root := FindProjectRoot(testFile)
	if root != tempDir {
		t.Errorf("expected project root %s, got %s", tempDir, root)
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()
	if s := FormatRelativeTime(now); s != "just now" {
		t.Errorf("expected 'just now', got %q", s)
	}
	if s := FormatRelativeTime(now.Add(-10 * time.Minute)); s != "10 mins ago" {
		t.Errorf("expected '10 mins ago', got %q", s)
	}
	if s := FormatRelativeTime(now.Add(-2 * time.Hour)); s != "2 hours ago" {
		t.Errorf("expected '2 hours ago', got %q", s)
	}
	if s := FormatRelativeTime(now.Add(-48 * time.Hour)); s != "2 days ago" {
		t.Errorf("expected '2 days ago', got %q", s)
	}
}

func TestListSessionsWithCache(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	// Create profile structure
	profileName := "testprof"
	baseDir, _ := GetBaseDir()
	profDir := filepath.Join(baseDir, profileName)
	brainDir := filepath.Join(profDir, ".gemini", "antigravity-cli", "brain")
	convID := "conv-12345"
	convDir := filepath.Join(brainDir, convID)
	logsDir := filepath.Join(convDir, ".system_generated", "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}

	// Create a dummy project root
	dummyProj := filepath.Join(tempHome, "mycoolproject")
	if err := os.MkdirAll(filepath.Join(dummyProj, ".git"), 0755); err != nil {
		t.Fatalf("failed to create dummy proj: %v", err)
	}

	transcriptContent := fmt.Sprintf(`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-08-20T10:00:00Z","content":"<USER_REQUEST>\nFix login bug\n</USER_REQUEST>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"view_file","args":{"AbsolutePath":"%s/main.go"}}]}
`, dummyProj)

	transcriptPath := filepath.Join(logsDir, "transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcriptContent), 0644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	ctx := context.Background()

	// First run: parses transcript and creates cache
	sessions, err := ListSessions(ctx, SessionFilter{})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ConvID != convID || sessions[0].ProjectName != "mycoolproject" || sessions[0].UserPrompt != "Fix login bug" {
		t.Errorf("unexpected session data: %+v", sessions[0])
	}

	// Verify cache was saved
	cache, err := LoadSessionCache()
	if err != nil {
		t.Fatalf("failed to load session cache: %v", err)
	}
	if len(cache) != 1 {
		t.Fatalf("expected 1 cached item, got %d", len(cache))
	}

	// Second run: should hit cache
	cachedSessions, err := ListSessions(ctx, SessionFilter{})
	if err != nil {
		t.Fatalf("second ListSessions failed: %v", err)
	}
	if len(cachedSessions) != 1 || cachedSessions[0].ConvID != convID {
		t.Fatalf("expected 1 session from cache, got %+v", cachedSessions)
	}

	// Test filter by project name
	projSessions, err := ListSessions(ctx, SessionFilter{Project: "mycoolproject"})
	if err != nil || len(projSessions) != 1 {
		t.Fatalf("expected 1 session matching project filter, got %d (err: %v)", len(projSessions), err)
	}

	nonMatchingSessions, err := ListSessions(ctx, SessionFilter{Project: "nonexistent"})
	if err != nil || len(nonMatchingSessions) != 0 {
		t.Fatalf("expected 0 sessions matching nonexistent filter, got %d", len(nonMatchingSessions))
	}
}

func TestIsInternalAutomatedSession(t *testing.T) {
	cases := []struct {
		prompt   string
		expected bool
	}{
		{
			prompt:   "Fix login bug in auth handler",
			expected: false,
		},
		{
			prompt:   "Please write a Conventional Commit message for these changes",
			expected: false,
		},
		{
			prompt:   "Analyze the staged changes in my project",
			expected: false,
		},
		{
			prompt:   "CHECK_SUMMARY: should not break if user mentions it",
			expected: false,
		},
		{
			prompt:   "[AGYS_INTERNAL_COMMIT_CHECK] You are an expert software developer and Git assistant.\nFormatting rules:...",
			expected: true,
		},
		{
			prompt:   "You are an expert software developer and Git assistant.\nFormatting rules:...",
			expected: true,
		},
	}

	for _, c := range cases {
		got := IsInternalAutomatedSession(c.prompt)
		if got != c.expected {
			t.Errorf("IsInternalAutomatedSession(%q) = %v, expected %v", c.prompt, got, c.expected)
		}
	}
}

func TestListSessionsEmpty(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	ctx := context.Background()
	sessions, err := ListSessions(ctx, SessionFilter{Project: "nonexistent-project-xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestMatchProject(t *testing.T) {
	sess := ConversationSession{
		ProjectName: "agys",
		ProjectPath: "/Volumes/QUAYWIN/Projects_1/agys",
	}

	// 1. Direct name match
	if !MatchProject("agys", sess) {
		t.Errorf("expected 'agys' to match sess")
	}
	if !MatchProject("AGYS", sess) {
		t.Errorf("expected 'AGYS' to match sess")
	}

	// 2. Full path match
	if !MatchProject("/Volumes/QUAYWIN/Projects_1/agys", sess) {
		t.Errorf("expected full path to match sess")
	}

	// 3. Subdirectory filter match
	if !MatchProject("/Volumes/QUAYWIN/Projects_1/agys/cmd", sess) {
		t.Errorf("expected subfolder path to match sess")
	}

	// 4. Non-matching project
	if MatchProject("caudata", sess) {
		t.Errorf("expected 'caudata' NOT to match 'agys'")
	}
	if MatchProject("/Volumes/QUAYWIN/Projects_1/caudata", sess) {
		t.Errorf("expected caudata path NOT to match 'agys'")
	}

	// 5. Empty filter matches everything
	if !MatchProject("", sess) {
		t.Errorf("expected empty filter to match sess")
	}
}

func TestListSessionsFromHistoryJsonl(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profileName := "histprof"
	baseDir, _ := GetBaseDir()
	profDir := filepath.Join(baseDir, profileName)
	cliDir := filepath.Join(profDir, ".gemini", "antigravity-cli")
	brainDir := filepath.Join(cliDir, "brain")
	convID := "conv-history-999"
	convDir := filepath.Join(brainDir, convID)
	logsDir := filepath.Join(convDir, ".system_generated", "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}

	dummyProj := filepath.Join(tempHome, "myhistoryproj")
	if err := os.MkdirAll(filepath.Join(dummyProj, ".git"), 0755); err != nil {
		t.Fatalf("failed to create dummy proj: %v", err)
	}

	// Create history.jsonl with workspace mapping
	historyContent := fmt.Sprintf(`{"display":"improve resume command","timestamp":1784550487866,"workspace":"%s","conversationId":"%s"}
`, dummyProj, convID)
	if err := os.WriteFile(filepath.Join(cliDir, "history.jsonl"), []byte(historyContent), 0644); err != nil {
		t.Fatalf("failed to write history.jsonl: %v", err)
	}

	transcriptContent := fmt.Sprintf(`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-08-20T10:00:00Z","content":"<USER_REQUEST>\nimprove resume command\n</USER_REQUEST>"}
`)
	if err := os.WriteFile(filepath.Join(logsDir, "transcript.jsonl"), []byte(transcriptContent), 0644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	ctx := context.Background()
	sessions, err := ListSessions(ctx, SessionFilter{Project: "myhistoryproj"})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session from history.jsonl, got %d", len(sessions))
	}
	if sessions[0].ProjectName != "myhistoryproj" || sessions[0].ProjectPath != dummyProj {
		t.Errorf("unexpected session data: %+v", sessions[0])
	}
}

func TestListSessionsVolumesPath(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	profileName := "volprof"
	baseDir, _ := GetBaseDir()
	profDir := filepath.Join(baseDir, profileName)
	brainDir := filepath.Join(profDir, ".gemini", "antigravity-cli", "brain")
	convID := "conv-vol-111"
	convDir := filepath.Join(brainDir, convID)
	logsDir := filepath.Join(convDir, ".system_generated", "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}

	// Simulated transcript with /Volumes/ path in tool calls and user information
	transcriptContent := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-08-20T10:00:00Z","content":"<USER_REQUEST>\ncheck volumes\n</USER_REQUEST>\n<user_information>\n/Volumes/QUAYWIN/Projects_1/agys -> quaywin/agys\n</user_information>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"view_file","args":{"AbsolutePath":"/Volumes/QUAYWIN/Projects_1/agys/cmd/resume.go"}}]}
`
	if err := os.WriteFile(filepath.Join(logsDir, "transcript.jsonl"), []byte(transcriptContent), 0644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	ctx := context.Background()
	sessions, err := ListSessions(ctx, SessionFilter{Project: "agys"})
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session matching 'agys', got %d", len(sessions))
	}
	if sessions[0].ProjectName != "agys" {
		t.Errorf("expected ProjectName 'agys', got %q", sessions[0].ProjectName)
	}
}
