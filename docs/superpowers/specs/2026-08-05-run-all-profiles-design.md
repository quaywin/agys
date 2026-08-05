# Design Spec: `agys run --all` / `-a` Flag

## Overview
Add an `--all` (`-a`) flag to the `agys run` command. When this flag is supplied, `agys` will sequentially execute the target `agy` command across all active profiles registered in `agys`.

## Requirements
1. **Flag Support**: Add `--all` (`-a`) boolean flag to `runCmd` in `cmd/run.go`.
2. **Profile Iteration**:
   - Fetch the list of profiles using `profile.List()`.
   - If no profiles exist, report an error.
   - For each profile in the list:
     - Output an informative banner to `os.Stderr` (e.g., `[agys] Executing command on profile "name" (X/Y)...`).
     - Execute the command via `runWithProfile`.
3. **Argument Handling**:
   - When `--all` / `-a` is present, positional arguments represent the `agy` command arguments (`agyArgs`), and no specific profile name needs to be passed as the first positional argument.
4. **Backward Compatibility**:
   - Without `--all` / `-a`, `agys run` behaves exactly as before.

## Test Plan
- Unit test for flag parsing and execution logic in `cmd/run_test.go` (or `cmd/run.go`).
- Verify manual execution with `go test ./...`.
- Verify build with `go build .`.
