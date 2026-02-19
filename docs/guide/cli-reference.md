# CLI Reference

All commands accept a `--project <dir>` flag to specify the project directory and `--json` for machine-readable output.

## Project Commands

### `powerhour init`

Create a project directory with starter CSV, default YAML config, and standard directories.

```bash
powerhour init --project <dir>
```

### `powerhour check`

Verify configuration and external tool availability.

```bash
powerhour check --project <dir> [--strict]
```

`--strict` fails on missing or outdated tools, and also validates configuration: profile references, plan file existence, segment template tokens, and orphaned profiles (warnings).

### `powerhour status`

Print the parsed song plan and any validation issues.

```bash
powerhour status --project <dir> [--json]
```

### `powerhour config show`

Print the effective configuration (defaults applied) as YAML.

```bash
powerhour config show --project <dir>
```

### `powerhour config edit`

Open the project configuration in `$EDITOR`, creating a starter file when missing.

```bash
powerhour config edit --project <dir>
```

## Fetch & Render

### `powerhour fetch`

Download or copy source media into the project cache.

```bash
powerhour fetch --project <dir> [flags]
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
```

| Flag | Description |
|------|-------------|
| `--concurrency N` | Limit parallel ffmpeg processes |
| `--force` | Overwrite existing segment files |
| `--no-progress` | Disable interactive progress table |
| `--index <n\|n-m>` | Limit to specific plan rows (repeatable) |
| `--collection <name>` | Target a specific collection |
| `--json` | Structured output |

## Validation

### `powerhour validate filenames`

Audit cached source filenames against the active template, renaming cached files that no longer match.

```bash
powerhour validate filenames --project <dir> [--index <n>] [--json]
```

### `powerhour validate segments`

Reconcile rendered segment filenames/logs with the configured template, renaming legacy outputs when possible.

```bash
powerhour validate segments --project <dir> [--index <n>] [--json]
```

## Cache Management

### `powerhour migrate`

Move project-local cache files into the global cache (`~/.powerhour/cache/`).

```bash
powerhour migrate --project <dir> [--dry-run]
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
```

### `powerhour tools install`

Install or update managed tools in the local cache.

```bash
powerhour tools install [tool|all] [--version <v>] [--force] [--json]
```
