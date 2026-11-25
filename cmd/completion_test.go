package cmd

import (
	"strings"
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
	hasEffortFlag := false
	for _, f := range flags {
		if strings.HasPrefix(f, "--effort=") {
			hasEffortFlag = true
			break
		}
	}
	if !hasEffortFlag {
		t.Errorf("expected --effort= in flag completions, got %v", flags)
	}

	// Test model completions
	models, _ := CompleteAgyArgs([]string{"-m"}, "")
	if len(models) == 0 {
		t.Errorf("expected model completions for -m flag")
	}
	hasHydrogen := false
	for _, m := range models {
		if strings.HasPrefix(m, "gemini-4-hydrogen\t") {
			hasHydrogen = true
			break
		}
	}
	if !hasHydrogen {
		t.Errorf("expected gemini-4-hydrogen in model completions")
	}

	// Test effort completions for gemini-4-hydrogen (includes xhigh)
	effortsH4, _ := CompleteAgyArgs([]string{"-m", "gemini-4-hydrogen", "--effort"}, "")
	hasXHigh := false
	for _, e := range effortsH4 {
		if strings.HasPrefix(e, "xhigh\t") {
			hasXHigh = true
			break
		}
	}
	if !hasXHigh {
		t.Errorf("expected xhigh effort completion for gemini-4-hydrogen, got %v", effortsH4)
	}

	// Test effort completions for gemini-3.8-flash (excludes xhigh)
	effortsFlash, _ := CompleteAgyArgs([]string{"--model=gemini-3.8-flash", "--effort"}, "")
	for _, e := range effortsFlash {
		if strings.HasPrefix(e, "xhigh\t") {
			t.Errorf("did not expect xhigh effort completion for gemini-3.8-flash, got %v", effortsFlash)
		}
	}

	// Test equals-prefixed flag completions (-m= and --effort=)
	modelsEq, dirEq := CompleteAgyArgs(nil, "-m=")
	if dirEq != cobra.ShellCompDirectiveNoFileComp || len(modelsEq) == 0 {
		t.Errorf("expected model completions for -m=, got dir=%v len=%d", dirEq, len(modelsEq))
	}
	effortsEq, _ := CompleteAgyArgs([]string{"-m=gemini-4-hydrogen"}, "--effort=")
	hasXHighEq := false
	for _, e := range effortsEq {
		if strings.HasPrefix(e, "xhigh\t") {
			hasXHighEq = true
			break
		}
	}
	if !hasXHighEq {
		t.Errorf("expected xhigh completion for -m=gemini-4-hydrogen --effort=, got %v", effortsEq)
	}

	// Test subcommand completions
	subcmds, _ := CompleteAgyArgs(nil, "")
	if len(subcmds) == 0 {
		t.Errorf("expected subcommand completions")
	}
	hasPluginSub := false
	for _, s := range subcmds {
		if strings.HasPrefix(s, "plugin\t") {
			hasPluginSub = true
			break
		}
	}
	if !hasPluginSub {
		t.Errorf("expected 'plugin' subcommand completion, got %v", subcmds)
	}

	// Test plugin verb completions
	pluginVerbs, _ := CompleteAgyArgs([]string{"plugin"}, "")
	hasValidate := false
	for _, v := range pluginVerbs {
		if strings.HasPrefix(v, "validate\t") {
			hasValidate = true
			break
		}
	}
	if !hasValidate {
		t.Errorf("expected 'validate' verb for plugin completion, got %v", pluginVerbs)
	}

	// Test --mode= value completions and --settings= flag presence
	modes, _ := CompleteAgyArgs(nil, "--mode=")
	if len(modes) != 3 {
		t.Errorf("expected 3 --mode= completions, got %v", modes)
	}
	hasSettingsFlag := false
	for _, f := range flags {
		if strings.HasPrefix(f, "--settings=") {
			hasSettingsFlag = true
			break
		}
	}
	if !hasSettingsFlag {
		t.Errorf("expected --settings= in flag completions")
	}

	// After subcommand + verb, must NOT re-suggest top-level subcommands
	afterVerb, afterDir := CompleteAgyArgs([]string{"plugin", "list"}, "")
	if len(afterVerb) != 0 || afterDir != cobra.ShellCompDirectiveDefault {
		t.Errorf("expected no subcommands after 'plugin list', got %v (dir=%v)", afterVerb, afterDir)
	}

	// Flag value matching a subcommand name (e.g. --model models) must still allow subcommands later
	afterModelVal, _ := CompleteAgyArgs([]string{"--model", "models"}, "")
	if len(afterModelVal) == 0 {
		t.Errorf("expected top-level subcommand completions after '--model models'")
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
