package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quaywin/agys/pkg/profile"
)

func TestRunCommandFlags(t *testing.T) {
	flag := runCmd.Flags().Lookup("all")
	if flag == nil {
		t.Fatalf("Expected 'all' flag to exist on runCmd")
	}
	if flag.Shorthand != "a" {
		t.Errorf("Expected 'all' flag shorthand to be 'a', got %s", flag.Shorthand)
	}

	noSlFlag := runCmd.Flags().Lookup("no-statusline")
	if noSlFlag == nil {
		t.Fatalf("Expected 'no-statusline' flag to exist on runCmd")
	}
}

func TestEnsureDefaultModelAndEffort(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("AGYS_DIR", tempDir)
	_ = profile.SaveCachedDiscoveredModels(&profile.DiscoveredModels{
		FetchedAt:   time.Now(),
		LatestFlash: profile.DefaultGeminiModel,
		LatestPro:   "gemini-3.1-pro",
		AllModels:   []string{profile.DefaultGeminiModel, "gemini-3.1-pro"},
	})

	t.Run("Default behavior when no model or effort specified", func(t *testing.T) {
		args := []string{"-p", "hello"}
		res := EnsureDefaultModelAndEffort(args)
		expected := []string{"-p", "hello", "--model", "gemini-3.8-flash", "--effort", "high"}
		if len(res) != len(expected) {
			t.Fatalf("expected len %d, got %d: %v", len(expected), len(res), res)
		}
		for i, v := range expected {
			if res[i] != v {
				t.Errorf("at index %d: expected %q, got %q", i, v, res[i])
			}
		}
	})

	t.Run("Preserves custom model and skips default model injection", func(t *testing.T) {
		args := []string{"--model", "claude-3-5-sonnet"}
		res := EnsureDefaultModelAndEffort(args)
		if len(res) != 2 || res[0] != "--model" || res[1] != "claude-3-5-sonnet" {
			t.Errorf("expected original args preserved without adding effort for claude, got: %v", res)
		}
	})

	t.Run("Appends effort high for gemini flash models when effort not specified", func(t *testing.T) {
		args := []string{"--model", "gemini-3.8-flash"}
		res := EnsureDefaultModelAndEffort(args)
		expected := []string{"--model", "gemini-3.8-flash", "--effort", "high"}
		if len(res) != len(expected) {
			t.Fatalf("expected %v, got %v", expected, res)
		}
	})

	t.Run("Resolves latest and auto to latest Gemini model", func(t *testing.T) {
		argsLatest := []string{"--model", "latest"}
		resLatest := EnsureDefaultModelAndEffort(argsLatest)
		if len(resLatest) != 4 || resLatest[1] != "gemini-3.8-flash" || resLatest[3] != "high" {
			t.Errorf("expected latest to resolve to gemini-3.8-flash with high effort, got %v", resLatest)
		}

		argsAuto := []string{"--model=auto"}
		resAuto := EnsureDefaultModelAndEffort(argsAuto)
		if len(resAuto) != 3 || resAuto[0] != "--model=gemini-3.8-flash" || resAuto[2] != "high" {
			t.Errorf("expected auto to resolve to gemini-3.8-flash with high effort, got %v", resAuto)
		}

		argsMLatest := []string{"-m", "latest"}
		resMLatest := EnsureDefaultModelAndEffort(argsMLatest)
		if len(resMLatest) != 4 || resMLatest[0] != "--model" || resMLatest[1] != "gemini-3.8-flash" || resMLatest[3] != "high" {
			t.Errorf("expected -m latest to normalize to --model gemini-3.8-flash with high effort, got %v", resMLatest)
		}

		argsMAuto := []string{"-m=auto"}
		resMAuto := EnsureDefaultModelAndEffort(argsMAuto)
		if len(resMAuto) != 3 || resMAuto[0] != "--model=gemini-3.8-flash" || resMAuto[2] != "high" {
			t.Errorf("expected -m=auto to normalize to --model=gemini-3.8-flash with high effort, got %v", resMAuto)
		}

		argsMExplicit := []string{"-m=gemini-3.7-flash"}
		resMExplicit := EnsureDefaultModelAndEffort(argsMExplicit)
		if len(resMExplicit) != 3 || resMExplicit[0] != "--model=gemini-3.7-flash" || resMExplicit[2] != "high" {
			t.Errorf("expected -m=gemini-3.7-flash to normalize to --model=gemini-3.7-flash, got %v", resMExplicit)
		}

		argsMClaude := []string{"-m", "claude-sonnet-4-6"}
		resMClaude := EnsureDefaultModelAndEffort(argsMClaude)
		if len(resMClaude) != 2 || resMClaude[0] != "--model" || resMClaude[1] != "claude-sonnet-4-6" {
			t.Errorf("expected -m claude-sonnet-4-6 to normalize without effort, got %v", resMClaude)
		}
	})

	t.Run("Preserves custom effort when provided", func(t *testing.T) {
		args := []string{"--effort", "low"}
		res := EnsureDefaultModelAndEffort(args)
		expected := []string{"--effort", "low", "--model", "gemini-3.8-flash"}
		if len(res) != len(expected) {
			t.Fatalf("expected len %d, got %d: %v", len(expected), len(res), res)
		}
	})

	t.Run("Ignores subcommands like models, agents, help, mcp, mic-serve, remote-control, acp", func(t *testing.T) {
		subcmds := []string{"models", "agents", "help", "version", "mcp", "mic-serve", "remote-control", "acp"}
		for _, sc := range subcmds {
			args := []string{sc}
			res := EnsureDefaultModelAndEffort(args)
			if len(res) != 1 || res[0] != sc {
				t.Errorf("expected subcommand %q to remain unmodified, got %v", sc, res)
			}
		}
	})

	t.Run("Does not inject conflicting --effort for variant-suffixed models like gemini-4-hydrogen-xhigh", func(t *testing.T) {
		for _, variant := range []string{"gemini-4-hydrogen-xhigh", "Gemini 4 Hydrogen (XHigh)", "gemini-3.8-flash-high"} {
			res := EnsureDefaultModelAndEffort([]string{"-m", variant})
			for _, a := range res {
				if a == "--effort" || strings.HasPrefix(a, "--effort=") {
					t.Fatalf("expected NO --effort injection for variant %q, got %v", variant, res)
				}
			}
		}
	})

	t.Run("Resolves hydrogen and pro aliases and preserves xhigh default effort", func(t *testing.T) {
		_ = profile.SaveCachedDiscoveredModels(&profile.DiscoveredModels{
			SchemaVersion:  profile.ModelCacheSchemaVersion,
			FetchedAt:      time.Now(),
			LatestFlash:    "gemini-3.8-flash",
			LatestHydrogen: "gemini-4-hydrogen",
			AllModels:      []string{"gemini-4-hydrogen", "gemini-3.8-flash"},
			ModelEfforts: map[string][]string{
				"gemini-4-hydrogen": {"low", "medium", "high", "xhigh"},
				"gemini-3.8-flash":  {"low", "medium", "high"},
			},
		})

		res := EnsureDefaultModelAndEffortWithOptions([]string{"-m", "hydrogen"}, "gemini-4-hydrogen", "xhigh")
		expected := []string{"--model", "gemini-4-hydrogen", "--effort", "xhigh"}
		if len(res) != len(expected) {
			t.Fatalf("expected %v, got %v", expected, res)
		}
		for i, v := range expected {
			if res[i] != v {
				t.Errorf("at index %d: expected %q, got %q", i, v, res[i])
			}
		}
	})

	t.Run("Clamps cached xhigh effort to high when switching to gemini-3.8-flash", func(t *testing.T) {
		_ = profile.SaveCachedDiscoveredModels(&profile.DiscoveredModels{
			SchemaVersion:  profile.ModelCacheSchemaVersion,
			FetchedAt:      time.Now(),
			LatestFlash:    "gemini-3.8-flash",
			LatestHydrogen: "gemini-4-hydrogen",
			AllModels:      []string{"gemini-4-hydrogen", "gemini-3.8-flash"},
			ModelEfforts: map[string][]string{
				"gemini-4-hydrogen": {"low", "medium", "high", "xhigh"},
				"gemini-3.8-flash":  {"low", "medium", "high"},
			},
		})

		res := EnsureDefaultModelAndEffortWithOptions([]string{"--model", "gemini-3.8-flash"}, "gemini-4-hydrogen", "xhigh")
		expected := []string{"--model", "gemini-3.8-flash", "--effort", "high"}
		if len(res) != len(expected) {
			t.Fatalf("expected %v, got %v", expected, res)
		}
		for i, v := range expected {
			if res[i] != v {
				t.Errorf("at index %d: expected %q, got %q", i, v, res[i])
			}
		}
	})
}

func hasFlagValue(args []string, flag, val string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == flag+"="+val {
			return true
		}
		if args[i] == flag && i+1 < len(args) && args[i+1] == val {
			return true
		}
	}
	return false
}

func hasAnyFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

func TestPrepareRunModelAndEffortArgs_EffortOverridePersistence(t *testing.T) {
	agysDir := t.TempDir()
	t.Setenv("AGYS_DIR", agysDir)

	if err := profile.SaveCachedDiscoveredModels(&profile.DiscoveredModels{
		SchemaVersion:  profile.ModelCacheSchemaVersion,
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

	profileDir := filepath.Join(agysDir, "profiles", "testprof")
	cliDir := filepath.Join(profileDir, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(cliDir, 0755); err != nil {
		t.Fatalf("failed to create profile dir: %v", err)
	}
	settingsPath := filepath.Join(cliDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"model":"Gemini 4 Hydrogen (XHigh)"}`), 0600); err != nil {
		t.Fatalf("failed to seed settings.json: %v", err)
	}

	// (a) Initial no-flag run should resolve Hydrogen + xhigh from settings.json
	args1, _ := prepareRunModelAndEffortArgs(profileDir, []string{"chat"})
	if !hasFlagValue(args1, "--model", "gemini-4-hydrogen") || !hasFlagValue(args1, "--effort", "xhigh") {
		t.Fatalf("step (a) expected --model gemini-4-hydrogen and --effort xhigh, got %v", args1)
	}

	// (b) Explicit --effort low must update .active_model, .active_effort, and settings.json
	args2, _ := prepareRunModelAndEffortArgs(profileDir, []string{"--effort", "low", "chat"})
	if !hasFlagValue(args2, "--model", "gemini-4-hydrogen") || !hasFlagValue(args2, "--effort", "low") {
		t.Fatalf("step (b) expected --model gemini-4-hydrogen and --effort low, got %v", args2)
	}
	activeModelBytes, err := os.ReadFile(filepath.Join(profileDir, ".active_model"))
	if err != nil {
		t.Fatalf("failed reading .active_model: %v", err)
	}
	if strings.TrimSpace(string(activeModelBytes)) != "gemini-4-hydrogen-low" {
		t.Errorf("step (b) expected .active_model 'gemini-4-hydrogen-low', got %q", strings.TrimSpace(string(activeModelBytes)))
	}
	settingsBytes, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed reading settings.json: %v", err)
	}
	if !strings.Contains(string(settingsBytes), "gemini-4-hydrogen-low") {
		t.Errorf("step (b) expected settings.json to contain 'gemini-4-hydrogen-low', got %s", string(settingsBytes))
	}

	// (c) Subsequent no-flag run must keep low (no stale XHigh override from .active_model)
	args3, _ := prepareRunModelAndEffortArgs(profileDir, []string{"chat"})
	if !hasFlagValue(args3, "--model", "gemini-4-hydrogen") || !hasFlagValue(args3, "--effort", "low") {
		t.Fatalf("step (c) expected persisted --effort low, got %v", args3)
	}

	// (d) Variant-suffixed -m gemini-4-hydrogen-xhigh must NOT append --effort (conflicts in agy)
	args4, _ := prepareRunModelAndEffortArgs(profileDir, []string{"-m", "gemini-4-hydrogen-xhigh"})
	if hasAnyFlag(args4, "--effort", "-e") {
		t.Fatalf("step (d) must not inject --effort alongside variant model, got %v", args4)
	}
	effBytes, err := os.ReadFile(filepath.Join(profileDir, ".active_effort"))
	if err != nil || strings.TrimSpace(string(effBytes)) != "xhigh" {
		t.Errorf("step (d) expected .active_effort 'xhigh', got %q (err=%v)", strings.TrimSpace(string(effBytes)), err)
	}

	// (e) Switching to -m gemini-3.8-flash after xhigh must clamp stale xhigh to high
	args5, _ := prepareRunModelAndEffortArgs(profileDir, []string{"-m", "gemini-3.8-flash"})
	if (!hasFlagValue(args5, "--model", "gemini-3.8-flash") && !hasFlagValue(args5, "-m", "gemini-3.8-flash")) || !hasFlagValue(args5, "--effort", "high") {
		t.Fatalf("step (e) expected clamped --effort high for gemini-3.8-flash, got %v", args5)
	}
}

func TestRemoteControlArgHandling(t *testing.T) {
	t.Run("Ensures model and remote-control flags are preserved", func(t *testing.T) {
		args := []string{"--remote-control"}
		res := EnsureDefaultModelAndEffort(args)
		// Should include model and effort
		hasModel := false
		hasRemote := false
		for _, a := range res {
			if a == "--model" {
				hasModel = true
			}
			if a == "--remote-control" {
				hasRemote = true
			}
		}
		if !hasModel || !hasRemote {
			t.Errorf("expected both --model and --remote-control in result, got %v", res)
		}
	})
}

func TestIsInteractiveSession(t *testing.T) {
	// Interactive cases
	interactiveCases := [][]string{
		{},
		{"-c"},
		{"--continue"},
		{"--conversation", "conv-12345"},
		{"--conversation=conv-12345"},
		{"--model", "gemini-3.8-flash"},
		{"--prompt-interactive", "hello world"},
		{"-i", "hello world"},
		{"-i", "update the database"},
		{"--model", "agent"},
		{"--system-prompt", "models"},
		{"update", "the", "login", "button"},
		{"help", "me", "refactor", "this"},
		{"models", "of", "computation"},
		{"--", "update", "the", "login", "button"},
		{"--", "models"},
		{"--model", "gemini-3.8-flash", "--", "update", "docs"},
	}
	for _, tc := range interactiveCases {
		if !isInteractiveSession(tc) {
			t.Errorf("expected %v to be detected as interactive, got false", tc)
		}
	}

	// Non-interactive cases
	nonInteractiveCases := [][]string{
		{"-p", "run task"},
		{"-p=run task"},
		{"--print", "run task"},
		{"--prompt", "run task"},
		{"--prompt=run task"},
		{"--print=run task"},
		{"--input-format", "stream-json"},
		{"--input-format=stream-json"},
		{"--output-format", "json"},
		{"--output-format=json"},
		{"-h"},
		{"--help"},
		{"-v"},
		{"--version"},
		{"auth", "status"},
		{"config", "get"},
		{"mcp", "list"},
		{"plugin", "validate", "--strict", "my-plugin"},
		{"plugins", "import", "source"},
		{"install", "--skip-aliases"},
		{"mic-serve"},
		{"remote-control", "status"},
		{"models"},
		{"agents"},
		{"version"},
		{"update"},
	}
	for _, tc := range nonInteractiveCases {
		if isInteractiveSession(tc) {
			t.Errorf("expected %v to be detected as non-interactive, got true", tc)
		}
	}
}

func TestResolveResumeProfile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", tempHome+"/.agys")

	p1 := "work"
	p2 := "personal"
	dir1, err := profile.Create(p1)
	if err != nil {
		t.Fatalf("failed to create p1: %v", err)
	}
	_, err = profile.Create(p2)
	if err != nil {
		t.Fatalf("failed to create p2: %v", err)
	}

	convID := "conv-resume-test"
	brainDir := filepath.Join(dir1, ".gemini", "antigravity-cli", "brain", convID)
	logsDir := filepath.Join(brainDir, ".system_generated", "logs")
	_ = os.MkdirAll(logsDir, 0700)
	_ = os.WriteFile(filepath.Join(logsDir, "transcript.jsonl"), []byte(`{"step_index":0}`+"\n"), 0600)
	_ = profile.SaveLastConversation(convID)

	t.Run("Auto mode auto-switches to owning profile", func(t *testing.T) {
		resProf, resArgs, err := resolveResumeProfile("auto", []string{"--conversation=" + convID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resProf != p1 {
			t.Errorf("expected auto mode to switch to %q, got %q", p1, resProf)
		}
		if len(resArgs) != 1 || resArgs[0] != "--conversation="+convID {
			t.Errorf("expected args to remain intact, got %v", resArgs)
		}
	})

	t.Run("Same explicit profile continues without migration", func(t *testing.T) {
		resProf, resArgs, err := resolveResumeProfile(p1, []string{"--conversation=" + convID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resProf != p1 {
			t.Errorf("expected %q, got %q", p1, resProf)
		}
		if len(resArgs) != 1 || resArgs[0] != "--conversation="+convID {
			t.Errorf("expected args to remain intact, got %v", resArgs)
		}
	})

	t.Run("Different explicit profile triggers migration", func(t *testing.T) {
		resProf, resArgs, err := resolveResumeProfile(p2, []string{"-c"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resProf != p2 {
			t.Errorf("expected profile to remain %q, got %q", p2, resProf)
		}
		// -c should be replaced with --conversation=<convID>
		expectedArg := "--conversation=" + convID
		if len(resArgs) != 1 || resArgs[0] != expectedArg {
			t.Errorf("expected -c to be replaced by %q, got %v", expectedArg, resArgs)
		}

		// Conversation should now belong to p2
		owner, err := profile.FindProfileByConversation(convID)
		if err != nil || owner != p2 {
			t.Errorf("expected conversation owner after migration to be %q, got %q (err: %v)", p2, owner, err)
		}
	})

	t.Run("Same explicit profile with -r or --resume normalizes to canonical flag", func(t *testing.T) {
		resProf, resArgs, err := resolveResumeProfile(p1, []string{"-r"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resProf != p1 {
			t.Errorf("expected %q, got %q", p1, resProf)
		}
		expectedArg := "--conversation=" + convID
		if len(resArgs) != 1 || resArgs[0] != expectedArg {
			t.Errorf("expected -r to be replaced by %q, got %v", expectedArg, resArgs)
		}
	})
}

func TestNormalizeAgyArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty args",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "version subcommand normalized to --version flag",
			input:    []string{"version"},
			expected: []string{"--version"},
		},
		{
			name:     "login normalized to empty args",
			input:    []string{"login"},
			expected: []string{},
		},
		{
			name:     "auth login normalized to empty args",
			input:    []string{"auth", "login"},
			expected: []string{},
		},
		{
			name:     "models subcommand untouched",
			input:    []string{"models"},
			expected: []string{"models"},
		},
		{
			name:     "acp subcommand untouched",
			input:    []string{"acp"},
			expected: []string{"acp"},
		},
		{
			name:     "arbitrary prompt args untouched",
			input:    []string{"-p", "hello world"},
			expected: []string{"-p", "hello world"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeAgyArgs(tc.input)
			if len(result) != len(tc.expected) {
				t.Fatalf("expected length %d, got %d (result: %v)", len(tc.expected), len(result), result)
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tc.expected[i], result[i])
				}
			}
		})
	}
}

func TestNormalizeResumeArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		convID   string
		expected []string
	}{
		{
			name:     "-r with convID",
			input:    []string{"-r"},
			convID:   "c-123",
			expected: []string{"--conversation=c-123"},
		},
		{
			name:     "-r without convID",
			input:    []string{"-r"},
			convID:   "",
			expected: []string{"--continue"},
		},
		{
			name:     "--resume with convID",
			input:    []string{"--resume"},
			convID:   "c-123",
			expected: []string{"--conversation=c-123"},
		},
		{
			name:     "--resume without convID",
			input:    []string{"--resume"},
			convID:   "",
			expected: []string{"--continue"},
		},
		{
			name:     "--resume=explicitID",
			input:    []string{"--resume=custom-456"},
			convID:   "ignored",
			expected: []string{"--conversation=custom-456"},
		},
		{
			name:     "--resume explicitID positional",
			input:    []string{"--resume", "custom-789"},
			convID:   "ignored",
			expected: []string{"--conversation", "custom-789"},
		},
		{
			name:     "-r=explicitID",
			input:    []string{"-r=custom-456"},
			convID:   "ignored",
			expected: []string{"--conversation=custom-456"},
		},
		{
			name:     "-r explicitID positional",
			input:    []string{"-r", "custom-789"},
			convID:   "ignored",
			expected: []string{"--conversation", "custom-789"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeResumeArgs(tc.input, tc.convID)
			if len(result) != len(tc.expected) {
				t.Fatalf("expected length %d, got %d (result: %v)", len(tc.expected), len(result), result)
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tc.expected[i], result[i])
				}
			}
		})
	}
}

func TestFilterPreservedSessionFlags(t *testing.T) {
	got := filterPreservedSessionFlags([]string{"-r", "conv-1", "--dangerously-skip-permissions", "-c", "--resume=conv-2", "--model", "gemini-4-hydrogen"})
	want := []string{"--dangerously-skip-permissions", "--model", "gemini-4-hydrogen"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("at index %d: expected %q, got %q", i, want[i], got[i])
		}
	}

	orchArgs := EnsureDefaultModelAndEffortWithOptions(nil, "Orchestrator", "xhigh")
	if hasAnyFlag(orchArgs, "--effort") || !hasFlagValue(orchArgs, "--model", "Orchestrator") {
		t.Errorf("expected Orchestrator args without --effort, got %v", orchArgs)
	}
}
