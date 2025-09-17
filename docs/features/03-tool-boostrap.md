03_tool_bootstrap.md

Objective
- Manage external tools: detect, download, persist absolute paths to per-user cache.

Tasks
- Cache dirs:
  - macOS: ~/Library/Application Support/PowerHour/bin
  - Windows: %LOCALAPPDATA%\PowerHour\bin
  - Linux: ~/.local/share/powerhour/bin
- Implement:
  - Detect()
  - Install(tool, ver)
  - Version(path)
- Commands:
  - powerhour tools install [tool|all]
  - powerhour tools list
- check fails with --strict if missing.

Acceptance
- tools install downloads to cache.
- No PATH reliance; absolute paths stored.