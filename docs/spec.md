# Spec: Antigravity CLI Switcher (`agys`)

## Objective
`agys` (Antigravity CLI Switcher) is an open-source Go-based CLI utility designed to manage multiple isolated account profiles for the `agy` CLI tool. It achieves account isolation by dynamically overriding the `HOME` environment variable for `agy` execution to profile-specific base directories under `~/.antigravity-profiles/<profile_name>/`.

## Tech Stack
- Language: Go (Golang) 1.22+
- CLI Framework: `github.com/spf13/cobra`
- Packaging & Release: GoReleaser + GitHub Actions
- Installer: POSIX-compliant Shell Script (`install.sh`)

## Commands & Subcommands
- `agys add <profile_name>`:
  - Validates profile name (alphanumeric, dashes, underscores).
  - Creates directory `~/.antigravity-profiles/<profile_name>`.
  - Runs `HOME=~/.antigravity-profiles/<profile_name> agy login` attached to `os.Stdin`, `os.Stdout`, `os.Stderr`.
- `agys list` (alias `ls`):
  - Scans `~/.antigravity-profiles/`.
  - Displays list of configured profile directories.
- `agys delete <profile_name>` (alias `rm`):
  - Prompts for user confirmation `[y/N]`.
  - On confirmation, removes directory `~/.antigravity-profiles/<profile_name>`.
- `agys run <profile_name> -- [agy_commands...]`:
  - Validates profile existence.
  - Prepares `exec.Cmd` executing `agy [agy_commands...]` with `HOME=~/.antigravity-profiles/<profile_name>`.
  - Directs `os.Stdin`, `os.Stdout`, `os.Stderr` to preserve terminal interactive behaviors and TTY features.
- `agys quota [profile_name]` (alias `q`):
  - Checks model quota and usage for one or all profiles.
  - Queries Google's internal APIs using the profile's OAuth token.
  - Displays remaining quota percentage and refresh windows.
  - Supports `--json` flag to output results in JSON format.

## Project Structure
```text
agys/
├── .github/
│   └── workflows/
│       └── release.yml
├── .goreleaser.yaml
├── docs/
│   └── spec.md
├── tasks/
│   ├── plan.md
│   └── todo.md
├── cmd/
│   ├── root.go
│   ├── add.go
│   ├── list.go
│   ├── delete.go
│   ├── run.go
│   └── quota.go
├── pkg/
│   └── profile/
│       ├── profile.go
│       ├── quota.go
│       └── profile_test.go
├── install.sh
├── main.go
└── go.mod
```

## Testing Strategy
- Unit tests for profile name validation and profile directory handling in `pkg/profile/`.
- Integration unit tests for CLI subcommand definitions and argument parsing using Cobra execution test harnesses.

## Boundaries
- Always: Preserve user input/output streams completely during `run` and `add` operations.
- Ask first: File/directory deletions (delete command prompts for confirmation).
- Never: Modify files outside `~/.antigravity-profiles/<profile_name>` during profile manipulation.

## Success Criteria
1. Complete Go codebase compiling cleanly.
2. Full functional support for `add`, `list`, `delete`, and `run` subcommands.
3. `.goreleaser.yaml` supporting `darwin/linux` and `amd64/arm64` targets.
4. `.github/workflows/release.yml` triggering on tags matching `v*`.
5. Robust `install.sh` POSIX shell script supporting auto-arch detection, latest tag resolution via GitHub API, download, checksum/extraction, and installation into `$PATH`.
