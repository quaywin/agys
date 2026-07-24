package profile

import (
	"context"
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

func TestListSessionsEmpty(t *testing.T) {
	ctx := context.Background()
	sessions, err := ListSessions(ctx, SessionFilter{Project: "nonexistent-project-xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}
