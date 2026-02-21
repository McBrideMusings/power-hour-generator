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

**CLI layer** (`internal/cli/`): Cobra-based commands. Each file corresponds to a command (`init.go`, `fetch.go`, `render.go`, `validate.go`, `concat.go`, `convert_cmd.go`, etc.). Collection-aware variants live in `collections_fetch.go` and `collections_render.go`. `tools.go` includes the `tools encoding` subcommand for interactive codec/encoding setup. Global flags: `--project`, `--json`, `--index n|n-m`, `--collection <name>`.

**Config** (`internal/config/`): YAML config parsed into strongly-typed structs with defaults. Profiles define reusable overlay collections. Collections and legacy `clips.song` are mutually exclusive. `validation.go` provides `ValidateStrict()` for structured config validation (profile refs, plan paths, template tokens, orphaned profiles, timeline sequence). The `timeline` section defines the playback sequence via `TimelineConfig` → `[]SequenceEntry` → optional `InterleaveConfig`. `EncodingConfig` mirrors `tools.EncodingDefaults` for per-project encoding overrides (video codec, resolution, fps, crf, preset, bitrate, container, audio codec/bitrate, sample rate, channels, loudnorm).

**Cache** (`internal/cache/`): Index-based caching with `.powerhour/index.json` tracking source identifiers, cached file paths, and ffprobe metadata. Source resolution uses yt-dlp for URLs, direct reference for local files. `runner.go` abstracts command execution for testability.

**Render** (`internal/render/`): Builds FFmpeg filter graphs (scale, pad, fade, drawtext overlays, loudnorm). `filters.go` constructs the filter chains. `templates.go` handles `$TOKEN`-based filename expansion. `service.go` orchestrates parallel ffmpeg workers. `concat.go` resolves timeline segment order (with cycling interleave clips), writes ffmpeg concat lists, and runs concatenation (stream copy with re-encode fallback).

**Project resolver** (`internal/project/`): Resolves config + CSV into an executable clip timeline. `resolver.go` for legacy clips, `collections.go` for collection-based projects.

**CSV loading** (`pkg/csvplan/`): Auto-detects CSV vs TSV. `loader.go` for standard schema (title, artist, start_time, duration, name, link). `collection.go` for collection-specific loading with configurable header mappings. `permissive_import.go` provides heuristic-based CSV/TSV importing with auto-detected delimiters and column roles. `yaml_plan.go` loads YAML-format plan files with normalized field names. All CSV columns captured in `CustomFields` map for dynamic template tokens.

**Paths** (`internal/paths/`): `ProjectPaths` struct resolves standard project directory layout (cache/, segments/, logs/, .powerhour/).

**Tools** (`internal/tools/`): Auto-detects or installs yt-dlp/ffmpeg/ffprobe to per-user cache (`~/Library/Application Support/PowerHour/bin/` on macOS). `EnsureAll()` is the preferred entry point — it calls `Detect()` once for all tools and only installs what's missing. `release_cache.go` caches GitHub API responses (1h TTL) so `minimum_version: latest` doesn't hit the network every run. `detect.go` uses checksum-based manifest trust to skip slow `--version` shell-outs when the binary hasn't changed. `encoding.go` manages codec family probing (H.264/HEVC/VP9/AV1), encoding profiles cached at `~/.powerhour/encoding_profile.json`, and global encoding defaults at `~/.powerhour/encoding.yaml`. `EncodingDefaults` is the comprehensive encoding data model covering all video/audio parameters; `ResolveEncoding(profile, global, project)` merges the cascade.

**TUI** (`internal/tui/`): Bubbletea-based progress display. `StatusWriter` (`status.go`) provides a pre-TUI spinner with elapsed time per phase. `ProgressModel` (`progress.go`) renders a live table with tick animation, marquee scrolling for long values, and a spinner footer. `RunWithWork` (`run.go`) bridges the work goroutine and bubbletea event loop (50ms startup delay + 5ms per-send yield to avoid render races). `encoding_setup.go` is a 12-row interactive carousel for configuring all encoding parameters (video codec, resolution, fps, crf, preset, video bitrate, container, audio codec, audio bitrate, sample rate, channels, loudnorm). Probes hardware encoders asynchronously on `Init()` with grayed-out placeholder rows, then populates options from the probe result.

**Logging** (`internal/logx/`): `NewGlobal(prefix)` writes to `~/.powerhour/logs/<prefix>-<timestamp>.log` for debugging tool detection and fetch operations.

## Key Design Decisions

- **No backwards compatibility**: Freely break older project layouts; migrate forward instead of supporting legacy configs.
- **Single source of truth**: CSV plan and YAML config are authoritative; caches are derived state.
- **Template system**: Download filenames use `$TOKEN` placeholders. Overlay text uses `{token}` brace syntax (case-insensitive). Dynamic fields from CSV columns are automatically available in both.
- **Overlay profiles** under `profiles.overlays` are the single source of truth for overlay configuration. Do not recreate legacy `overlays` blocks.
- **Timing anchors**: `from_start`, `from_end`, `absolute`, `persistent`. Each overlay segment supports independent fade in/out.
- **Never show a blank screen**: Every CLI phase must have visible progress feedback — use `StatusWriter` for setup phases and `ProgressModel` for main work.
- **Tool detection performance**: Avoid redundant `Detect()` calls (use `EnsureAll` not per-tool `Ensure`). Trust manifest checksums to skip `yt-dlp --version` (can take 6-7s). Cache GitHub API responses for `minimum_version: latest`. Version-qualify download filenames to prevent stale binary reuse.
- **Encoding data model harmony**: `config.EncodingConfig` and `tools.EncodingDefaults` have the same fields (video codec, width, height, fps, crf, preset, video bitrate, container, audio codec, audio bitrate, sample rate, channels, loudnorm). Resolution chain: built-in defaults → global `~/.powerhour/encoding.yaml` → project `powerhour.yaml` `encoding:` block. Use `encodingConfigToDefaults()` in CLI layer to convert between the types.
- **Codec families**: H.264, HEVC, VP9, AV1. Each family lists hardware then software encoder candidates. `av1_videotoolbox` does not exist in ffmpeg — AV1 software encoders are `libsvtav1`, `librav1e`, `libaom-av1`.
- **Interleave cycling**: When interleave clips are exhausted during timeline resolution, they cycle from the beginning (modulo). A single interstitial clip repeats between every song. Empty interleave collections are gracefully skipped.

## Testing Patterns

- Standard `testing` package with table-driven tests.
- Temp directories for file-based tests.
- Mock command runners in cache tests (`runner.go` abstraction).
- `test_helpers_test.go` in render package for shared test utilities.
- `newCacheService` var in `fetch.go` is typed for testability; `newCacheServiceWithStatus` adds status callback support.
- Known pre-existing failure: `TestBuildFilterGraphIncludesOverlays` in `internal/render/filters_test.go` — unrelated to config/timeline work.
- `config` cannot import `render` (import cycle via `project`). When config validation needs render-owned data (e.g. valid template tokens), pass it as a parameter from the CLI layer.

## Documentation Site

VitePress docs live in `docs/`. Run the dev server from the `docs/` directory:

```bash
cd docs && npm run docs:dev    # starts on port 5193
cd docs && npm run docs:build  # production build
```

Sections: guide, architecture, development, roadmap.

## Agent Behavior

- **No plan files in the repo.** Do not create `docs/plans/` or save plan documents as files in the repository. Use Claude Code's own plan file location if needed, or just discuss plans in conversation.
- **No Claude attribution.** Do not add "Co-authored-by: Claude" or any Claude/AI attribution to commit messages or pull requests.

## Workflow

**Source of truth**: [GitHub Issues](https://github.com/McBrideMusings/power-hour-generator/issues) with milestones for each phase. Legacy ticket files in `docs/tickets/` are archived — do not update them.

**Labels**:
- Status: `status:todo`, `status:in-progress`, `status:blocked`, `status:done`
- Type: `type:feature`, `type:bug`, `type:chore`, `type:docs`, `type:spec`
- Priority: `priority:high`, `priority:medium`, `priority:low`

**Branch naming**: `<issue>-<short-desc>` (e.g., `ph-8-timeline-config`, `33-render-progress`).

**Commits**: Plain, descriptive messages referencing the issue (e.g., `Add config structs #8`). No conventional commit prefixes. Use the issue number, not the PH- prefix.

**No PRs.** This is a solo project. Do not create pull requests. Merge branches directly to main.

**Session workflow**:
1. Pick an issue from the current milestone
2. Create a branch, implement, test
3. Merge to main, push, close the issue
