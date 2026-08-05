# Design Spec: `agys plugin` Command

## Overview
Add a dedicated `agys plugin` subcommand suite (`install`, `list`, `uninstall`) with first-class `--all` (`-a`) support. This command executes plugin management directly in isolated profile environments without the overhead of full CLI conversation session initialization.

## Command Syntax
- `agys plugin install <target> [profile_name] [--all/-a]`
- `agys plugin list [profile_name] [--all/-a]`
- `agys plugin uninstall <name> [profile_name] [--all/-a]`

## Behavior & Requirements
1. **Lightweight Execution**:
   - Executes `agy plugin ...` by setting `HOME=<profileDir>` without invoking conversation tracking, keychain sync output, or interactive session banners.
2. **Clean Output Format**:
   - When `--all` / `-a` is passed, prints compact, clean progress markers per profile:
     ```text
     [1/6] tram520      ✔ superpowers installed
     [2/6] quaywin      ✔ superpowers installed
     ...
     [agys] Plugin superpowers processed across 6 profiles.
     ```
3. **Single / Default Profile Support**:
   - If `--all` is false and no profile name is provided, uses the current default profile (via `profile.GetCurrent()`).
4. **Aliases**:
   - Register `agys plugins` as an alias for `agys plugin`.

## File Changes
- Create `cmd/plugin.go` with Cobra command structures for `pluginCmd`, `pluginInstallCmd`, `pluginListCmd`, and `pluginUninstallCmd`.
- Create `cmd/plugin_test.go` with unit tests for flag parsing and command registration.
- Update `README.md` with usage examples.

## Verification
- Unit tests: `rtk go test ./cmd -run TestPluginCommand`
- Full test suite: `rtk go test ./...`
- Manual execution of `agys plugin install https://github.com/obra/superpowers --all`.
