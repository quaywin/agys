# Implementation Plan: `agys run --all` / `-a` Flag

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Support executing an `agy` command across all profiles sequentially via `agys run --all` (or `agys run -a`).

**Architecture:** Add a `--all` (`-a`) boolean flag to `runCmd`. When set, `runCmd` fetches all available profiles via `profile.List()`, loops over each profile, prints a formatted banner to `os.Stderr`, and invokes `runWithProfile`.

**Tech Stack:** Go (Standard Library, Cobra CLI framework).

## Global Constraints
- Must maintain 100% backward compatibility for single profile / auto / default profile runs when `--all` is not passed.
- Must use `rtk` for git and shell commands.

---

### Task 1: Add `--all` / `-a` flag and implementation to `cmd/run.go` and `cmd/run_test.go`

**Files:**
- Modify: `/Users/quaywin/Projects_1/agys/cmd/run.go`
- Create: `/Users/quaywin/Projects_1/agys/cmd/run_test.go`

**Interfaces:**
- Consumes: `profile.List()`
- Produces: `runAll` flag on `runCmd` in Cobra registry.

- [ ] **Step 1: Write the failing test in `cmd/run_test.go`**

```go
package cmd

import (
	"testing"
)

func TestRunCommandFlags(t *testing.T) {
	flag := runCmd.Flags().Lookup("all")
	if flag == nil {
		t.Fatalf("Expected 'all' flag to exist on runCmd")
	}
	if flag.Shorthand != "a" {
		t.Errorf("Expected 'all' flag shorthand to be 'a', got %s", flag.Shorthand)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `rtk go test ./cmd -run TestRunCommandFlags`
Expected: FAIL with "Expected 'all' flag to exist on runCmd"

- [ ] **Step 3: Update `cmd/run.go` to add `--all` flag and iteration logic**

In `cmd/run.go`:
1. Declare `var runAll bool`.
2. Register `runCmd.Flags().BoolVarP(&runAll, "all", "a", false, "Execute agy command across all profiles sequentially")` in `init()`.
3. Update `runCmd.RunE` and `Args`:
   - Set `Args: cobra.MinimumNArgs(0)`.
   - In `RunE`:
     ```go
     if runAll {
         profiles, err := profile.List()
         if err != nil {
             return err
         }
         if len(profiles) == 0 {
             return fmt.Errorf("no active profiles found")
         }
         agyArgs := args
         var lastErr error
         for i, p := range profiles {
             fmt.Fprintf(os.Stderr, "\n[agys] Executing on profile %q (%d/%d)...\n", p, i+1, len(profiles))
             if err := runWithProfile(cmd, p, agyArgs); err != nil {
                 fmt.Fprintf(os.Stderr, "[agys] Profile %q failed: %v\n", p, err)
                 lastErr = err
             }
         }
         return lastErr
     }
     ```

- [ ] **Step 4: Run tests to verify they pass**

Run: `rtk go test ./...`
Expected: PASS

- [ ] **Step 5: Build and test manually**

Run: `rtk go build -o agys .`
Run: `rtk ./agys run --all -- agy --help`
Expected: Successfully iterates over all profiles.

- [ ] **Step 6: Commit**

```bash
rtk git add cmd/run.go cmd/run_test.go
rtk git commit -m "feat: add --all / -a flag to agys run command"
```
