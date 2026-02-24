# CLI

All commands accept a `--project <dir>` flag to specify the project directory and `--json` for machine-readable output.

## Project Commands

### `powerhour init`

Create a project directory with starter CSV, default YAML config, and standard directories.

```bash
powerhour init --project <dir>
go run ./cmd/powerhour init --project <dir>
```

### `powerhour check`

Verify configuration and external tool availability.

```bash
powerhour check --project <dir> [--strict]
go run ./cmd/powerhour check --project <dir> [--strict]
```

`--strict` fails on missing or outdated tools, and also validates configuration: profile references, plan file existence, segment template tokens, and orphaned profiles (warnings). Also displays encoding status (configured codec, container, bitrate, and probe date).

### `powerhour status`

Print the parsed song plan and any validation issues.

```bash
powerhour status --project <dir> [--json]
go run ./cmd/powerhour status --project <dir> [--json]
```

### `powerhour config show`

Print the effective configuration (defaults applied) as YAML.

```bash
powerhour config show --project <dir>
go run ./cmd/powerhour config show --project <dir>
```

### `powerhour config edit`

Open the project configuration in `$EDITOR`, creating a starter file when missing.

```bash
powerhour config edit --project <dir>
go run ./cmd/powerhour config edit --project <dir>
```

## Fetch & Render

### `powerhour fetch`

Download or copy source media into the project cache.

```bash
powerhour fetch --project <dir> [flags]
go run ./cmd/powerhour fetch --project <dir> [flags]
```

| Flag | Description |
|------|-------------|
| `--force` | Re-download even when cached |
| `--reprobe` | Run ffprobe on cached files |
| `--no-download` | Skip new downloads, only reindex existing files |
| `--no-progress` | Disable interactive progress table |
| `--index <n\|n-m>` | Limit to specific 1-based plan rows (repeatable) |
| `--collection <name>` | Target a specific collection |
| `--json` | Machine-readable output |

### `powerhour render`

Render cached sources into segments with scaling, fades, overlays, and audio normalization.

```bash
powerhour render --project <dir> [flags]
go run ./cmd/powerhour render --project <dir> [flags]
```

| Flag | Description |
|------|-------------|
| `--concurrency N` | Limit parallel ffmpeg processes |
| `--force` | Overwrite existing segment files (bypasses change detection) |
| `--dry-run` | Show what would be rendered or skipped without executing FFmpeg |
| `--no-progress` | Disable interactive progress table |
| `--index <n\|n-m>` | Limit to specific plan rows (repeatable) |
| `--collection <name>` | Target a specific collection |
| `--sample-time <time>` | Extract a single frame at specified time for testing overlays |
| `--sample-output <path>` | Output path for the sample frame |
| `--json` | Structured output |

Render tracks input hashes in `.powerhour/render-state.json` and automatically skips unchanged segments on subsequent runs. Use `--force` to bypass change detection, or `--dry-run` to preview what would happen.

### `powerhour concat`

Concatenate rendered segments into a final video following the timeline sequence.

```bash
powerhour concat --project <dir> [--output <path>] [--dry-run]
go run ./cmd/powerhour concat --project <dir> [--output <path>] [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--output <path>` | Output file path (default: `powerhour.<container>` in project dir) |
| `--dry-run` | List segment order without concatenating |

Tries stream copy first for speed. If segments have mismatched codecs, falls back to re-encoding using the resolved encoding defaults (global defaults merged with project overrides).

### `powerhour convert`

Convert a CSV/TSV plan file to YAML format with permissive column detection.

```bash
powerhour convert --project <dir> [--output <path>] [--dry-run]
go run ./cmd/powerhour convert --project <dir> [--output <path>] [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--output <path>` | Output YAML file path |
| `--dry-run` | Preview detected columns without writing |

Auto-detects delimiters, header presence, and column roles (link, start_time, duration) using heuristics.

## Validation

### `powerhour validate filenames`

Audit cached source filenames against the active template, renaming cached files that no longer match.

```bash
powerhour validate filenames --project <dir> [--index <n>] [--json]
go run ./cmd/powerhour validate filenames --project <dir> [--index <n>] [--json]
```

### `powerhour validate segments`

Reconcile rendered segment filenames/logs with the configured template, renaming legacy outputs when possible.

```bash
powerhour validate segments --project <dir> [--index <n>] [--json]
go run ./cmd/powerhour validate segments --project <dir> [--index <n>] [--json]
```

## Cache Management

### `powerhour migrate`

Move project-local cache files into the global cache (`~/.powerhour/cache/`).

```bash
powerhour migrate --project <dir> [--dry-run]
go run ./cmd/powerhour migrate --project <dir> [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Print what would be moved without moving files |

Files are moved (not copied) and the global index is updated. Entries already present in the global cache with a live file are skipped. After migration, the project will use the global cache automatically.

## Tool Management

### `powerhour tools list`

Report resolved tool versions and locations.

```bash
powerhour tools list [--json]
go run ./cmd/powerhour tools list [--json]
```

### `powerhour tools install`

Install or update managed tools in the local cache.

```bash
powerhour tools install [tool|all] [--version <v>] [--force] [--json]
go run ./cmd/powerhour tools install [tool|all] [--version <v>] [--force] [--json]
```

### `powerhour tools encoding`

Interactively configure global encoding defaults via a TUI carousel.

```bash
powerhour tools encoding
go run ./cmd/powerhour tools encoding
```

Probes available hardware encoders (VideoToolbox, NVENC, AMF) and software encoders across codec families (H.264, HEVC, VP9, AV1) on each invocation. The carousel covers 12 settings:

| Setting | Options |
|---------|---------|
| Video codec | Probed hardware + software encoders |
| Resolution | 1280×720, 1920×1080, 3840×2160 |
| FPS | 24, 30, 60 |
| CRF | 18, 20, 23, 28 |
| Preset | fast, medium, slow |
| Video bitrate | 4M, 8M, 16M, 24M |
| Container | mp4, mkv, mov |
| Audio codec | aac, libopus |
| Audio bitrate | 128k, 192k, 256k, 320k |
| Sample rate | 44100, 48000 |
| Channels | 1 (mono), 2 (stereo) |
| Loudnorm | enabled, disabled |

Defaults are saved to `~/.powerhour/encoding.yaml` and apply globally. Per-project overrides can be set in the `encoding:` block of `powerhour.yaml`.

In non-TTY environments, the command probes and auto-saves best defaults without the interactive carousel.
