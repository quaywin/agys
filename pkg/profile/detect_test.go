package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindProfileByConversation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	p1 := "work"
	p2 := "personal"
	dir1, _ := Create(p1)
	dir2, _ := Create(p2)

	convID1 := "conv-111"
	convID2 := "conv-222"

	// Create conversation directories
	_ = os.MkdirAll(filepath.Join(dir1, ".gemini", "antigravity-cli", "brain", convID1), 0700)
	_ = os.MkdirAll(filepath.Join(dir2, ".gemini", "antigravity-cli", "brain", convID2), 0700)

	// Test lookups
	found1, err := FindProfileByConversation(convID1)
	if err != nil || found1 != p1 {
		t.Errorf("Expected to find %q in %q, got %q (err: %v)", convID1, p1, found1, err)
	}

	found2, err := FindProfileByConversation(convID2)
	if err != nil || found2 != p2 {
		t.Errorf("Expected to find %q in %q, got %q (err: %v)", convID2, p2, found2, err)
	}

	// Test non-existent conversation
	foundNone, err := FindProfileByConversation("non-existent")
	if err != nil || foundNone != "" {
		t.Errorf("Expected empty result for non-existent conversation, got %q (err: %v)", foundNone, err)
	}
}

func TestFindProfileByLatestConversation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	p1 := "work"
	p2 := "personal"
	dir1, _ := Create(p1)
	dir2, _ := Create(p2)

	// Setup brain folders
	brain1 := filepath.Join(dir1, ".gemini", "antigravity-cli", "brain", "conv-work")
	brain2 := filepath.Join(dir2, ".gemini", "antigravity-cli", "brain", "conv-pers")
	_ = os.MkdirAll(brain1, 0700)
	_ = os.MkdirAll(brain2, 0700)

	// Write transcript files and manipulate mod time
	file1 := filepath.Join(brain1, ".system_generated", "logs", "transcript.jsonl")
	file2 := filepath.Join(brain2, ".system_generated", "logs", "transcript.jsonl")
	_ = os.MkdirAll(filepath.Dir(file1), 0700)
	_ = os.MkdirAll(filepath.Dir(file2), 0700)

	_ = os.WriteFile(file1, []byte("{}"), 0600)
	_ = os.WriteFile(file2, []byte("{}"), 0600)

	// Make file2 newer than file1
	now := time.Now()
	_ = os.Chtimes(file1, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	_ = os.Chtimes(file2, now, now)

	latest, err := FindProfileByLatestConversation()
	if err != nil || latest != p2 {
		t.Errorf("Expected latest profile to be %q, got %q (err: %v)", p2, latest, err)
	}

	// Make file1 newer than file2
	_ = os.Chtimes(file1, now.Add(10*time.Minute), now.Add(10*time.Minute))

	latest, err = FindProfileByLatestConversation()
	if err != nil || latest != p1 {
		t.Errorf("Expected latest profile to be %q, got %q (err: %v)", p1, latest, err)
	}
}

func TestSaveGetLastConversation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	// Test default state: empty cache
	id, err := GetLastConversation()
	if err != nil {
		t.Fatalf("GetLastConversation failed: %v", err)
	}
	if id != "" {
		t.Errorf("Expected empty last conversation, got %q", id)
	}

	// Test save
	testID := "test-conv-abc"
	if err := SaveLastConversation(testID); err != nil {
		t.Fatalf("SaveLastConversation failed: %v", err)
	}

	// Test read back
	id, err = GetLastConversation()
	if err != nil {
		t.Fatalf("GetLastConversation failed: %v", err)
	}
	if id != testID {
		t.Errorf("Expected conversation ID %q, got %q", testID, id)
	}
}
