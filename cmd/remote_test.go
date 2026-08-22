package cmd

import (
	"testing"
)

func TestRemoteCommandFlags(t *testing.T) {
	t.Run("Remote command flags exist", func(t *testing.T) {
		portFlag := remoteCmd.Flags().Lookup("port")
		if portFlag == nil || portFlag.Shorthand != "p" {
			t.Errorf("expected -p/--port flag on remoteCmd")
		}

		nameFlag := remoteCmd.Flags().Lookup("name")
		if nameFlag == nil || nameFlag.Shorthand != "n" {
			t.Errorf("expected -n/--name flag on remoteCmd")
		}

		fgFlag := remoteCmd.Flags().Lookup("foreground")
		if fgFlag == nil || fgFlag.Shorthand != "f" {
			t.Errorf("expected -f/--foreground flag on remoteCmd")
		}

		skipFlag := remoteCmd.Flags().Lookup("dangerously-skip-permissions")
		if skipFlag == nil || skipFlag.Shorthand != "y" {
			t.Errorf("expected -y/--dangerously-skip-permissions flag on remoteCmd")
		}
	})

	t.Run("Remote stop flags exist", func(t *testing.T) {
		allFlag := remoteStopCmd.Flags().Lookup("all")
		if allFlag == nil || allFlag.Shorthand != "a" {
			t.Errorf("expected -a/--all flag on remoteStopCmd")
		}
	})

	t.Run("Remote logs flags exist", func(t *testing.T) {
		followFlag := remoteLogsCmd.Flags().Lookup("follow")
		if followFlag == nil || followFlag.Shorthand != "f" {
			t.Errorf("expected -f/--follow flag on remoteLogsCmd")
		}

		linesFlag := remoteLogsCmd.Flags().Lookup("lines")
		if linesFlag == nil || linesFlag.Shorthand != "n" {
			t.Errorf("expected -n/--lines flag on remoteLogsCmd")
		}
	})
}
