package cmd

import (
	"testing"
)

func TestIdeCommandFlags(t *testing.T) {
	if ideCmd.Use != "ide [profile_name] [project_path]" {
		t.Errorf("Expected ideCmd.Use to be 'ide [profile_name] [project_path]', got %s", ideCmd.Use)
	}
}
