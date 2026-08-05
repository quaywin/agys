package cmd

import (
	"testing"
)

func TestPluginCommandFlags(t *testing.T) {
	if pluginCmd.Use != "plugin" {
		t.Errorf("Expected pluginCmd.Use to be 'plugin', got %s", pluginCmd.Use)
	}

	installAll := pluginInstallCmd.Flags().Lookup("all")
	if installAll == nil {
		t.Fatalf("Expected 'all' flag on pluginInstallCmd")
	}
	if installAll.Shorthand != "a" {
		t.Errorf("Expected 'all' flag shorthand to be 'a', got %s", installAll.Shorthand)
	}

	listAll := pluginListCmd.Flags().Lookup("all")
	if listAll == nil {
		t.Fatalf("Expected 'all' flag on pluginListCmd")
	}

	uninstallAll := pluginUninstallCmd.Flags().Lookup("all")
	if uninstallAll == nil {
		t.Fatalf("Expected 'all' flag on pluginUninstallCmd")
	}
}
