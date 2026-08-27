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
