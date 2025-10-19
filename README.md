# Power Hour Generator

Power Hour Generator is a cross-platform CLI, written in Go, that orchestrates `yt-dlp` and `ffmpeg` to produce power hour clip libraries. It ingests a structured CSV describing each clip, manages project-local caches of source footage, and renders normalized MP4 segments with consistent overlays. The initial release focuses on generating discrete per-clip files that you can assemble in an editor of your choice.

## Project status

The project is in active development. This document describes the planned capabilities for the first Go-based release. The previous Python implementation is being replaced and no longer applies.

## Planned capabilities

- Single self-contained binary, `powerhour`, targeting macOS, Windows, and Linux.
- CLI orchestrates downloads via `yt-dlp`, probes/transcodes with `ffmpeg`/`ffprobe`, and hides those toolchains behind a unified interface.
- Project-oriented workflow: each project is an on-disk directory with standardized file names and a `.powerhour` cache.
- Automatic download caching to avoid re-fetching source media across multiple renders.
- Overlay system that applies:
  - Title and artist text bottom-left on entry, with fade in/out.
  - Optional end credit text near clip end.
  - Persistent index badge bottom-right for the entire clip.
- Configurable video/audio encoding parameters and overlay styling via optional YAML.
- Normalized output: H.264 video at CRF 20 (`veryfast`) with AAC audio (192 kbps, 48 kHz) by default.
- One MP4 per source row; concatenation workflows will be addressed in a later release.

## Workflow overview

1. Create a project directory and add a `powerhour.csv` (or TSV) describing the clips in playback order.
2. (Optional) Add a `powerhour.yaml` to override fonts, colors, overlay timing, or encoding defaults.
3. Run the CLI pointing at the project directory; the tool will download sources into `.powerhour/src`, render segments into `.powerhour/segments`, and write logs and metadata alongside the outputs.
4. Import the generated segment files into your preferred editor to build the final compilation.

Currently implemented commands cover project scaffolding, validation, cache population, and tool management, with rendering-oriented subcommands to follow.

### CLI commands

- `powerhour init --project <dir>` – create the project directory, starter CSV, and default YAML.
- `powerhour check --project <dir> [--strict]` – verify configuration and external tool availability (fails on missing tools when `--strict` is set).
- `powerhour status --project <dir> [--json]` – print the parsed song plan and any validation issues.
- `powerhour fetch --project <dir> [--force] [--reprobe] [--json]` – download or copy sources into the cache and refresh probe metadata.
- `powerhour tools list [--json]` – report resolved tool versions and locations.
- `powerhour tools install [tool|all] [--version <v>] [--force] [--json]` – install or update managed tools in the local cache.

The global `--json` flag applies to every command for machine-readable output when supported.

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
  .powerhour/
    src/           # cached source downloads
    segments/      # rendered clip outputs
    logs/          # per-clip render logs
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
audio:
  acodec: aac
  bitrate_kbps: 192
overlays:
  font_file: ""
  font_size_main: 42
  font_size_index: 36
  color: "white"
  outline_color: "black"
  begin_text:
    template: "{title} — {artist}"
    duration_s: 4.0
    fade_in_s: 0.5
    fade_out_s: 0.5
  end_text:
    template: "{name}"
    offset_from_end_s: 4.0
    duration_s: 4.0
  index_badge:
    template: "{index}"
    persistent: true
files:
  plan: powerhour.csv
  cookies: cookies.txt

tools:
  yt-dlp:
    minimum_version: latest
```

Use the optional `files` block to point at a different CSV/TSV plan or supply a cookies text file that will be passed to `yt-dlp` during fetches.

Set explicit tool requirements under the optional `tools` block. Provide a concrete version string or use the keyword `latest` to enforce the most recent release when running checks or installs.

To refresh cached binaries after tightening a minimum, run `powerhour tools install all --project <project_dir> --force`. Drop `--force` if you only need to install binaries that are currently below the configured threshold.

All fields are optional; missing values fall back to built-in defaults. Templates use brace-delimited tokens that resolve against the clip metadata.

## External tools

`powerhour` manages the presence of its external dependencies, downloading them into a per-user cache when needed:

- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) for media retrieval.
- [`ffmpeg`/`ffprobe`](https://ffmpeg.org/) for probing and rendering.

Users do not need to install these separately, but locally-installed versions will be reused when possible.

## Development

The CLI is being implemented in Go. Planned development workflow:

- Requires Go (1.21+ recommended) and a standard toolchain on macOS, Windows, or Linux.
- Run unit tests with `go test ./...`.
- Cross-compilation and release packaging will produce static binaries per platform.
- Continuous integration and additional tooling are still being defined.

### Current CLI validation

- `gofmt -w $(find cmd internal -name '*.go')` – ensures all Go sources stay canonically formatted before builds.
- `go build ./...` – compiles every package to confirm the CLI scaffolding and dependencies link cleanly.
- `go run ./cmd/powerhour init --project sample_project` – smoke-tests project initialization, generating the `.powerhour/` structure plus default CSV/YAML and logging the run.
- `go run ./cmd/powerhour check --project sample_project --strict` – exercises configuration loading, external tool probes, and fails when required tooling is missing or outdated.
- `go run ./cmd/powerhour fetch --project sample_project --force --reprobe` – populates the source cache and refreshes probe data for every row.
- `go run ./cmd/powerhour status --project sample_project --json` – parses `powerhour.csv`, prints the formatted table, and emits machine-readable output for automated checks.
- `go run ./cmd/powerhour tools list --json` – enumerates resolved tool paths and versions.

Contributions, issue reports, and feature ideas are welcome as the Go implementation takes shape.
