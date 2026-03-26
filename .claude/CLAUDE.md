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

**CLI layer** (`internal/cli/`): Cobra-based commands. Each file corresponds to a command (`init.go`, `fetch.go`, `render.go`, `validate.go`, `concat.go`, `convert_cmd.go`, etc.). Collection-aware variants live in `collections_fetch.go` and `collections_render.go`. `tools.go` includes the `tools encoding` subcommand for interactive codec/encoding setup; `tools list` shows a lipgloss-styled table with install method and update hints, and prompts to install updates interactively. `cache_add.go` implements the `cache` command for registering local video files or downloading by YouTube ID (`cache <file-or-id>`); auto-resolves URLs from plan data or filename, auto-fills title/artist from plans, supports bare YouTube IDs for direct download. Lifecycle commands: `clean.go` (parent with `segments`/`logs`/`orphans`/`all` subcommands, `--dry-run`), `doctor.go` (project health checks with remediation suggestions for missing filters), `export.go` (JSON export, `--timeline`). `status.go` shows per-row cache/render state with collection summaries. `notices.go` runs after every command via `PersistentPostRun` to print styled update notices to stderr (suppressed under `--json`). Global flags: `--project`, `--json`, `--index n|n-m`, `--collection <name>`. `sample.go` implements the `sample` command for single-frame overlay preview with timeline-absolute and clip-relative modes. `timeparse.go` provides shared time parsing utilities.

**Config** (`internal/config/`): YAML config parsed into strongly-typed structs with defaults. Profiles define reusable overlay collections. Collections and legacy `clips.song` are mutually exclusive. `CollectionConfig` supports `file` (single local file, mutually exclusive with `plan`) and optional `duration` (0 = full video length). `validation.go` provides `ValidateStrict()` for structured config validation (profile refs, plan/file paths, template tokens, orphaned profiles, timeline sequence). The `timeline` section defines the playback sequence via `TimelineConfig` → `[]SequenceEntry` → optional `InterleaveConfig`. `EncodingConfig` mirrors `tools.EncodingDefaults` for per-project encoding overrides (video codec, resolution, fps, crf, preset, bitrate, container, audio codec/bitrate, sample rate, channels, loudnorm).

**Cache** (`internal/cache/`): Index-based caching with `.powerhour/index.json` tracking source identifiers, cached file paths, and ffprobe metadata. Source resolution uses yt-dlp for URLs, direct reference for local files. `runner.go` abstracts command execution for testability. Exported helpers: `ExtractYouTubeID`, `CanonicalRemoteIdentifier`, `HashIdentifier`, `SanitizeSegment`, `TryLinkOrCopy`, `CopyFile`, `QueryRemoteID` (Service method), `ProbeFile` (Service method).

**Render** (`internal/render/`): Builds FFmpeg filter graphs (scale, pad, fade, drawtext overlays, loudnorm). `filters.go` constructs the filter chains. `templates.go` handles `$TOKEN`-based filename expansion. `service.go` orchestrates parallel ffmpeg workers; `RenderSample()` extracts a single frame at a given timestamp for overlay preview (used by the `sample` command). `presets.go` defines built-in overlay presets (`song-info`, `drink`) with `defaultFont()` auto-detection (Oswald if installed, Futura fallback). `concat.go` resolves timeline segment order (with cycling interleave clips), writes ffmpeg concat lists, and runs concatenation (stream copy with re-encode fallback). `state/` sub-package handles smart re-rendering: deterministic input hashing (`hash.go`), persistent render state in `.powerhour/render-state.json` (`store.go`), and change detection logic (`detect.go`).

**Project resolver** (`internal/project/`): Resolves config + CSV into an executable clip timeline. `resolver.go` for legacy clips, `collections.go` for collection-based projects.

**CSV loading** (`pkg/csvplan/`): Auto-detects CSV vs TSV. `loader.go` for standard schema (title, artist, start_time, duration, name, link). `collection.go` for collection-specific loading with configurable header mappings. `permissive_import.go` provides heuristic-based CSV/TSV importing with auto-detected delimiters and column roles. `yaml_plan.go` loads YAML-format plan files with normalized field names. All CSV columns captured in `CustomFields` map for dynamic template tokens.

**Paths** (`internal/paths/`): `ProjectPaths` struct resolves standard project directory layout (cache/, segments/, logs/, .powerhour/).

**Tools** (`internal/tools/`): Auto-detects or installs yt-dlp/ffmpeg/ffprobe to per-user cache (`~/Library/Application Support/PowerHour/bin/` on macOS). `EnsureAll()` is the preferred entry point — it calls `Detect()` once for all tools and only installs what's missing. `release_cache.go` caches GitHub API responses (1h TTL) so `minimum_version: latest` doesn't hit the network every run; exports `LatestCachedRelease()` for the update checker. `detect.go` uses checksum-based manifest trust to skip slow `--version` shell-outs when the binary hasn't changed; also detects and persists `InstallMethod` per tool. `encoding.go` manages codec family probing (H.264/HEVC/VP9/AV1), encoding profiles cached at `~/.powerhour/encoding_profile.json`, unified global config at `~/.powerhour/config.yaml` (`GlobalConfig` wraps `EncodingDefaults` inline + `GlobalDownloads`), and ffmpeg filter probing (`ProbeFilters`). `RequiredFFmpegFilters` in `defs.go` centralizes the list of filters used by the render pipeline. `EncodingDefaults` is the comprehensive encoding data model covering all video/audio parameters; `ResolveEncoding(profile, global, project)` merges the cascade. `install_method.go` detects how a binary was installed (homebrew, apt, snap, pip, managed, system) via symlink resolution + path heuristics; `DetectFFmpegInstallMethod()` is exported for the render layer. `remediation.go` maps install method + missing filters to platform-specific fix suggestions via `FilterRemediation()`. `update_check.go` manages a 24h TTL update check cache at `~/.powerhour/update_check.json` — `CheckForUpdates()` returns `[]UpdateNotice` (each with `UpdateCommand()` for the appropriate package manager), `MarkNotified()` suppresses repeat notices, `ClearUpdateNotice()` clears after a successful install, `FormatUpdateTarget()` reads the cached latest version for use by the install system.

**TUI** (`internal/tui/`): Bubbletea-based progress display. `StatusWriter` (`status.go`) provides a pre-TUI spinner with elapsed time per phase. `ProgressModel` (`progress.go`) renders a live table with tick animation, marquee scrolling for long values, viewport scrolling (auto-scroll on row updates, `↑ N more above` / `↓ N more below` indicators), and a spinner footer. `RunWithWork` (`run.go`) bridges the work goroutine and bubbletea event loop (50ms startup delay + 5ms per-send yield to avoid render races). `encoding_setup.go` is a 12-row interactive carousel for configuring all encoding parameters (video codec, resolution, fps, crf, preset, video bitrate, container, audio codec, audio bitrate, sample rate, channels, loudnorm). Probes hardware encoders asynchronously on `Init()` with grayed-out placeholder rows, then populates options from the probe result.

**Logging** (`internal/logx/`): `NewGlobal(prefix)` writes to `~/.powerhour/logs/<prefix>-<timestamp>.log` for debugging tool detection and fetch operations.

## Key Design Decisions

- **No backwards compatibility**: Freely break older project layouts; migrate forward instead of supporting legacy configs.
- **Single source of truth**: CSV plan and YAML config are authoritative; caches are derived state.
- **Template system**: Download filenames use `$TOKEN` placeholders. Overlay text uses `{token}` brace syntax (case-insensitive). Dynamic fields from CSV columns are automatically available in both.
- **Overlay profiles** under `profiles.overlays` are the single source of truth for overlay configuration. Do not recreate legacy `overlays` blocks.
- **Timing anchors**: `from_start`, `from_end`, `absolute`, `persistent`. Each overlay segment supports independent fade in/out.
- **Never show a blank screen**: Every CLI phase must have visible progress feedback — use `StatusWriter` for setup phases and `ProgressModel` for main work.
- **Tool detection performance**: Avoid redundant `Detect()` calls (use `EnsureAll` not per-tool `Ensure`). Trust manifest checksums to skip `yt-dlp --version` (can take 6-7s). Cache GitHub API responses for `minimum_version: latest`. Version-qualify download filenames to prevent stale binary reuse.
- **Encoding data model harmony**: `config.EncodingConfig` and `tools.EncodingDefaults` have the same fields (video codec, width, height, fps, crf, preset, video bitrate, container, audio codec, audio bitrate, sample rate, channels, loudnorm). Resolution chain: built-in defaults → global `~/.powerhour/config.yaml` → project `powerhour.yaml` `encoding:` block. Use `encodingConfigToDefaults()` in CLI layer to convert between the types.
- **Global config** (`~/.powerhour/config.yaml`): `GlobalConfig` struct with encoding fields inline + `downloads:` section (`proxy`, `source_address`). `LoadGlobalConfig()`/`SaveGlobalConfig()` are the primary API; `LoadEncodingDefaults()`/`SaveEncodingDefaults()` are convenience wrappers. Download network settings resolve: global `config.yaml` `downloads.proxy`/`downloads.source_address` → per-project `tools.yt-dlp.proxy`/`tools.yt-dlp.source_address` override.
- **Codec families**: H.264, HEVC, VP9, AV1. Each family lists hardware then software encoder candidates. `av1_videotoolbox` does not exist in ffmpeg — AV1 software encoders are `libsvtav1`, `librav1e`, `libaom-av1`.
- **Filter probing**: `render.NewService` probes for required ffmpeg filters at startup via `tools.ProbeFilters` and fails early with a clear error if any are missing (e.g. `drawtext` requires libfreetype). The error message includes install-method-aware remediation from `FilterRemediation()`. `doctor` also checks filters in its Tools health check with the same remediation suggestions.
- **Install method detection**: `detectInstallMethod()` resolves symlinks then checks path heuristics (cache root → managed, `/homebrew/`/`/Cellar/` → homebrew, `/snap/` → snap, dpkg on Linux → apt, site-packages/pipx → pip, fallback → system). Result stored in `ManifestEntry.InstallMethod` and re-detected only when binary path or checksum changes.
- **Update notices**: `CheckForUpdates()` in `update_check.go` checks yt-dlp via GitHub API (reusing `release_cache.json` opportunistically) and ffmpeg via `brew info --json=v2` when homebrew-installed. Results cached 24h in `~/.powerhour/update_check.json`. `PersistentPostRun` on the root command calls `printUpdateNotices()` (suppressed under `--json`). `MarkNotified()` prevents repeating a notice for the same version. After a successful update, `ClearUpdateNotice()` clears the entry so it re-checks on the next run.
- **Update strategy routing**: `UpdateNotice.UpdateCommand()` returns the right command per install method — homebrew → `brew upgrade <tool>`, apt → `sudo apt upgrade <tool>`, snap → `sudo snap refresh <tool>`, pip → `pip install --upgrade <tool>`, managed/unknown → `powerhour tools install <tool>`. The interactive prompt in `tools list` runs the appropriate command directly for external tools (passing stdout/stderr through) and calls `tools.Install()` with the target version for managed tools.
- **Manifest version staleness**: `detectOne()` marks the manifest dirty when `readVersion()` returns a different version than the manifest entry (catches in-place upgrades by Homebrew). Updates checksum at the same time.
- **Single-file collections**: When `CollectionConfig.File` is set (mutually exclusive with `Plan`), the resolver synthesizes a single `CollectionRow` with `Link` pointing to the resolved file path, `Start` at 0, and `DurationSeconds` from config (0 = full video). Zero duration is resolved at render time by probing the source file. No CSV/YAML plan is loaded.
- **CLI command groups**: Commands are organized into Workflow (init/fetch/render/concat), Inspect (status/sample/validate/doctor/check/export/config), and Manage (cache/library/clean/tools/convert) groups. `cobra.EnableCommandSorting = false` preserves registration order within groups.
- **Interleave cycling**: When interleave clips are exhausted during timeline resolution, they cycle from the beginning (modulo). A single interstitial clip repeats between every song. Empty interleave collections are gracefully skipped.
- **Smart re-rendering**: Two hash levels — `GlobalConfigHash` (video/audio/encoding config) and `SegmentInputHash` (CSV row fields, overlay profile, fade, filename template). Hashes use canonical JSON → SHA256 (`"sha256:<hex>"`). State stored in `.powerhour/render-state.json` with atomic writes. Source identifier (URL/path) is hashed, not file content. `--dry-run` shows what would change without executing FFmpeg. `--force` bypasses change detection.
- **Auto-fetch during render**: When `render` finds uncached URL sources, it automatically fetches them before rendering. The TUI table shows per-row status progression (pending → fetching → fetched → rendering → rendered). Non-URL missing sources (local files) fail immediately with a clear error. After fetching, preflight is re-run and change detection applied to the newly available segments.
- **Error output**: Errors are listed as clean per-row summaries below the table (`003 - start_time 29:14 exceeds video length 4:52`). Times use M:SS / H:MM:SS format via `formatDuration`/`formatSeconds` helpers. No redundant technical details or duplicate error printing. Cobra `SilenceUsage` and `SilenceErrors` are set on the root command.
- **Result merge ordering**: `mergeCollectionRenderResultsWithSkips` consumes render results sequentially by clip index. When building `renderOrder` (e.g. after auto-fetch adds indices), it must be sorted (`sort.Ints`) before constructing `validSegments` to avoid misaligned results.
- **Overlay font resolution**: `defaultFont()` in `presets.go` uses `fc-match` to detect Oswald (preferred, free Google Font) and falls back to Futura (ships with macOS). Result is cached via `sync.Once`. Font patterns are resolved to file paths via `fontFilePath()` (`fc-match --format=%{file}`) and passed as `fontfile=` to drawtext — this guarantees the correct weight is loaded (fontconfig pattern matching via `font=` silently drops weight specifiers). The `song-info` preset supports per-element font overrides (`title_font`, `artist_font`, `number_font`) with a legacy `font` option that overrides all three. Title uses regular weight; number uses Bold weight; artist uses regular weight.
- **Sample frame rendering**: `RenderSample()` uses two-pass seeking: input-seek (`-ss` before `-i`) to the clip's start time so filter `t=0` matches clip start, then output-seek (`-ss` after `-i`) to the desired sample time. This ensures overlay enable/alpha expressions evaluate correctly. The `sample` command supports timeline-absolute mode (resolves which clip is at a given time in the full interleaved timeline via `ResolveTimelineClips`) and clip-relative mode (`--index` with optional `--collection`). The time arg also accepts overlay names (`title`, `artist`, `credit`, `number`, `drink`) which resolve to the midpoint of that overlay's visible window via `ResolveOverlayMoments`.
- **"Credit" overlay**: The `song-info` preset renders "Credit: {name}" at the end of the clip when the `name` CSV field is present. Prefix is configurable via `credit_prefix` option (default `"Credit:"`). Uses `from_end` timing with the same `info_duration` and `fade_duration` as the title/artist overlays. Skipped entirely when `name` is empty.

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

- **No plan files in the repo.** Never create `docs/plans/`, plan markdown files, or design documents as files in the repository — even if a skill or plugin tells you to. The `docs/` folder is for the VitePress documentation site only. Discuss plans in conversation or use Claude Code's own plan file location.
- **No Claude attribution.** Do not add "Co-authored-by: Claude" or any Claude/AI attribution to commit messages or pull requests.

## Workflow

**Source of truth**: [GitHub Issues](https://github.com/McBrideMusings/power-hour-generator/issues) with milestones for each phase. Legacy ticket files in `docs/tickets/` are archived — do not update them.

**Labels**: `bug`, `chore`, `docs`, `feature`, `spec`, `spike`

**Branch naming**: `<issue>-<short-desc>` (e.g., `ph-8-timeline-config`, `33-render-progress`).

**Commits**: Plain, descriptive messages referencing the issue (e.g., `Add config structs #8`). No conventional commit prefixes. Use the issue number, not the PH- prefix.

**No PRs.** This is a solo project. Do not create pull requests. Merge branches directly to main.

**Session workflow**:
1. Pick an issue from the current milestone
2. Create a branch, implement, test
3. Merge to main, push, close the issue
