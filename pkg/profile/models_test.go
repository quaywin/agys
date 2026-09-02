package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractGeminiVersion(t *testing.T) {
	tests := []struct {
		model string
		wantV GeminiModelVersion
		ok    bool
	}{
		{"gemini-3.8-flash", GeminiModelVersion{3, 8, 0}, true},
		{"gemini-3.10-flash", GeminiModelVersion{3, 10, 0}, true},
		{"gemini-4.0-flash", GeminiModelVersion{4, 0, 0}, true},
		{"gemini-2.5-flash", GeminiModelVersion{2, 5, 0}, true},
		{"gemini-3.1-pro", GeminiModelVersion{3, 1, 0}, true},
		{"claude-sonnet-4-6", GeminiModelVersion{}, false},
		{"gpt-4o", GeminiModelVersion{}, false},
	}

	for _, tt := range tests {
		got, ok := ExtractGeminiVersion(tt.model)
		if ok != tt.ok {
			t.Errorf("ExtractGeminiVersion(%q) ok = %v, want %v", tt.model, ok, tt.ok)
		}
		if got != tt.wantV {
			t.Errorf("ExtractGeminiVersion(%q) = %v, want %v", tt.model, got, tt.wantV)
		}
	}
}

func TestGeminiModelVersion_GreaterThan(t *testing.T) {
	v4_0 := GeminiModelVersion{4, 0, 0}
	v3_10 := GeminiModelVersion{3, 10, 0}
	v3_9 := GeminiModelVersion{3, 9, 0}
	v3_8 := GeminiModelVersion{3, 8, 0}
	v2_5 := GeminiModelVersion{2, 5, 0}

	if !v4_0.GreaterThan(v3_10) {
		t.Errorf("expected 4.0 > 3.10")
	}
	if !v3_10.GreaterThan(v3_9) {
		t.Errorf("expected 3.10 > 3.9")
	}
	if !v3_9.GreaterThan(v3_8) {
		t.Errorf("expected 3.9 > 3.8")
	}
	if !v3_8.GreaterThan(v2_5) {
		t.Errorf("expected 3.8 > 2.5")
	}
	if v2_5.GreaterThan(v3_8) {
		t.Errorf("expected 2.5 NOT > 3.8")
	}
}

func TestParseAgyModelsOutput_OrderIndependent(t *testing.T) {
	// Sample with future/arbitrary order (3.7 first, 4.0 in middle, 3.8 at bottom)
	sampleOutput := `Fetching available models...
gemini-3.7-flash-high	Gemini 3.7 Flash (High)
gemini-2.5-flash-high	Gemini 2.5 Flash (High)
gemini-4.0-flash-high	Gemini 4.0 Flash (High)
gemini-3.8-flash-high	Gemini 3.8 Flash (High)
gemini-3.1-pro-high	Gemini 3.1 Pro (High)
claude-sonnet-4-6	Claude Sonnet 4.6 (Thinking)
`

	dm := ParseAgyModelsOutput(sampleOutput)
	if dm.LatestFlash != "gemini-4.0-flash" {
		t.Errorf("expected LatestFlash to be 'gemini-4.0-flash', got %q", dm.LatestFlash)
	}
	if dm.LatestPro != "gemini-3.1-pro" {
		t.Errorf("expected LatestPro to be 'gemini-3.1-pro', got %q", dm.LatestPro)
	}
}

func TestModelSupportsEffort(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gemini-3.8-flash", true},
		{"gemini-3.7-flash", true},
		{"gemini-3.1-pro", true},
		{"flash", true},
		{"", true},
		{"gemini-2.5-pro", false},
		{"claude-3-5-sonnet", false},
		{"claude-sonnet-4-6", false},
		{"gpt-4o", false},
		{"gpt-oss-120b-medium", false},
		{"deepseek-r1", false},
		{"qwen-2.5", false},
	}

	for _, tt := range tests {
		got := ModelSupportsEffort(tt.model)
		if got != tt.want {
			t.Errorf("ModelSupportsEffort(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestModelCachePersistence(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGYS_DIR", tempDir)

	// Reset cachedModels in memory
	modelCacheLock.Lock()
	cachedModels = nil
	modelCacheLock.Unlock()

	// Save fake discovered models
	dm := &DiscoveredModels{
		FetchedAt:   time.Now(),
		LatestFlash: "gemini-3.9-flash",
		LatestPro:   "gemini-3.9-pro",
		AllModels:   []string{"gemini-3.9-flash", "gemini-3.9-pro"},
	}
	if err := SaveCachedDiscoveredModels(dm); err != nil {
		t.Fatalf("SaveCachedDiscoveredModels failed: %v", err)
	}

	// Read from cache
	cached := ReadCachedDiscoveredModels()
	if cached == nil || cached.LatestFlash != "gemini-3.9-flash" {
		t.Fatalf("expected cached LatestFlash to be 'gemini-3.9-flash', got %v", cached)
	}

	// GetLatestGeminiModel should now return cached model
	if m := GetLatestGeminiModel(); m != "gemini-3.9-flash" {
		t.Errorf("expected 'gemini-3.9-flash', got %q", m)
	}

	// Clean up
	_ = os.Remove(filepath.Join(tempDir, "models_cache.json"))
}
