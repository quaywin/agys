package cmd

import (
	"testing"
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
	t.Run("Default behavior when no model or effort specified", func(t *testing.T) {
		args := []string{"-p", "hello"}
		res := EnsureDefaultModelAndEffort(args)
		expected := []string{"-p", "hello", "--model", "gemini-3.7-flash", "--effort", "high"}
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

	t.Run("Preserves custom effort when provided", func(t *testing.T) {
		args := []string{"--effort", "low"}
		res := EnsureDefaultModelAndEffort(args)
		expected := []string{"--effort", "low", "--model", "gemini-3.7-flash"}
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
