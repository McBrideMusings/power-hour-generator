---
name: verify-project
description: Verify Power Hour Generator CLI builds, passes tests, and CLI surface works
---

# verify-project

Build the Go CLI, run all tests, and verify the command surface works end-to-end.

## Build

```bash
admin deploy
```

Expected output (last lines):
```
[2m→ wrote: tmp/deploy.2026-08-12_211007.log[0m
[32mgo install ./cmd/powerhour complete[0m
```

The build downloads dependencies (first run: ~10s), compiles with `go install ./cmd/powerhour`, and installs to `$GOBIN` (default `~/go/bin`, already on PATH after first `admin` setup).

## Tests

```bash
go test ./...
```

Expected: all 13 packages pass. Output tail:
```
ok  	powerhour/internal/render/state	0.517s
ok  	powerhour/internal/tools	1.742s
ok  	powerhour/internal/tui	0.348s
ok  	powerhour/internal/tui/dashboard	1.262s
ok  	powerhour/pkg/csvplan	1.883s
```

Some packages have no test files (`cmd/powerhour`, `internal/cachedoctor`) and show `[no test files]` — that's normal.

## CLI surface

```bash
powerhour --help
```

Verifies the binary is installed and all top-level subcommands are present: `init`, `add`, `fetch`, `render`, `concat`, `tui` (workflow), plus `status`, `sample`, `validate`, `doctor`, `check`, `export`, `config` (inspect), and `cache`, `library`, `clean`, `tools`, `convert` (manage).

```bash
powerhour check
```

Verifies external tool integration; expected to show ffmpeg, vlc, and yt-dlp versions, plus current encoding status. This command hits the actual tools on the system.

## What passes

- Build compiles without error
- All 13 test packages pass (no test failures)
- CLI responds to `--help` with full command tree
- `powerhour check` runs and reports tool availability

## Gotchas

- First `admin deploy` downloads the Go toolchain (~1.5 GB); runs are fast after.
- `powerhour check` requires yt-dlp and ffmpeg on PATH or in the project's tool cache; `powerhour tools install` populates the cache.
- The repo root has a pre-built `powerhour` binary (untracked build artifact, not source) — verify the binary comes from `~/go/bin` after `admin deploy`.
