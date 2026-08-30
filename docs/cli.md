# CLI

All commands accept a `--project <dir>` flag to specify the project directory and `--json` for machine-readable output.

## Project Commands

### `powerhour init`

Create a project directory with starter collection plans, default YAML config, and standard directories. YAML plans are the default; pass `--plan-format csv` or `--plan-format tsv` to scaffold delimiter-based plans instead.

```bash
powerhour init --project <dir> [--plan-format yaml|csv|tsv]
go run ./cmd/powerhour init --project <dir> [--plan-format yaml|csv|tsv]
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

### `powerhour config`

Print the effective configuration (defaults applied) as YAML.

```bash
powerhour config --project <dir>
go run ./cmd/powerhour config --project <dir>
```

### `powerhour add`

Add a single URL/path row or append YAML, CSV, or TSV rows into an existing collection. The input can be passed directly as a quoted argument, with `--file`, or piped over stdin, and the destination collection keeps its existing on-disk storage format.

```bash
powerhour add --project <dir> --collection songs "https://youtu.be/example"
powerhour add --project <dir> --collection songs --file rows.csv
cat rows.yaml | go run ./cmd/powerhour add --project <dir> --collection songs
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
| `--json` | Structured output |

Render tracks input hashes in `.powerhour/render-state.json` and automatically skips unchanged segments on subsequent runs. Use `--force` to bypass change detection, or `--dry-run` to preview what would happen.

### `powerhour sample`

Extract a single frame for previewing overlays without rendering full clips.

```bash
powerhour sample <time|overlay-name> [flags]
```

The first argument accepts a timestamp (`500ms`, `2s`, `0:30`) or an overlay name (`title`, `artist`, `credit`, `number`, `drink`) to automatically sample at the midpoint of that overlay's visible window.

**Modes:**

| Flags | Behavior |
|-------|----------|
| `<time>` only | Timeline-absolute: finds which clip is at that position in the full concatenated video |
| `--index N` | Time relative to the Nth clip in timeline order (including interstitials) |
| `--collection <name> --index N` | Time relative to row N in the specified collection |
| `<overlay-name> --index N` | Sample at the midpoint of the named overlay's visible window |

| Flag | Description |
|------|-------------|
| `--index <n>` | Target a specific clip (timeline slot, or collection row if `--collection` is set) |
| `--collection <name>` | Narrow `--index` to a specific collection's rows (requires `--index`) |
| `--output <path>` | Output file path (default: `samples/<segment>_sample_<time>.png`) |

```bash
# What's at the 10-minute mark of the full power hour?
powerhour sample 10m

# Preview song #5 at 2 seconds in
powerhour sample 2s --collection songs --index 5

# Preview the title overlay of song #5 (auto-picks midpoint)
powerhour sample title --collection songs --index 5

# Preview the credit overlay
powerhour sample credit --collection songs --index 5
```

### `powerhour finalize`

Assemble the playback order into the final video.

```bash
powerhour finalize --project <dir> [--output <path>] [--dry-run]
go run ./cmd/powerhour finalize --project <dir> [--output <path>] [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--output <path>` | Output file path (default: `powerhour.<container>` in project dir) |
| `--dry-run` | List segment order without concatenating |

Tries stream copy first for speed. If segments have mismatched codecs, falls back to re-encoding using the resolved encoding defaults (global defaults merged with project overrides).

### `powerhour tui`

Launch the interactive dashboard.

```bash
powerhour tui --project <dir>
go run ./cmd/powerhour tui --project <dir>
```

The dashboard is the primary day-to-day interface for managing projects. It provides a full-screen terminal UI for viewing and editing collections, cache entries, render state, and the timeline. Views are navigated with arrow keys and numbers (1-9 to jump directly). Collections are edited inline, and rows can be reordered, deleted, or modified without leaving the dashboard. The cache view allows filtering and metadata inspection. The timeline view shows the resolved playback order — the sequence of clips finalize will assemble into the final video.

Requires at least one configured collection to launch.

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

### `powerhour validate collection`

Validate a specific collection with detailed row information.

```bash
powerhour validate collection --project <dir> --collection <name>
go run ./cmd/powerhour validate collection --project <dir> --collection <name>
```

| Flag | Description |
|------|-------------|
| `--collection <name>` | Collection name to validate (required) |

### `powerhour doctor`

Check project health.

```bash
powerhour doctor --project <dir> [--json]
go run ./cmd/powerhour doctor --project <dir> [--json]
```

Runs a series of health checks including configuration validity, tool availability, filter support, cache index integrity, and render state consistency. Reports issues with suggested remediation steps. Results can be output as structured JSON for programmatic use.

### `powerhour export`

Export project data as JSON.

```bash
powerhour export --project <dir> [--timeline] [--json]
go run ./cmd/powerhour export --project <dir> [--timeline] [--json]
```

| Flag | Description |
|------|-------------|
| `--timeline` | Include resolved timeline in output |

Exports the project configuration, all collection rows, and optionally the resolved timeline sequence (useful for external tools that build on the power hour workflow).

## Cache Management

### `powerhour cache add`

Register a video into the project cache.

```bash
powerhour cache <file-or-id> [flags]
powerhour cache add <file-or-id> [flags]
```

Both spellings work. The `cache` parent command carries the add handler directly, so the flat form (used in the examples below and in `--help`) and the explicit `cache add` subcommand are equivalent.

| Flag | Description |
|------|-------------|
| `--url <url>` | Source URL (auto-detected from filename if omitted) |
| `--title "..."` | Override title metadata |
| `--artist "..."` | Override artist metadata |
| `--dry-run` | Preview what would happen without making changes |
| `--no-probe` | Skip ffprobe metadata extraction |

The command accepts a local file path, a yt-dlp-style filename like `"Title [HWl1Tu9oZmY].webm"`, or a bare YouTube ID. When a local file is provided, the URL is auto-detected from the file's metadata or can be supplied explicitly with `--url`. Downloads by ID if the argument is not a file on disk.

Examples:

```bash
# Local file, auto-resolves URL from yt-dlp metadata
powerhour cache HWl1Tu9oZmY.webm

# yt-dlp filename format
powerhour cache "Title [HWl1Tu9oZmY].webm"

# Downloads by YouTube ID
powerhour cache HWl1Tu9oZmY

# Explicit URL override
powerhour cache song.webm --url https://youtu.be/example
```

The file is copied (or hardlinked) into the project's active cache directory, probed with ffprobe, and registered in the index with a link mapping from the URL to the canonical identifier.

### `powerhour cache remove`

Remove a cache entry.

```bash
powerhour cache remove <identifier> [flags]
go run ./cmd/powerhour cache remove <identifier> [flags]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without deleting |
| `--keep-file` | Remove index entry but leave cached file on disk |

The identifier can be a YouTube video ID (e.g., `dQw4w9WgXcQ`), a full identifier (e.g., `youtube:dQw4w9WgXcQ`), or a filename or path substring matching the cached file. For URL-backed entries, the cached file is deleted; for local files, only the index entry is removed.

### `powerhour cache doctor`

Inspect and repair cached title/artist metadata.

```bash
powerhour cache doctor [flags]
go run ./cmd/powerhour cache doctor [flags]
```

| Flag | Description |
|------|-------------|
| `--all` | Include cache entries not referenced by the current project |
| `--write` | Apply high-confidence fixes non-interactively |
| `--yes` | Accept all fixes when used with `--write` |
| `--requery` | Re-query yt-dlp metadata for URL-backed entries before normalization |
| `--index <n\|n-m>` | Limit to specific 1-based row index or range (repeatable) |
| `--identifier <id>` | Limit to specific cache identifier(s) (repeatable) |
| `--artist <substring>` | Filter by current or proposed artist substring |

Applies metadata normalization heuristics to detect and fix common issues: "Artist - Title" format splitting, video suffix removal ("Official Video", "HD", etc.), uploader fallback, and track number extraction. Interactive by default; use `--write` for non-interactive batch mode.

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

## Library

The library is a global shared cache of media sources at `~/.powerhour/cache/` (configurable). Multiple projects can import from and contribute to the shared library, reducing redundant downloads and enabling efficient media reuse across power hour projects.

### `powerhour library list`

List all sources in the library.

```bash
powerhour library list [--json]
go run ./cmd/powerhour library list [--json]
```

### `powerhour library search`

Search library sources by identifier, title, or artist.

```bash
powerhour library search <query> [--json]
go run ./cmd/powerhour library search <query> [--json]
```

### `powerhour library info`

Show detailed information about a library source.

```bash
powerhour library info <identifier> [--json]
go run ./cmd/powerhour library info <identifier> [--json]
```

### `powerhour library import`

Import a project's local cache into the library.

```bash
powerhour library import --project <dir> [--dry-run]
go run ./cmd/powerhour library import --project <dir> [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--project <dir>` | Path to the project directory to import from (required) |
| `--dry-run` | Print actions without moving files |

Moves project-local cache files into the global library, updating the global index. Entries already present in the library with a live file are skipped. After import, the project will use the global library automatically for subsequent operations.

### `powerhour library prune`

Remove sources not used recently.

```bash
powerhour library prune [--dry-run] [--older-than <duration>]
go run ./cmd/powerhour library prune [--dry-run] [--older-than <duration>]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | List what would be removed without deleting |
| `--older-than <duration>` | Prune entries not used within this duration (default: `90d`, e.g., `30d`, `6m`, `1y`) |

### `powerhour library verify`

Check integrity of library sources via ffprobe.

```bash
powerhour library verify [--fix] [--json]
go run ./cmd/powerhour library verify [--fix] [--json]
```

| Flag | Description |
|------|-------------|
| `--fix` | Re-download corrupt or missing URL sources |

Verifies that all library files are readable and not corrupted. With `--fix`, attempts to re-download any corrupt or missing URL-backed sources.

## Playback Order

The playback order is the materialized, authoritative list of slots (collection rows and inline `file:` entries) that make up the final concatenated output, stored at `playback-order.yaml` in the project root. Every subcommand reconciles the stored order against the current timeline and pools before acting, and reports what changed (rows dropped, added, or filled) so a stale order is never mutated silently.

### `powerhour order`

List every slot: number, kind, collection, row id, resolved display label, and lock state.

```bash
powerhour order [--json]
go run ./cmd/powerhour order [--json]
```

### `powerhour order swap`

Swap the occupants of two slots.

```bash
powerhour order swap <slotA> <slotB> [--json]
go run ./cmd/powerhour order swap <slotA> <slotB> [--json]
```

Slot numbers are 1-based, matching `powerhour order`'s listing. A slot backed by an inline `file:` entry has no collection or pool — it always holds its position — so swapping it errors. Swapping a locked slot is allowed; locking only excludes a slot from `shuffle`.

### `powerhour order set`

Assign a specific row id to a slot.

```bash
powerhour order set <slot> <row-id> [--json]
go run ./cmd/powerhour order set <slot> <row-id> [--json]
```

The row id must belong to the slot's own collection. A `file:` slot cannot be set, for the same reason it cannot be swapped.

### `powerhour order lock` / `powerhour order unlock`

Lock or unlock a slot so `shuffle` skips (or resumes) touching it.

```bash
powerhour order lock <slot> [--json]
powerhour order unlock <slot> [--json]
go run ./cmd/powerhour order lock <slot> [--json]
go run ./cmd/powerhour order unlock <slot> [--json]
```

### `powerhour order shuffle`

Shuffle playback-order slots.

```bash
powerhour order shuffle [--collection <name>] [--json]
go run ./cmd/powerhour order shuffle [--collection <name>] [--json]
```

| Flag | Description |
|------|-------------|
| `--collection <name>` | Shuffle only this collection's slots; omit to shuffle every collection present in the order |

Scope is global: the shuffled group spans the whole order regardless of which timeline sequence entry produced each slot, so a song can freely cross a `file:` bookend like an intermission clip. Locked slots are excluded. Behavior follows the collection's `selection`: `once` permutes the rows already occupying the group (every row keeps exactly one slot); `repeat` redraws each slot independently from the collection's full pool.

### `powerhour order reconcile`

Reconcile the stored order against the current timeline and pools without otherwise mutating it, reporting and persisting whatever changed.

```bash
powerhour order reconcile [--json]
go run ./cmd/powerhour order reconcile [--json]
```

## Cleanup

### `powerhour clean segments`

Remove all rendered segments and render state.

```bash
powerhour clean segments [--dry-run]
go run ./cmd/powerhour clean segments [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | List what would be removed without deleting |

### `powerhour clean logs`

Remove all log files.

```bash
powerhour clean logs [--dry-run]
go run ./cmd/powerhour clean logs [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | List what would be removed without deleting |

### `powerhour clean orphans`

Remove segment files not in the current plan.

```bash
powerhour clean orphans [--dry-run] [--renumbered]
go run ./cmd/powerhour clean orphans [--dry-run] [--renumbered]
```

A file is an orphan only when nothing live claims it. Two categories that look
like orphans but are not:

- **Inline `file:` segments** under `segments/__inline__/`. They belong to no
  collection, so they are resolved from the playback order instead.
- **Mis-numbered renders.** The segment filename template embeds the clip's
  playback position, so moving a row leaves its already-rendered segment on
  disk under the old number. That file is still the row's clip, trim and
  overlays — only the burned-in number is stale. The TUI previewer plays it
  (the yellow `◐` state) and `render` renames it rather than re-encoding, so
  `clean orphans` reports it as `kept` and leaves it alone.

`--renumbered` removes mis-numbered renders that a newer one has superseded.
The newest render of each row is always kept, with or without the flag.

| Flag | Description |
|------|-------------|
| `--dry-run` | List what would be removed without deleting |
| `--renumbered` | Also remove superseded mis-numbered renders of live rows |

### `powerhour clean all`

Remove segments, logs, render state, and concat artifacts.

```bash
powerhour clean all [--dry-run]
go run ./cmd/powerhour clean all [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | List what would be removed without deleting |
