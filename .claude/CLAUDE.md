# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Power Hour Generator (`powerhour`) is a Go CLI that orchestrates `yt-dlp` and `ffmpeg` to produce power hour video clip libraries. It ingests CSV/TSV files describing clips, manages a project-local cache of source footage, and renders normalized MP4 segments with configurable text overlays.

## Build & Test Commands

```bash
# Format code
gofmt -w $(find cmd internal pkg -name '*.go')

# Build all packages
go build ./...

# Build CLI binary
go build -o powerhour ./cmd/powerhour

# Run all tests
go test ./...

# Run tests in sandboxed/constrained environments
GOCACHE=$(mktemp -d) go test ./...

# Run a single package's tests
go test ./internal/render/...
go test ./pkg/csvplan/...

# Vet
go vet ./...
```

## Architecture

**Entry point**: `cmd/powerhour/main.go` → delegates to `internal/cli.Execute()`.

**CLI layer** (`internal/cli/`): Cobra-based commands. Each file corresponds to a command (`init.go`, `fetch.go`, `render.go`, `validate.go`, etc.). Collection-aware variants live in `collections_fetch.go` and `collections_render.go`. Global flags: `--project`, `--json`, `--index n|n-m`, `--collection <name>`.

**Config** (`internal/config/`): YAML config parsed into strongly-typed structs with defaults. Profiles define reusable overlay collections. Collections and legacy `clips.song` are mutually exclusive.

**Cache** (`internal/cache/`): Index-based caching with `.powerhour/index.json` tracking source identifiers, cached file paths, and ffprobe metadata. Source resolution uses yt-dlp for URLs, direct reference for local files. `runner.go` abstracts command execution for testability.

**Render** (`internal/render/`): Builds FFmpeg filter graphs (scale, pad, fade, drawtext overlays, loudnorm). `filters.go` constructs the filter chains. `templates.go` handles `$TOKEN`-based filename expansion. `service.go` orchestrates parallel ffmpeg workers.

**Project resolver** (`internal/project/`): Resolves config + CSV into an executable clip timeline. `resolver.go` for legacy clips, `collections.go` for collection-based projects.

**CSV loading** (`pkg/csvplan/`): Auto-detects CSV vs TSV. `loader.go` for standard schema (title, artist, start_time, duration, name, link). `collection.go` for collection-specific loading with configurable header mappings. All CSV columns captured in `CustomFields` map for dynamic template tokens.

**Paths** (`internal/paths/`): `ProjectPaths` struct resolves standard project directory layout (cache/, segments/, logs/, .powerhour/).

**Tools** (`internal/tools/`): Auto-detects or installs yt-dlp/ffmpeg/ffprobe to per-user cache (`~/Library/Application Support/PowerHour/bin/` on macOS). `EnsureAll()` is the preferred entry point — it calls `Detect()` once for all tools and only installs what's missing. `release_cache.go` caches GitHub API responses (1h TTL) so `minimum_version: latest` doesn't hit the network every run. `detect.go` uses checksum-based manifest trust to skip slow `--version` shell-outs when the binary hasn't changed.

**TUI** (`internal/tui/`): Bubbletea-based progress display. `StatusWriter` (`status.go`) provides a pre-TUI spinner with elapsed time per phase. `ProgressModel` (`progress.go`) renders a live table with tick animation, marquee scrolling for long values, and a spinner footer. `RunWithWork` (`run.go`) bridges the work goroutine and bubbletea event loop (50ms startup delay + 5ms per-send yield to avoid render races).

**Logging** (`internal/logx/`): `NewGlobal(prefix)` writes to `~/.powerhour/logs/<prefix>-<timestamp>.log` for debugging tool detection and fetch operations.

## Key Design Decisions

- **No backwards compatibility**: Freely break older project layouts; migrate forward instead of supporting legacy configs.
- **Single source of truth**: CSV plan and YAML config are authoritative; caches are derived state.
- **Template system**: Download filenames use `$TOKEN` placeholders. Overlay text uses `{token}` brace syntax (case-insensitive). Dynamic fields from CSV columns are automatically available in both.
- **Overlay profiles** under `profiles.overlays` are the single source of truth for overlay configuration. Do not recreate legacy `overlays` blocks.
- **Timing anchors**: `from_start`, `from_end`, `absolute`, `persistent`. Each overlay segment supports independent fade in/out.
- **Never show a blank screen**: Every CLI phase must have visible progress feedback — use `StatusWriter` for setup phases and `ProgressModel` for main work.
- **Tool detection performance**: Avoid redundant `Detect()` calls (use `EnsureAll` not per-tool `Ensure`). Trust manifest checksums to skip `yt-dlp --version` (can take 6-7s). Cache GitHub API responses for `minimum_version: latest`. Version-qualify download filenames to prevent stale binary reuse.

## Testing Patterns

- Standard `testing` package with table-driven tests.
- Temp directories for file-based tests.
- Mock command runners in cache tests (`runner.go` abstraction).
- `test_helpers_test.go` in render package for shared test utilities.
- `newCacheService` var in `fetch.go` is typed for testability; `newCacheServiceWithStatus` adds status callback support.
- Known pre-existing failures in `internal/cli/status_test.go` (`TestStatusCommandTableOutput`, `TestStatusCommandJSONOutput`) — unrelated to TUI/tools work.

## Documentation Site

VitePress docs live in `docs/`. Run the dev server from the `docs/` directory:

```bash
cd docs && npm run docs:dev    # starts on port 5193
cd docs && npm run docs:build  # production build
```

Sections: guide, architecture, development, roadmap.
