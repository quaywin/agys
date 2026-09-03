package cmd

import (
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

	t.Run("Ignores subcommands like models, agents, help", func(t *testing.T) {
		subcmds := []string{"models", "agents", "help", "version"}
		for _, sc := range subcmds {
			args := []string{sc}
			res := EnsureDefaultModelAndEffort(args)
			if len(res) != 1 || res[0] != sc {
				t.Errorf("expected subcommand %q to remain unmodified, got %v", sc, res)
			}
		}
	})
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
