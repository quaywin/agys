# AGENTS.md — Development Guidelines for agys

## ⚠️ Critical Binary Deployment Rule

> [!CAUTION]
> **NEVER** copy, build, overwrite, or delete the binary at `/Users/quaywin/.local/bin/agys` directly during agent tasks.
> (e.g. `cp ... /Users/quaywin/.local/bin/agys`, `go build -o /Users/quaywin/.local/bin/agys`, `rm /Users/quaywin/.local/bin/agys`).
>
> **Reason**: Overwriting a live executable binary on macOS while active terminal panes, background watchers, or Herdr hooks are running will corrupt running process mappings and trigger an instant kernel SIGKILL (`[1] <PID> killed agys`).

### ✅ Allowed Build & Test Procedures
1. **Local Build & Test**:
   ```bash
   go build ./...
   go test -v ./...
   ```
2. **Local Binary in Repository**:
   ```bash
   go build -o ./bin/agys .
   ```
3. **Go Install (Standard)**:
   ```bash
   go install .
   ```
   *(Installs safely to `$GOPATH/bin/agys` without touching the system path).*

---

## 🛠️ Project Architecture & Best Practices

- **Language**: 100% Pure Go (Zero Python / pip / asdf / shims dependency).
- **Herdr Integration**:
  - `cmd/herdr_hook.go`: Cobra subcommand `agys herdr-hook [session|quota]`.
  - `pkg/profile/herdr.go`: Lifecycle hook handler, socket RPC, compact sidebar badge (`FormatModelAbbreviation`), and Window Title (`5H` & `Weekly` quota).
  - `pkg/profile/quota.go`: `GetProfileFullQuotaDetailsForModel` with token-based dynamic matching and 3-tier fallback.
- **Model Resolution Order**:
  `Explicit -m/--model arg` -> `Live prompt transcript (USER_SETTINGS_CHANGE)` -> `.active_model cache` -> `settings.json` -> `default Gemini`.
- **Quality Assurance**:
  - Always run `go test ./...` and ensure zero test failures before completing tasks.
  - Keep `go vet ./...` clean without warnings.
