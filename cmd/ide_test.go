package cmd

import (
	"testing"
)

func TestIdeCommandFlags(t *testing.T) {
	if ideCmd.Use != "ide [profile_name] [project_path]" {
		t.Errorf("Expected ideCmd.Use to be 'ide [profile_name] [project_path]', got %s", ideCmd.Use)
	}

	flag := ideCmd.Flags().Lookup("force")
	if flag == nil {
		t.Fatalf("Expected 'force' flag to exist on ideCmd")
	}
	if flag.Shorthand != "f" {
		t.Errorf("Expected 'force' flag shorthand to be 'f', got %s", flag.Shorthand)
	}
}
