# Power Hour Generator

Power Hour Generator is a cross-platform CLI, written in Go, that orchestrates `yt-dlp` and `ffmpeg` to produce power hour clip libraries. It ingests a structured CSV describing each clip, manages project-local caches of source footage, and renders normalized MP4 segments with consistent overlays. The initial release focuses on generating discrete per-clip files that you can assemble in an editor of your choice.

## Project status

The project is in active development. This document describes the planned capabilities for the first Go-based release. The previous Python implementation is being replaced and no longer applies.

## Planned capabilities

- Single self-contained binary, `powerhour`, targeting macOS, Windows, and Linux.
- CLI orchestrates downloads via `yt-dlp`, probes/transcodes with `ffmpeg`/`ffprobe`, and hides those toolchains behind a unified interface.
- Project-oriented workflow: each project is an on-disk directory with standardized file names alongside `cache/`, `logs/`, `segments/`, and a hidden `.powerhour/index.json`.
- Automatic download caching to avoid re-fetching source media across multiple renders.
- Overlay system built from reusable segments that can each define text, transforms, timing, and positioning (defaults cover title + artist on entry, optional outro name, and a persistent index badge).
- Configurable video/audio encoding parameters and overlay styling via optional YAML.
- Normalized output: H.264 video at CRF 20 (`veryfast`) with AAC audio (192 kbps, 48 kHz) by default.
- One MP4 per source row; concatenation workflows will be addressed in a later release.

## Workflow overview

1. Create a project directory and add a `powerhour.csv` (or TSV) describing the clips in playback order.
2. (Optional) Add a `powerhour.yaml` to override fonts, colors, overlay timing, or encoding defaults.
3. Run the CLI pointing at the project directory; the tool will download sources into `cache/`, render segments into `segments/`, write logs under `logs/`, and maintain metadata in `.powerhour/index.json`.
4. Import the generated segment files into your preferred editor to build the final compilation.

Currently implemented commands cover project scaffolding, validation, cache population, tool management, and segment rendering.

### CLI commands

- `powerhour init --project <dir>` – create the project directory, starter CSV, and default YAML.
- `powerhour check --project <dir> [--strict]` – verify configuration and external tool availability (fails on missing tools when `--strict` is set).
- `powerhour config show --project <dir>` – print the effective configuration (defaults applied) as YAML.
- `powerhour config edit --project <dir>` – open the project configuration in `$EDITOR`, creating a starter file when missing.
- `powerhour status --project <dir> [--json]` – print the parsed song plan and any validation issues.
- `powerhour fetch --project <dir> [--force] [--reprobe] [--no-download] [--no-progress] [--index <n|n-m>] [--json]` – match existing cache files and download or copy missing sources, refreshing probe metadata. Optional flags: `--force` re-downloads even when cached, `--reprobe` runs ffprobe on cached files, `--no-download` skips new downloads and only reindexes existing files, `--no-progress` disables the interactive progress table, `--index` limits work to specific 1-based plan rows (single values or ranges, repeatable), and `--json` emits machine-readable output.
- `powerhour validate filenames --project <dir> [--index <n>] [--json]` – audit cached source filenames against the active template, renaming cached files that no longer match. Repeat `--index` to target specific rows.
- `powerhour validate segments --project <dir> [--index <n>] [--json]` – reconcile rendered segment filenames/logs with the configured template, renaming legacy outputs when possible.
- `powerhour tools list [--json]` – report resolved tool versions and locations.
- `powerhour tools install [tool|all] [--version <v>] [--force] [--json]` – install or update managed tools in the local cache.
- `powerhour render --project <dir> [--concurrency N] [--force] [--no-progress] [--index <n|n-m>] [--json]` – render cached rows into `segments/`, applying scaling, fades, overlays, audio resampling, and loudness normalization. `--concurrency` limits parallel ffmpeg processes, `--force` overwrites existing segment files, `--no-progress` disables the interactive progress table, `--index` restricts work to specific plan rows (single values or ranges, repeatable), and `--json` emits structured output.

The global `--json` flag applies to every command for machine-readable output when supported.

### Known issues

- The interactive render progress table currently prints an initial “starting” snapshot followed by the live table. The second snapshot contains the accurate state, but the redundant first table will be removed in an upcoming fix; use `--no-progress` as a temporary workaround if you prefer the legacy per-line output.

## Input CSV schema

The CSV (or TSV) columns are ordered by playback index:

- `title` (string) – Song or video title.
- `artist` (string) – Artist name.
- `start_time` (string) – `H:MM:SS[.ms]` or `M:SS[.ms]` trim start.
- `duration` (int) – Clip length in seconds.
- `name` (string, optional) – End-credit text to display near clip end.
- `link` (string) – Media source URL or local file path.

Example TSV:

```tsv
title	artist	start_time	duration	name	link
CHAMBEA	BAD BUNNY	1:50	65	pierce	https://youtu.be/gpIBmED4oss
```

## Project layout

```
project-root/
  powerhour.csv
  powerhour.yaml   # optional configuration
  cache/           # cached source downloads
  segments/        # rendered clip outputs
  logs/            # per-clip render logs
  .powerhour/
    index.json     # metadata about processed clips
```

## Overlay and rendering configuration

The optional `powerhour.yaml` lets you fine-tune rendering defaults:

```yaml
version: 1
video:
  width: 1920
  height: 1080
  fps: 30
  codec: libx264
  crf: 20
  preset: medium
audio:
  acodec: aac
  bitrate_kbps: 192
  sample_rate: 48000
  channels: 2
  loudnorm:
    enabled: true
    integrated_lufs: -14
    true_peak_db: -1.5
    lra_db: 11
outputs:
  segment_template: "$INDEX_PAD3_$SAFE_TITLE"
profiles:
  overlays:
    song-main:
      default_style:
        font_file: ""
        font_size: 42
        font_color: white
        outline_color: black
        outline_width: 2
        line_spacing: 4
      segments:
        - name: intro-title
          template: '{title}'
          style:
            font_size: 64
          position:
            origin: bottom-left
            offset_x: 40
            offset_y: 220
          timing:
            start:
              type: from_start
              offset_s: 0
            end:
              type: from_start
              offset_s: 4
            fade_in_s: 0.5
            fade_out_s: 0.5
        - name: intro-artist
          template: '{artist}'
          transform: uppercase
          style:
            font_size: 32
          position:
            origin: bottom-left
            offset_x: 40
            offset_y: 160
          timing:
            start:
              type: from_start
              offset_s: 0
            end:
              type: from_start
              offset_s: 4
            fade_in_s: 0.5
            fade_out_s: 0.5
        - name: index-badge
          template: '{index}'
          style:
            font_size: 140
          position:
            origin: bottom-right
            offset_x: 40
            offset_y: 40
          timing:
            start:
              type: from_start
              offset_s: 0
            end:
              type: persistent
clips:
  overlay_profile: song-main
  song:
    source:
      plan: powerhour.csv
      default_duration_s: 60
    render:
      fade_in_s: 0.5
      fade_out_s: 0.5
    overlays:
      profile: song-main
files:
  plan: powerhour.csv
  cookies: cookies.txt
downloads:
  filename_template: "$INDEX_$ID"
plan:
  default_duration_s: 60
  headers:
    duration: ["length"]
    start_time: ["start"]

tools:
  yt-dlp:
    minimum_version: latest
    proxy: socks5://127.0.0.1:9050
```

Each segment inherits properties from its profile’s `default_style` and can override font, colors, spacing, or even choose a different font file. `transform` supports `uppercase` or `lowercase`, letting you tweak casing without touching the source CSV. Timing anchors accept `from_start`, `from_end`, `absolute`, or `persistent`, and fades apply per segment. Position helpers (`origin`, `offset_x`, `offset_y`) compute sensible `drawtext` expressions, but you can always provide explicit `x`/`y` expressions for advanced layouts.

Use the optional `files` block to point at a different CSV/TSV plan or supply a cookies text file that will be passed to `yt-dlp` during fetches.

`profiles.overlays.<name>.default_style.font_file` expects a path to a TrueType or OpenType file. Leave it empty to fall back to FFmpeg's default font, or point at a font in `/System/Library/Fonts`, `/Library/Fonts`, or `~/Library/Fonts` on macOS (similar platform-specific font folders work on other OSes).

Provide alternate column names under `plan.headers` when your CSV uses friendly titles (e.g., map `duration` to accept `length`). Each canonical field can list multiple acceptable header strings; when omitted, the loader falls back to the standard schema. The `plan.default_duration_s` value supplies a project-wide fallback (default 60 seconds) that applies when the `duration` column is absent or empty, while per-row values still override it when present.

The audio block now exposes `sample_rate` plus an optional `loudnorm` section for EBU R128-style loudness normalization. Adjust the targets to match your delivery specs or disable normalization by setting `enabled: false`.

Use the `outputs.segment_template` string to control rendered segment filenames. The default (`$INDEX_PAD3_$SAFE_TITLE`) mirrors the existing behaviour (`001_teenagers.mp4`). Tokens are replaced with sanitized values so the final name is filesystem-safe; use `$$` to emit a literal dollar sign. Available segment tokens include:

- `$INDEX_PAD2`, `$INDEX_PAD3`, `$INDEX_PAD4` – zero-padded plan index (width 2/3/4).
- `$INDEX`, `$INDEX_RAW`, `$ROW_ID` – plan index without padding.
- `$TITLE`, `$ARTIST`, `$NAME`, `$START`, `$DURATION` – sanitized values from the CSV/TSV.
- `$SAFE_TITLE`, `$SAFE_ARTIST`, `$SAFE_NAME` – lowercased slug variants (hyphen separated).
- `$ID`, `$SAFE_ID` – cache identifier derived from the resolved source.
- `$SOURCE_BASENAME`, `$SAFE_SOURCE_BASENAME` – base name of the cached source file.

Example: `segment_template: "$ID_$INDEX_$TITLE_$NAME"` produces names such as `0J3vgcE5i2o_028_Chic_C_est_La_Vie_Madison.mp4`. When a token resolves to an empty string it’s simply omitted; repeated separators are collapsed automatically.

Set explicit tool requirements under the optional `tools` block. Provide a concrete version string or use the keyword `latest` to enforce the most recent release when running checks or installs. Supply a `proxy` value (for example, `socks5://127.0.0.1:9050`) when `yt-dlp` should run through a specific network proxy.

Control source cache filenames via the optional `downloads.filename_template` setting. When omitted, clips save as `<id>.<ext>`. Templates accept `$` placeholders; use `$$` to emit a literal dollar sign. Available substitutions include:

- `$ID` – Remote resources: yt-dlp media identifier; Local files: sanitized source basename (fallback to hash when unavailable).
- `$INDEX` / `$INDEX_PAD3` – Zero-padded (width 3) plan index.
- `$INDEX_RAW` / `$ROW_ID` – Unpadded plan index.
- `$HASH` / `$HASH10` / `$KEY` / `$KEY10` – SHA-256 hash of the source identifier (full or first 10 characters).
- `$TITLE`, `$ARTIST`, `$NAME`, `$START`, `$DURATION` – Sanitized values from the plan row.
- `$SOURCE_HOST`, `$SOURCE_ID` – Source URL hostname and identifier (sanitized).

Combine tokens to suit your workflow; for example, `$INDEX_$ID` recreates the previous default while `$TITLE_$ID` produces human-readable names. Run `powerhour validate filenames --project <dir>` anytime to audit and automatically rename existing cache files to the current template.

To refresh cached binaries after tightening a minimum, run `powerhour tools install all --project <project_dir> --force`. Drop `--force` if you only need to install binaries that are currently below the configured threshold.

All fields are optional; missing values fall back to built-in defaults. Templates use brace-delimited tokens that resolve against the clip metadata.

## External tools

`powerhour` manages the presence of its external dependencies, downloading them into a per-user cache when needed:

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) for media retrieval.
- [`ffmpeg`/`ffprobe`](https://ffmpeg.org/) for probing and rendering.

Users do not need to install these separately, but locally-installed versions will be reused when possible.

## Testing

Run the full suite with:

```bash
go test ./...
```

In sandboxed environments where the default Go build cache is not writable, point `GOCACHE` at a temporary directory for the duration of the run:

```bash
GOCACHE=$(mktemp -d) go test ./...
```

## Development

The CLI is being implemented in Go. Planned development workflow:

- Requires Go (1.21+ recommended) and a standard toolchain on macOS, Windows, or Linux.
- Cross-compilation and release packaging will produce static binaries per platform.
- Continuous integration and additional tooling are still being defined.

### Current CLI validation

- `gofmt -w $(find cmd internal -name '*.go')` – ensures all Go sources stay canonically formatted before builds.
- `go build ./...` – compiles every package to confirm the CLI scaffolding and dependencies link cleanly.
- `go run ./cmd/powerhour init --project sample_project` – smoke-tests project initialization, generating the cache/logs/segments directories, `.powerhour/index.json`, the default CSV/YAML, and logging the run.
- `go run ./cmd/powerhour check --project sample_project --strict` – exercises configuration loading, external tool probes, and fails when required tooling is missing or outdated.
- `go run ./cmd/powerhour fetch --project sample_project --force --reprobe` – populates the source cache and refreshes probe data for every row.
- `go run ./cmd/powerhour status --project sample_project --json` – parses `powerhour.csv`, prints the formatted table, and emits machine-readable output for automated checks.
- `go run ./cmd/powerhour tools list --json` – enumerates resolved tool paths and versions.

Contributions, issue reports, and feature ideas are welcome as the Go implementation takes shape.
