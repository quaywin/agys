package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCompleteAgyArgs(t *testing.T) {
	// Test flag completions
	flags, directive := CompleteAgyArgs(nil, "-")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(flags) == 0 {
		t.Errorf("expected flags list, got empty")
	}

	// Test model completions
	models, _ := CompleteAgyArgs([]string{"-m"}, "")
	if len(models) == 0 {
		t.Errorf("expected model completions for -m flag")
	}

	// Test subcommand completions
	subcmds, _ := CompleteAgyArgs(nil, "")
	if len(subcmds) == 0 {
		t.Errorf("expected subcommand completions")
	}
}

func TestCompleteRunArgs(t *testing.T) {
	// 1st arg: profiles
	res1, _ := CompleteRunArgs(nil, nil, "")
	if len(res1) == 0 {
		t.Errorf("expected profile list for 1st arg")
	}

	// 2nd arg+: agy params
	res2, _ := CompleteRunArgs(nil, []string{"work"}, "-")
	if len(res2) == 0 {
		t.Errorf("expected agy flags for 2nd arg")
	}
}

func TestCompletePriorityArgs(t *testing.T) {
	actions, _ := CompletePriorityArgs(nil, nil, "")
	if len(actions) != 3 {
		t.Errorf("expected 3 priority actions (set, get, list), got %d", len(actions))
	}

	setArgs, _ := CompletePriorityArgs(nil, []string{"set"}, "")
	if len(setArgs) == 0 {
		t.Errorf("expected profile list for 'priority set'")
	}
}

func TestCompleteSSHArgs(t *testing.T) {
	// Arg 0 (server)
	_, d0 := CompleteSSHArgs(nil, nil, "")
	if d0 != cobra.ShellCompDirectiveDefault {
		t.Errorf("expected ShellCompDirectiveDefault for server arg")
	}

	// Arg 1 with path prefix
	_, d1 := CompleteSSHArgs(nil, []string{"user@host"}, "/")
	if d1 != cobra.ShellCompDirectiveFilterDirs {
		t.Errorf("expected ShellCompDirectiveFilterDirs for path starting with /")
	}
}
