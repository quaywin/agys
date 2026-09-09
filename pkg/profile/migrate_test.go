package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateConversation_Success(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	srcProfile := "profile-src"
	destProfile := "profile-dst"

	srcDir, err := Create(srcProfile)
	if err != nil {
		t.Fatalf("failed to create src profile: %v", err)
	}
	destDir, err := Create(destProfile)
	if err != nil {
		t.Fatalf("failed to create dest profile: %v", err)
	}

	convID := "test-conv-999"

	// Setup brain directory in srcProfile
	srcBrainDir := filepath.Join(srcDir, ".gemini", "antigravity-cli", "brain", convID)
	srcLogsDir := filepath.Join(srcBrainDir, ".system_generated", "logs")
	if err := os.MkdirAll(srcLogsDir, 0700); err != nil {
		t.Fatalf("failed to create src brain dir: %v", err)
	}

	transcriptContent := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"hello world"}` + "\n"
	transcriptPath := filepath.Join(srcLogsDir, "transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcriptContent), 0600); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	artifactContent := "# Project Report\nEverything is working."
	artifactPath := filepath.Join(srcBrainDir, "report.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("failed to write artifact: %v", err)
	}

	// Setup history.jsonl in srcProfile
	historyLine := fmt.Sprintf(`{"display":"hello world","timestamp":1784550487866,"workspace":"/workspace/test","conversationId":"%s"}`+"\n", convID)
	otherHistoryLine := `{"display":"other conversation","timestamp":1784550000000,"workspace":"/workspace/other","conversationId":"other-conv-111"}` + "\n"
	srcHistoryPath := filepath.Join(srcDir, ".gemini", "antigravity-cli", "history.jsonl")
	if err := os.WriteFile(srcHistoryPath, []byte(otherHistoryLine+historyLine), 0600); err != nil {
		t.Fatalf("failed to write src history: %v", err)
	}

	// Verify before migration: conversation belongs to srcProfile
	ownerBefore, err := FindProfileByConversation(convID)
	if err != nil || ownerBefore != srcProfile {
		t.Fatalf("expected owner before migration to be %q, got %q (err: %v)", srcProfile, ownerBefore, err)
	}

	// Run migration
	if err := MigrateConversation(convID, srcProfile, destProfile); err != nil {
		t.Fatalf("MigrateConversation failed: %v", err)
	}

	// Verify after migration:
	// 1. Source brain directory is gone
	if _, err := os.Stat(srcBrainDir); !os.IsNotExist(err) {
		t.Errorf("expected src brain dir to be removed, but still exists")
	}

	// 2. Dest brain directory exists with files
	destBrainDir := filepath.Join(destDir, ".gemini", "antigravity-cli", "brain", convID)
	destTranscript := filepath.Join(destBrainDir, ".system_generated", "logs", "transcript.jsonl")
	data, err := os.ReadFile(destTranscript)
	if err != nil || string(data) != transcriptContent {
		t.Errorf("dest transcript content mismatch: %s (err: %v)", string(data), err)
	}

	destArtifact := filepath.Join(destBrainDir, "report.md")
	data, err = os.ReadFile(destArtifact)
	if err != nil || string(data) != artifactContent {
		t.Errorf("dest artifact content mismatch: %s (err: %v)", string(data), err)
	}

	// 3. Dest history.jsonl has the matching entry
	destHistoryPath := filepath.Join(destDir, ".gemini", "antigravity-cli", "history.jsonl")
	destHistData, err := os.ReadFile(destHistoryPath)
	if err != nil || string(destHistData) != historyLine {
		t.Errorf("dest history.jsonl mismatch: expected %q, got %q (err: %v)", historyLine, string(destHistData), err)
	}

	// 4. FindProfileByConversation now returns destProfile
	ownerAfter, err := FindProfileByConversation(convID)
	if err != nil || ownerAfter != destProfile {
		t.Errorf("expected owner after migration to be %q, got %q (err: %v)", destProfile, ownerAfter, err)
	}

	// 5. Last active conversation is updated
	lastConv, err := GetLastConversation()
	if err != nil || lastConv != convID {
		t.Errorf("expected last conversation to be %q, got %q (err: %v)", convID, lastConv, err)
	}
}

func TestMigrateConversation_Validation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	_, _ = Create("p1")
	_, _ = Create("p2")

	// Empty convID
	if err := MigrateConversation("", "p1", "p2"); err == nil {
		t.Errorf("expected error for empty convID, got nil")
	}

	// Empty src or dest
	if err := MigrateConversation("c1", "", "p2"); err == nil {
		t.Errorf("expected error for empty srcProfile, got nil")
	}
	if err := MigrateConversation("c1", "p1", ""); err == nil {
		t.Errorf("expected error for empty destProfile, got nil")
	}

	// Same src and dest
	if err := MigrateConversation("c1", "p1", "p1"); err != nil {
		t.Errorf("expected nil for same src and dest, got %v", err)
	}

	// Non-existent conversation
	if err := MigrateConversation("non-existent-conv", "p1", "p2"); err == nil {
		t.Errorf("expected error for non-existent conversation, got nil")
	}
}

func TestFindProfileAndConvByLatestConversation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	p1 := "work"
	p2 := "personal"
	dir1, _ := Create(p1)
	dir2, _ := Create(p2)

	brain1 := filepath.Join(dir1, ".gemini", "antigravity-cli", "brain", "conv-work")
	brain2 := filepath.Join(dir2, ".gemini", "antigravity-cli", "brain", "conv-pers")
	_ = os.MkdirAll(filepath.Join(brain1, ".system_generated", "logs"), 0700)
	_ = os.MkdirAll(filepath.Join(brain2, ".system_generated", "logs"), 0700)

	file1 := filepath.Join(brain1, ".system_generated", "logs", "transcript.jsonl")
	file2 := filepath.Join(brain2, ".system_generated", "logs", "transcript.jsonl")
	_ = os.WriteFile(file1, []byte("{}"), 0600)
	_ = os.WriteFile(file2, []byte("{}"), 0600)

	now := time.Now()
	_ = os.Chtimes(file1, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	_ = os.Chtimes(file2, now, now)

	// file2 is newer -> should be p2, conv-pers
	prof, conv, err := FindProfileAndConvByLatestConversation()
	if err != nil || prof != p2 || conv != "conv-pers" {
		t.Errorf("expected (%q, %q), got (%q, %q, err: %v)", p2, "conv-pers", prof, conv, err)
	}

	// file1 newer -> should be p1, conv-work
	_ = os.Chtimes(file1, now.Add(10*time.Minute), now.Add(10*time.Minute))
	prof, conv, err = FindProfileAndConvByLatestConversation()
	if err != nil || prof != p1 || conv != "conv-work" {
		t.Errorf("expected (%q, %q), got (%q, %q, err: %v)", p1, "conv-work", prof, conv, err)
	}
}
