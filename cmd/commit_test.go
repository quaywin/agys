package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCommitCmdFlags(t *testing.T) {
	if commitCmd.Name() != "commit" {
		t.Errorf("expected command name 'commit', got %q", commitCmd.Name())
	}

	msgFlag := commitCmd.Flags().Lookup("message")
	if msgFlag == nil || msgFlag.Shorthand != "m" {
		t.Errorf("expected -m / --message flag on commitCmd")
	}

	yesFlag := commitCmd.Flags().Lookup("yes")
	if yesFlag == nil || yesFlag.Shorthand != "y" {
		t.Errorf("expected -y / --yes flag on commitCmd")
	}

	dryRunFlag := commitCmd.Flags().Lookup("dry-run")
	if dryRunFlag == nil {
		t.Errorf("expected --dry-run flag on commitCmd")
	}

	allFlag := commitCmd.Flags().Lookup("all")
	if allFlag == nil || allFlag.Shorthand != "a" {
		t.Errorf("expected -a / --all flag on commitCmd")
	}

	pushFlag := commitCmd.Flags().Lookup("push")
	if pushFlag == nil || pushFlag.Shorthand != "p" {
		t.Errorf("expected -p / --push flag on commitCmd")
	}

	modelFlag := commitCmd.Flags().Lookup("model")
	if modelFlag == nil {
		t.Errorf("expected --model flag on commitCmd")
	}

	effortFlag := commitCmd.Flags().Lookup("effort")
	if effortFlag == nil {
		t.Errorf("expected --effort flag on commitCmd")
	}
}

func TestCommitCmdHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"commit", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error executing commit --help: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("agys commit [profile_name] [flags]")) {
		t.Errorf("help output missing usage string, got: %s", output)
	}
}

func TestCommitCmdFlagCompletions(t *testing.T) {
	fnModel, ok := commitCmd.GetFlagCompletionFunc("model")
	if !ok || fnModel == nil {
		t.Fatalf("expected completion function registered for commit --model flag")
	}
	models, dir := fnModel(commitCmd, nil, "")
	if dir != cobra.ShellCompDirectiveNoFileComp || len(models) == 0 {
		t.Errorf("unexpected commit --model completions: dir=%v len=%d", dir, len(models))
	}
	foundHydrogen := false
	for _, m := range models {
		if strings.HasPrefix(m, "gemini-4-hydrogen\t") {
			foundHydrogen = true
			break
		}
	}
	if !foundHydrogen {
		t.Errorf("expected gemini-4-hydrogen in commit --model completions, got %v", models)
	}

	fnEffort, ok := commitCmd.GetFlagCompletionFunc("effort")
	if !ok || fnEffort == nil {
		t.Fatalf("expected completion function registered for commit --effort flag")
	}

	prevCommitModel := commitModel
	t.Cleanup(func() { commitModel = prevCommitModel })

	commitModel = "gemini-3.8-flash"
	effortsFlash, dir := fnEffort(commitCmd, nil, "")
	if dir != cobra.ShellCompDirectiveNoFileComp || len(effortsFlash) == 0 {
		t.Errorf("unexpected commit --effort completions: dir=%v len=%d", dir, len(effortsFlash))
	}
	for _, e := range effortsFlash {
		if strings.HasPrefix(e, "xhigh\t") {
			t.Errorf("did not expect xhigh completion when commitModel=gemini-3.8-flash, got %v", effortsFlash)
		}
	}

	commitModel = "gemini-4-hydrogen"
	effortsHyd, _ := fnEffort(commitCmd, nil, "")
	hasXHigh := false
	for _, e := range effortsHyd {
		if strings.HasPrefix(e, "xhigh\t") {
			hasXHigh = true
			break
		}
	}
	if !hasXHigh {
		t.Errorf("expected xhigh completion when commitModel=gemini-4-hydrogen, got %v", effortsHyd)
	}
}
