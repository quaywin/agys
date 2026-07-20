# Implementation Plan: `agys`

## Architecture & Structure
1. `pkg/profile`: Core profile domain logic (path resolution, profile folder existence, validation, listing, deletion, command execution wrapper).
2. `cmd`: Cobra subcommand handlers (`root`, `add`, `list`, `delete`, `run`).
3. `main.go`: Application entrypoint calling `cmd.Execute()`.
4. Release & CI/CD: `.goreleaser.yaml` and `.github/workflows/release.yml`.
5. Installer: `install.sh` shell script.

## Sequential Steps
1. Initialize Go module (`go.mod`) and Cobra CLI dependency.
2. Implement `pkg/profile` utility package.
3. Implement `cmd/root.go`, `cmd/add.go`, `cmd/list.go`, `cmd/delete.go`, and `cmd/run.go`.
4. Create `main.go` and verify local build & CLI capabilities.
5. Create `.goreleaser.yaml` and `.github/workflows/release.yml`.
6. Write production-ready POSIX `install.sh`.
