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
		{"gemini-4-hydrogen", GeminiModelVersion{4, 0, 0}, true},
		{"gemini-4-hydrogen-xhigh", GeminiModelVersion{4, 0, 0}, true},
		{"gemini-4-carbon", GeminiModelVersion{4, 0, 0}, true},
		{"gemini-4-neon", GeminiModelVersion{4, 0, 0}, true},
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
gemini-4-hydrogen-xhigh	Gemini 4 Hydrogen (XHigh)
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
		{"gemini-4-hydrogen", true},
		{"gemini-4-carbon", true},
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

func TestHookProcess_DoesNotSpawnBackgroundRefresh(t *testing.T) {
	SetHookProcess(true)
	defer SetHookProcess(false)

	if !IsHookProcess() {
		t.Errorf("expected IsHookProcess to return true")
	}

	tempDir := t.TempDir()
	t.Setenv("AGYS_DIR", tempDir)

	// Save stale model cache (older than 24 hours)
	dm := &DiscoveredModels{
		FetchedAt:   time.Now().Add(-48 * time.Hour),
		LatestFlash: "gemini-3.8-flash",
		LatestPro:   "gemini-3.8-pro",
		AllModels:   []string{"gemini-3.8-flash"},
	}
	_ = SaveCachedDiscoveredModels(dm)

	// Calling GetOrRefreshModels in hook process should return existing cache without blocking or errors
	res := GetOrRefreshModels()
	if res == nil || res.LatestFlash != "gemini-3.8-flash" {
		t.Errorf("expected GetOrRefreshModels in hook process to return cached models, got %+v", res)
	}
}

func TestSplitModelAndEffort(t *testing.T) {
	tests := []struct {
		input      string
		wantBase   string
		wantEffort string
	}{
		{"Gemini 4 Hydrogen (XHigh)", "gemini-4-hydrogen", "xhigh"},
		{"Gemini 4 Hydrogen (High)", "gemini-4-hydrogen", "high"},
		{"Gemini 4 Hydrogen (Medium)", "gemini-4-hydrogen", "medium"},
		{"Gemini 4 Hydrogen (Low)", "gemini-4-hydrogen", "low"},
		{"gemini-4-hydrogen-xhigh", "gemini-4-hydrogen", "xhigh"},
		{"gemini-4-hydrogen-max", "gemini-4-hydrogen", "max"},
		{"gemini-4-hydrogen", "gemini-4-hydrogen", ""},
		{"Gemini 3.8 Flash (High)", "gemini-3.8-flash", "high"},
		{"gemini-3.8-flash-high", "gemini-3.8-flash", "high"},
		{"Claude Opus 4.6 (Thinking)", "claude-opus-4-6-thinking", ""},
		{"Claude Sonnet 4.6 (Thinking)", "claude-sonnet-4-6", ""},
		{"GPT-OSS 120B (Medium)", "gpt-oss-120b", "medium"},
		{"Orchestrator (Neon)", "orchestrator-neon", ""},
	}

	for _, tt := range tests {
		gotBase, gotEffort := SplitModelAndEffort(tt.input)
		if gotBase != tt.wantBase || gotEffort != tt.wantEffort {
			t.Errorf("SplitModelAndEffort(%q) = (%q, %q), want (%q, %q)", tt.input, gotBase, gotEffort, tt.wantBase, tt.wantEffort)
		}
	}
}

func TestParseAgyModelsOutput_HydrogenAndEfforts(t *testing.T) {
	sampleOutput := `Fetching available models...
gemini-4-hydrogen-high	Gemini 4 Hydrogen (High)
gemini-4-hydrogen-xhigh	Gemini 4 Hydrogen (XHigh)
gemini-4-hydrogen-medium	Gemini 4 Hydrogen (Medium)
gemini-4-hydrogen-low	Gemini 4 Hydrogen (Low)
gemini-3.8-flash-high	Gemini 3.8 Flash (High)
gemini-3.8-flash-medium	Gemini 3.8 Flash (Medium)
gemini-3.8-flash-low	Gemini 3.8 Flash (Low)
claude-sonnet-4-6	Claude Sonnet 4.6 (Thinking)
claude-opus-4-6-thinking	Claude Opus 4.6 (Thinking)
gpt-oss-120b-medium	GPT-OSS 120B (Medium)
`

	dm := ParseAgyModelsOutput(sampleOutput)
	if dm.SchemaVersion != ModelCacheSchemaVersion {
		t.Errorf("expected SchemaVersion %d, got %d", ModelCacheSchemaVersion, dm.SchemaVersion)
	}
	if dm.LatestFlash != "gemini-3.8-flash" {
		t.Errorf("expected LatestFlash 'gemini-3.8-flash', got %q", dm.LatestFlash)
	}
	if dm.LatestHydrogen != "gemini-4-hydrogen" {
		t.Errorf("expected LatestHydrogen 'gemini-4-hydrogen', got %q", dm.LatestHydrogen)
	}
	effs := dm.ModelEfforts["gemini-4-hydrogen"]
	wantEffs := []string{"low", "medium", "high", "xhigh"}
	if len(effs) != len(wantEffs) {
		t.Fatalf("expected gemini-4-hydrogen efforts %v, got %v", wantEffs, effs)
	}
	for i, w := range wantEffs {
		if effs[i] != w {
			t.Errorf("effort[%d] = %q, want %q", i, effs[i], w)
		}
	}
}

func TestClampEffortForModel(t *testing.T) {
	dir1 := t.TempDir()
	t.Setenv("AGYS_DIR", dir1)
	t.Cleanup(func() {
		modelCacheLock.Lock()
		cachedModels = nil
		cachedModelsPath = ""
		modelCacheLock.Unlock()
	})

	if err := SaveCachedDiscoveredModels(&DiscoveredModels{
		SchemaVersion:  ModelCacheSchemaVersion,
		FetchedAt:      time.Now(),
		LatestFlash:    "gemini-3.8-flash",
		LatestHydrogen: "gemini-4-hydrogen",
		AllModels:      []string{"gemini-4-hydrogen", "gemini-3.8-flash"},
		ModelEfforts: map[string][]string{
			"gemini-4-hydrogen": {"low", "medium", "high", "xhigh"},
			"gemini-3.8-flash":  {"low", "medium", "high"},
		},
	}); err != nil {
		t.Fatalf("failed to seed model cache: %v", err)
	}

	if got := ClampEffortForModel("gemini-4-hydrogen", "xhigh"); got != "xhigh" {
		t.Errorf("expected xhigh for gemini-4-hydrogen, got %q", got)
	}
	if got := ClampEffortForModel("gemini-4-hydrogen", "max"); got != "xhigh" {
		t.Errorf("expected max clamped to xhigh for gemini-4-hydrogen, got %q", got)
	}
	if got := ClampEffortForModel("gemini-3.8-flash", "xhigh"); got != "high" {
		t.Errorf("expected xhigh clamped to high for gemini-3.8-flash, got %q", got)
	}
	if got := ClampEffortForModel("claude-sonnet-4-6", "xhigh"); got != "" {
		t.Errorf("expected empty effort for claude-sonnet-4-6, got %q", got)
	}
	if !ModelSupportsEffort("gemini-4-neon") || !ModelSupportsEffort("gemini-4-argon") {
		t.Errorf("expected neon and argon to support effort")
	}
	if ModelSupportsEffort("Orchestrator (Neon)") {
		t.Errorf("expected Orchestrator (Neon) to return false for --effort flag")
	}

	// Verify in-memory cache is isolated by AGYS_DIR path
	t.Setenv("AGYS_DIR", t.TempDir())
	if leaked := ReadCachedDiscoveredModels(); leaked != nil {
		t.Errorf("expected nil cache after switching AGYS_DIR, got %+v", leaked)
	}
}

