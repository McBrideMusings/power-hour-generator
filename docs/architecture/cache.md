# Cache System

The cache system in `internal/cache/` manages downloading and indexing source media files.

## Global vs Local Cache

By default, media is cached globally at `~/.powerhour/cache/` with an index at `~/.powerhour/index.json`. This enables cross-project deduplication — if two projects reference the same YouTube video, it's only downloaded once.

Set `downloads.global_cache: false` in `powerhour.yaml` to use a project-local cache at `<project>/cache/` with an index at `<project>/.powerhour/index.json`.

The switching happens via `paths.ApplyGlobalCache()`, which swaps `CacheDir` and `IndexFile` on the `ProjectPaths` struct. All downstream code (fetch, render, validate) is cache-location-agnostic — it operates on whichever paths are resolved.

`powerhour migrate` moves files from a project's local cache into the global cache.

## Index

The index file tracks the relationship between plan rows and cached files:

- Source identifier (URL or local path)
- Cached file path
- ffprobe metadata (format, duration, streams)
- Download/copy status

`LoadFromPath()` and `SaveToPath()` provide generic path-based access. `Load(pp)` and `Save(pp, idx)` are convenience wrappers that use `pp.IndexFile`.

## Source Resolution

The cache service resolves sources in two ways:

- **URL sources** — downloaded via `yt-dlp` into the active cache directory
- **Local file sources** — copied (or referenced) into the active cache directory

Source identification uses the link field from the CSV. For URLs, `yt-dlp` extracts a media identifier; for local files, the absolute path serves as the key.

### Local File Missing Handling

When a local file reference doesn't exist on disk, the cache returns `ResolveStatusMissing` instead of a hard error. This is intentional — local files aren't "fetched" so a missing file is a warning, not a fetch failure. The `LocalSourceMissingError` type in `service.go` enables this distinction.

## Service Construction

`NewService()` and `NewServiceWithStatus()` initialize the cache service. The status-aware variant accepts a `tools.StatusFunc` callback so tool detection progress can be reported to the TUI:

```go
svc, err := cache.NewServiceWithStatus(ctx, pp, logger, nil, status.Update)
```

Internally, this calls `tools.EnsureAll()` to verify all required tools in a single pass.

## Runner Abstraction

`runner.go` provides an abstraction over command execution, enabling:

- Real execution of `yt-dlp` and `ffprobe` in production
- Mock command runners in tests

This pattern allows cache tests to run without requiring external tools.

## Testability

The `newCacheService` variable in `fetch.go` is typed as the `NewService` signature for test injection. `newCacheServiceWithStatus` provides the status-callback variant.

## Cache Operations

| Operation | Description |
|-----------|-------------|
| **Fetch** | Download or copy source, update index |
| **Re-probe** | Run ffprobe on existing cached file, update metadata |
| **Match** | Check if source is already cached |
| **Validate** | Audit filenames against the active template |
