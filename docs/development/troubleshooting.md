# Troubleshooting

## Common Issues

### Build cache not writable

**Symptom**: `go test` fails with permission errors in CI or sandboxed environments.

**Fix**: Use a temporary build cache:

```bash
GOCACHE=$(mktemp -d) go test ./...
```

### Missing external tools

**Symptom**: `powerhour check --strict` fails with missing tool errors.

**Fix**: Install managed tools:

```bash
powerhour tools install all
```

Or install `yt-dlp` and `ffmpeg` through your system package manager. The CLI will detect system-installed tools.

### Tool detection is slow

**Symptom**: Fetch or render hangs for 10-20 seconds during "Detecting tools..." with no other output.

**Causes and fixes**:

- **`yt-dlp --version` is slow** — Some yt-dlp versions (notably 2026.02.04+) take 6-7 seconds for `--version`. The tool system uses checksum-based manifest trust to skip this on subsequent runs. If the manifest is missing or the checksum doesn't match, the slow version check runs once.

- **`minimum_version: latest` triggers GitHub API** — This calls `api.github.com` to resolve the latest release tag. The response is cached in `release_cache.json` (1-hour TTL). If GitHub is slow or unreachable, this can block. Check `~/Library/Application Support/PowerHour/bin/release_cache.json` to see if the cache is stale.

- **Redundant Detect calls** — Use `tools.EnsureAll()` (not per-tool `Ensure()`) from CLI code. See [Tool Management architecture](/architecture/tools).

### Tool install hangs indefinitely

**Symptom**: CLI hangs with no output during tool installation, even after ctrl+c and restart.

**Cause**: Stale lock file left by a previously killed process.

**Fix**: Lock files are at `~/Library/Application Support/PowerHour/bin/<tool>.lock`. The system auto-breaks locks older than 10 minutes, but if you're stuck:

```bash
rm ~/Library/Application\ Support/PowerHour/bin/yt-dlp.lock
```

### Fetch errors for local files

**Symptom**: Fetch reports "missing" status for local source paths.

**Note**: This is expected behavior — missing local files show as `missing` in the fetch table rather than as errors. Local files aren't "fetched" from the network, so a missing local file is a warning that the file needs to be provided, not a fetch failure.

**Fix**: Ensure local paths in your CSV are absolute or relative to the project root. Verify the file exists at the specified path.

### Segment template produces unexpected filenames

**Fix**: Use `powerhour validate filenames` to audit and rename cached files, or `powerhour validate segments` for rendered segments:

```bash
powerhour validate filenames --project myproject
powerhour validate segments --project myproject
```

### Config not taking effect

**Fix**: Check the effective configuration to see what defaults are applied:

```bash
powerhour config show --project myproject
```

## Known Test Failures

The following test failures are pre-existing and unrelated to TUI or tools work:

- `TestStatusCommandTableOutput` in `internal/cli/status_test.go`
- `TestStatusCommandJSONOutput` in `internal/cli/status_test.go`

These will be addressed when the status command is updated to work with the collections system.

## Debug Logging

Global debug logs are written to `~/.powerhour/logs/` with timestamped filenames (e.g. `fetch-20260213-143000.log`). These capture:

- Project resolution and config loading
- Tool detection timing
- Fetch/render phase transitions

Use these to diagnose timing issues or unexpected behavior during tool detection and fetch operations.
