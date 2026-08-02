package cmd

import (
	"bytes"
	"testing"
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
