# Implementation Plan: `agys plugin` Command Suite

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a dedicated `agys plugin` subcommand suite (`install`, `list`, `uninstall`) with clean output and `--all` / `-a` multi-profile support.

**Architecture:** Create `cmd/plugin.go` defining Cobra subcommands `pluginCmd`, `pluginInstallCmd`, `pluginListCmd`, `pluginUninstallCmd`. Helper function `execPluginCmd` runs `agy plugin <action> <args>` with `HOME` set directly to each profile directory, formatting output concisely per profile.

**Tech Stack:** Go (Cobra CLI, `os/exec`).

## Global Constraints
- Must use `rtk` for git and shell commands.
- Must preserve existing profile isolation by setting `HOME=<profileDir>`.

---

### Task 1: Create `cmd/plugin.go` and `cmd/plugin_test.go`

**Files:**
- Create: `/Users/quaywin/Projects_1/agys/cmd/plugin.go`
- Create: `/Users/quaywin/Projects_1/agys/cmd/plugin_test.go`

- [ ] **Step 1: Write failing unit test in `cmd/plugin_test.go`**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `rtk go test ./cmd -run TestPluginCommandFlags`
Expected: FAIL with "undefined: pluginCmd"

- [ ] **Step 3: Implement `cmd/plugin.go`**

Create `cmd/plugin.go` with:
- Cobra command definitions (`pluginCmd`, `pluginInstallCmd`, `pluginListCmd`, `pluginUninstallCmd`).
- `--all` / `-a` boolean flag registered on all subcommands.
- Helper function `execPluginCmd(action string, pluginArg string, profileName string, isAll bool) error`.
- When `isAll` is true:
  Iterate over `profile.List()`, construct `exec.CommandContext("agy", "plugin", action, pluginArg)`, set `cmd.Env` with `HOME=<profileDir>`, run command, print output formatted as `[%d/%d] %-12s %s\n`.
- Add `rootCmd.AddCommand(pluginCmd)`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `rtk go test ./...`
Expected: PASS

- [ ] **Step 5: Build and manual verification**

Run: `rtk go build -o agys . && rtk cp agys /Users/quaywin/.local/bin/agys`
Run: `rtk agys plugin --help`
Run: `rtk agys plugin install https://github.com/obra/superpowers --all`

- [ ] **Step 6: Commit**

```bash
rtk git add cmd/plugin.go cmd/plugin_test.go
rtk git commit -m "feat: add dedicated agys plugin command suite with --all support"
```
