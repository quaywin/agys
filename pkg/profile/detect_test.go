package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindProfileByConversation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	p1 := "work"
	p2 := "personal"
	dir1, _ := Create(p1)
	dir2, _ := Create(p2)

	convID1 := "conv-111"
	convID2 := "conv-222"
	convID3 := "conv-333"

	// Create conversation directories in both antigravity-cli and antigravity brain paths
	_ = os.MkdirAll(filepath.Join(dir1, ".gemini", "antigravity-cli", "brain", convID1), 0700)
	_ = os.MkdirAll(filepath.Join(dir2, ".gemini", "antigravity-cli", "brain", convID2), 0700)
	_ = os.MkdirAll(filepath.Join(dir1, ".gemini", "antigravity", "brain", convID3), 0700)

	// Test lookups
	found1, err := FindProfileByConversation(convID1)
	if err != nil || found1 != p1 {
		t.Errorf("Expected to find %q in %q, got %q (err: %v)", convID1, p1, found1, err)
	}

	found2, err := FindProfileByConversation(convID2)
	if err != nil || found2 != p2 {
		t.Errorf("Expected to find %q in %q, got %q (err: %v)", convID2, p2, found2, err)
	}

	found3, err := FindProfileByConversation(convID3)
	if err != nil || found3 != p1 {
		t.Errorf("Expected to find %q in %q, got %q (err: %v)", convID3, p1, found3, err)
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
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

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
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

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

func TestSaveGetSessionFlags(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	convID := "conv-test-flags-123"
	flags := []string{"--dangerously-skip-permissions", "--model=pro"}

	// Initially empty
	got, err := GetSessionFlags(convID)
	if err != nil {
		t.Fatalf("GetSessionFlags failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Expected empty flags, got %v", got)
	}

	// Save flags
	if err := SaveSessionFlags(convID, flags); err != nil {
		t.Fatalf("SaveSessionFlags failed: %v", err)
	}

	// Retrieve flags
	got, err = GetSessionFlags(convID)
	if err != nil {
		t.Fatalf("GetSessionFlags failed: %v", err)
	}
	if len(got) != 2 || got[0] != flags[0] || got[1] != flags[1] {
		t.Errorf("Expected flags %v, got %v", flags, got)
	}
}

func TestFindProfileAndConvByLatestConversationInWorkspace(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	p1 := "work"
	p2 := "personal"
	dir1, _ := Create(p1)
	dir2, _ := Create(p2)

	projA := filepath.Join(tempHome, "projects", "projA")
	projB := filepath.Join(tempHome, "projects", "projB")
	_ = os.MkdirAll(projA, 0700)
	_ = os.MkdirAll(projB, 0700)

	convID1 := "conv-projA-123"
	convID2 := "conv-projB-456"

	brain1 := filepath.Join(dir1, ".gemini", "antigravity-cli", "brain", convID1)
	brain2 := filepath.Join(dir2, ".gemini", "antigravity-cli", "brain", convID2)
	_ = os.MkdirAll(filepath.Join(brain1, ".system_generated", "logs"), 0700)
	_ = os.MkdirAll(filepath.Join(brain2, ".system_generated", "logs"), 0700)

	// Mock transcript with workspace URI in <user_information>
	t1 := fmt.Sprintf(`{"step":1,"content":"<user_information>\n[URI] -> [CorpusName]:\n%s -> projA\n</user_information>\n<USER_REQUEST>task A</USER_REQUEST>"}`+"\n", projA)
	t2 := fmt.Sprintf(`{"step":1,"content":"<user_information>\n[URI] -> [CorpusName]:\n%s -> projB\n</user_information>\n<USER_REQUEST>task B</USER_REQUEST>"}`+"\n", projB)

	f1 := filepath.Join(brain1, ".system_generated", "logs", "transcript.jsonl")
	f2 := filepath.Join(brain2, ".system_generated", "logs", "transcript.jsonl")
	_ = os.WriteFile(f1, []byte(t1), 0600)
	_ = os.WriteFile(f2, []byte(t2), 0600)

	// Make conv2 (personal, projB) newer than conv1 (work, projA)
	now := time.Now()
	_ = os.Chtimes(f1, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	_ = os.Chtimes(f2, now, now)

	// 1. Scoped to projA: should return work (p1) even though projB is newer globally
	profA, cidA, err := FindProfileAndConvByLatestConversationInWorkspace(projA)
	if err != nil {
		t.Fatalf("FindProfileAndConvByLatestConversationInWorkspace failed: %v", err)
	}
	if profA != p1 || cidA != convID1 {
		t.Errorf("Expected (%s, %s) for projA, got (%s, %s)", p1, convID1, profA, cidA)
	}

	// 2. Scoped to projB: should return personal (p2)
	profB, cidB, err := FindProfileAndConvByLatestConversationInWorkspace(projB)
	if err != nil {
		t.Fatalf("FindProfileAndConvByLatestConversationInWorkspace failed: %v", err)
	}
	if profB != p2 || cidB != convID2 {
		t.Errorf("Expected (%s, %s) for projB, got (%s, %s)", p2, convID2, profB, cidB)
	}

	// 3. Unknown workspace: should fall back to globally latest (personal, convID2)
	unknownDir := filepath.Join(tempHome, "projects", "unknown")
	profU, cidU, err := FindProfileAndConvByLatestConversationInWorkspace(unknownDir)
	if err != nil {
		t.Fatalf("FindProfileAndConvByLatestConversationInWorkspace failed: %v", err)
	}
	if profU != p2 || cidU != convID2 {
		t.Errorf("Expected fallback to (%s, %s) for unknown workspace, got (%s, %s)", p2, convID2, profU, cidU)
	}
}
