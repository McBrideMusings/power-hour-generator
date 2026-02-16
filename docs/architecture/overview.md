# Architecture Overview

Power Hour Generator is structured as a layered Go application where the CLI delegates to internal packages for configuration, caching, rendering, and project resolution.

## High-Level Flow

```
CSV Plan + YAML Config
        │
        ▼
  Project Resolver ──── resolves clips from config + CSV
        │
        ▼
  Cache Service ──────── downloads/indexes source media
        │
        ▼
  Render Service ─────── builds FFmpeg filter graphs, runs workers
        │
        ▼
  Segment Files ──────── normalized MP4s in segments/
```

## Package Layout

```
cmd/powerhour/
  main.go                   # Entry point → cli.Execute()

internal/
  cli/                      # Cobra commands (init, fetch, render, validate, etc.)
  config/                   # YAML config parsing with defaults
  cache/                    # Source download caching and index management
  render/                   # FFmpeg filter graph construction and execution
  project/                  # Config + CSV → clip timeline resolution
  paths/                    # Project directory layout resolution
  tools/                    # External tool detection and installation
  tui/                      # Bubbletea progress display (StatusWriter, ProgressModel)
  logx/                     # File-based logging (project + global ~/.powerhour/logs/)

pkg/
  csvplan/                  # CSV/TSV loading and validation
```

## Key Design Decisions

- **No backwards compatibility**: Freely break older project layouts; migrate forward instead of supporting legacy configs
- **Single source of truth**: CSV plan and YAML config are authoritative; caches are derived state
- **Template system**: Download filenames use `$TOKEN` placeholders; overlay text uses `{token}` brace syntax. Dynamic fields from CSV columns are available in both.
- **Overlay profiles** under `profiles.overlays` are the single source of truth for overlay configuration
- **Collections over legacy clips**: The collections system is the primary path; legacy `clips.song` architecture is being removed

## Data Flow

1. **CLI** parses flags and loads config
2. **Config** merges YAML with defaults into strongly-typed structs
3. **Project Resolver** or **Collection Resolver** combines config + CSV into clip lists
4. **Cache Service** maps clips to source files, downloading via yt-dlp or copying local files
5. **Render Service** builds FFmpeg commands with filter graphs and runs them in parallel
6. **Output** is individual MP4 segments in the project's `segments/` directory

## Subsystem Details

- [CLI Layer](/architecture/cli) — Cobra command structure and flag handling
- [Config System](/architecture/config) — YAML parsing, defaults, and validation
- [Cache System](/architecture/cache) — Source download management and indexing
- [Render Pipeline](/architecture/render) — FFmpeg filter graphs and parallel execution
- [CSV Loading](/architecture/csv-loading) — Plan file parsing and schema validation
- [Tool Management](/architecture/tools) — Detection, installation, and version caching
- [TUI System](/architecture/tui) — Bubbletea progress display and status feedback
