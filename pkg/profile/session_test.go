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

func TestListSessionsEmpty(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	ctx := context.Background()
	sessions, err := ListSessions(ctx, SessionFilter{Project: "nonexistent-project-xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}
