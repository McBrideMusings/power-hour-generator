# Tool Management

The `internal/tools/` package handles detection, installation, and version management of external tools (`yt-dlp`, `ffmpeg`, `ffprobe`).

## Cache Location

Tools are cached per-user:

| OS | Path |
|----|------|
| macOS | `~/Library/Application Support/PowerHour/bin/` |
| Linux | `~/.local/share/powerhour/bin/` |
| Windows | `%LOCALAPPDATA%\PowerHour\bin\` |

Override with `POWERHOUR_TOOLS_DIR` environment variable.

## Manifest

`manifest.json` (in the cache root) records installed tool metadata:

- Tool name, version, source (cache or system)
- Binary paths
- SHA-256 checksum of the main binary
- Installation timestamp

## Detection (`Detect`)

`Detect()` iterates all known tools and determines their status:

1. **Manifest validation** — checks if recorded paths still exist on disk
2. **Checksum trust** — if the binary's SHA-256 matches the manifest, trusts the recorded version without running `--version` (avoids a 6-7s shell-out for `yt-dlp` on some versions)
3. **Version fallback** — if checksum doesn't match, runs the version command
4. **Cache scan** — looks for cached binaries if manifest is invalid
5. **System PATH** — falls back to system-installed tools

### Minimum Version Resolution

When a config specifies `minimum_version: latest`, `resolveMinimumVersion()` calls the GitHub API to resolve the latest release tag. This result is cached in `release_cache.json` with a 1-hour TTL to avoid hitting GitHub on every run.

## Installation (`EnsureAll`)

`EnsureAll()` is the preferred entry point. It:

1. Calls `Detect()` **once** for all tools (not per-tool)
2. Checks which tools are satisfied
3. Installs only what's missing, passing `SkipInitialCheck` to avoid redundant Detect calls inside `Install()`
4. Accepts a `StatusFunc` callback for per-tool progress reporting

```go
toolStatuses, err := tools.EnsureAll(ctx, []string{"yt-dlp", "ffmpeg"}, statusFn)
```

Avoid using per-tool `Ensure()` from CLI code — it calls `Detect()` for every tool, resulting in redundant work.

### Version-Qualified Downloads

Download filenames include the version (e.g. `yt-dlp_macos.2026.02.04`) so different versions don't shadow each other in the downloads cache. Without this, `ensureDownload` would skip downloading a new version because the old file already exists and no checksum is available to detect the mismatch.

## Lock Files

`acquireInstallLock()` uses exclusive file creation for per-tool locking during installation. Stale locks (older than 10 minutes, left by killed processes) are automatically broken.

## Performance Summary

| Optimization | Impact |
|-------------|--------|
| `EnsureAll` (single Detect) | 2x → 1x Detect calls |
| Checksum manifest trust | Skips 6-7s `yt-dlp --version` |
| Release cache (1h TTL) | Skips GitHub API call |
| Version-qualified downloads | Prevents stale binary reuse |
| `SkipInitialCheck` | Eliminates redundant Detect in Install |
| **Net result** | **~20s → ~0.5s** steady-state tool check |
