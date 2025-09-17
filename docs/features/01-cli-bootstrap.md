01_bootstrap_cli.md

Objective
- Scaffold Go CLI with cobra.
- Implement project detection, config loading, logging, and check command skeleton.

Tasks
- Initialize Go module powerhour.
- Add spf13/cobra.
- Packages:
  - internal/paths: resolve project root (--project).
  - internal/config: load powerhour.yaml if present; apply defaults.
  - internal/logx: file logger; .powerhour/logs/.
  - internal/tools: probe yt-dlp, ffmpeg, ffprobe.
- Commands:
  - powerhour init → create .powerhour/, empty powerhour.csv, default powerhour.yaml.
  - powerhour check → print resolved tool paths/versions.
- Flags: --project DIR, --json.

Acceptance
- init idempotent.
- check prints JSON with tool info.
- Logs in .powerhour/logs.