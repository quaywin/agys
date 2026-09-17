package profile

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSessionCacheLoadSave(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	// Test loading when cache doesn't exist
	cache, err := LoadSessionCache()
	if err != nil {
		t.Fatalf("expected no error loading empty cache, got: %v", err)
	}
	if len(cache) != 0 {
		t.Fatalf("expected empty cache, got %d items", len(cache))
	}

	// Save an item to cache
	now := time.Now().Truncate(time.Second)
	cache["store:test-conv-1"] = CachedSessionInfo{
		Profile:         "store",
		ConvID:          "test-conv-1",
		ModTime:         now,
		ProjectPath:     "/test/project",
		ProjectName:     "project",
		UserPrompt:      "hello world",
		TranscriptMTime: now.UnixNano(),
		TranscriptSize:  1024,
	}

	if err := SaveSessionCache(cache); err != nil {
		t.Fatalf("failed to save session cache: %v", err)
	}

	// Reload cache
	loaded, err := LoadSessionCache()
	if err != nil {
		t.Fatalf("failed to reload session cache: %v", err)
	}

	item, exists := loaded["store:test-conv-1"]
	if !exists {
		t.Fatalf("expected item 'store:test-conv-1' in loaded cache")
	}
	if item.Profile != "store" || item.ConvID != "test-conv-1" || item.ProjectName != "project" || item.UserPrompt != "hello world" {
		t.Errorf("loaded item mismatch: %+v", item)
	}
}

func TestPruneSessionCache(t *testing.T) {
	cache := make(SessionCache)
	cache["p1:conv-1"] = CachedSessionInfo{Profile: "p1", ConvID: "conv-1"}
	cache["p1:conv-2"] = CachedSessionInfo{Profile: "p1", ConvID: "conv-2"}
	cache["p2:conv-3"] = CachedSessionInfo{Profile: "p2", ConvID: "conv-3"}

	validKeys := map[string]bool{
		"p1:conv-1": true,
		"p2:conv-3": true,
	}

	pruned := PruneSessionCache(cache, validKeys)
	if pruned != 1 {
		t.Errorf("expected 1 item pruned, got %d", pruned)
	}
	if _, exists := cache["p1:conv-2"]; exists {
		t.Errorf("expected p1:conv-2 to be pruned")
	}
	if len(cache) != 2 {
		t.Errorf("expected 2 items remaining, got %d", len(cache))
	}
}

func TestUpdateSessionCache(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	err := UpdateSessionCache(func(diskCache SessionCache) error {
		diskCache["p1:conv-1"] = CachedSessionInfo{
			Profile:     "p1",
			ConvID:      "conv-1",
			ProjectName: "(Global)",
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateSessionCache failed: %v", err)
	}

	loaded, err := LoadSessionCache()
	if err != nil {
		t.Fatalf("LoadSessionCache failed: %v", err)
	}
	if item, exists := loaded["p1:conv-1"]; !exists || item.ProjectName != "(Global)" {
		t.Errorf("expected global session in cache, got %+v", item)
	}
}

