03_tool_bootstrap.md

Objective
- Guarantee `powerhour` can run required external binaries on macOS, Windows, and Linux without manual setup.
- Provide a single entry point for detecting, installing, and upgrading `ffmpeg`/`ffprobe` and `yt-dlp`.

Managed tools
- ffmpeg (bundled ffprobe) — require >= 6.0, record both binaries together.
- yt-dlp — prefer official GitHub release artefacts.
- Future tools reuse the same manifest and commands.

Rules
- Cache roots:
  - macOS: ~/Library/Application Support/PowerHour/bin
  - Windows: %LOCALAPPDATA%\PowerHour\bin
  - Linux: ~/.local/share/powerhour/bin
  - Respect `POWERHOUR_TOOLS_DIR` override when set.
- Layout: {cache}/{tool}/{version}/{binary}. Keep archives under {cache}/downloads/ before unpack.
- Persist manifest {cache}/manifest.json capturing tool, version, source (`cache|system`), path, checksum, installed_at.
- `Detect()` order: manifest → cache folders → PATH lookup. Return structured status per tool.
- When using PATH binary, still store absolute path + version in manifest with `source=system`.
- `Install(tool, version)` downloads the release artefact, verifies checksum, unpacks, chmod +x (non-Windows), writes manifest. Idempotent unless `--force`. Until release metadata is wired up, copying from PATH into the cache is an acceptable fallback.
- Download URLs and checksums live in a table keyed by OS/arch; support pinned versions from config (`tools.ffmpeg.version`, etc.).
- Guard installs with a file lock to avoid concurrent extraction races.
- Never mutate PATH; downstream code consumes absolute paths from manifest.

Tasks
- internal/tools package:
  - `Detect(ctx) ([]Status, error)` returning manifest-aware statuses and calling `Version(path)` for validation.
  - `Install(ctx, tool, version, opts)` handling download, extraction, checksum, manifest updates.
  - `Ensure(ctx, tool)` ensures minimum version, optionally triggering install.
  - `Version(path)` runs the binary (`--version` or `-version`) and parses semantic version.
  - Manifest reader/writer utilities with optimistic locking.
- internal/cli commands:
  - `powerhour tools list [--json]` prints table with tool, version, source, path, notes.
  - `powerhour tools install [tool|all] [--version X] [--force]` ensures selected tool(s); surfaces progress meter.
  - `powerhour check` consumes `internal/tools.Detect`; with `--strict` exit non-zero if any required tool missing/incompatible.
- Integrations:
  - `internal/config` exposes tool version pins.
  - `internal/logx` captures download/extraction logs under .powerhour/logs/.
  - Downstream packages request tool paths via `internal/tools.Lookup("ffmpeg")`.

Acceptance
- Fresh machine: `powerhour tools install all` downloads binaries into cache, manifest lists absolute paths, subsequent runs reuse them.
- Removing a cached binary causes next `Detect` to mark it missing and `Install` restores it automatically.
- `powerhour tools list` reports accurate versions and sources (cache vs system) on all platforms.
- `powerhour check --strict` fails when any required tool is absent or too old, and passes once tools are installed.
- Rendering/fetch commands invoke binaries via manifest paths; execution succeeds even when PATH lacks ffmpeg/yt-dlp.
