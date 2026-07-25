package cmd

import (
	"testing"
)

func TestGuiCommandFlags(t *testing.T) {
	if guiCmd.Use != "gui [profile_name]" {
		t.Errorf("Expected guiCmd.Use to be 'gui [profile_name]', got %s", guiCmd.Use)
	}

	flag := guiCmd.Flags().Lookup("force")
	if flag == nil {
		t.Fatalf("Expected 'force' flag to exist on guiCmd")
	}
	if flag.Shorthand != "f" {
		t.Errorf("Expected 'force' flag shorthand to be 'f', got %s", flag.Shorthand)
	}
}
