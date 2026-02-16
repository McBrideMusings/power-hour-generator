# Development Setup

## Requirements

- **Go 1.21+** (project uses Go 1.24.2)
- macOS, Windows, or Linux
- Git

External tools (`yt-dlp`, `ffmpeg`, `ffprobe`) are auto-managed by the CLI but useful to have locally for debugging.

## Install & Run

```bash
# Clone the repository
git clone https://github.com/McBrideMusings/power-hour-generator.git
cd power-hour-generator

# Build the CLI binary
go build -o powerhour ./cmd/powerhour

# Or run directly without building
go run ./cmd/powerhour
```

## Commands

| Category | Command | Description |
|----------|---------|-------------|
| **Build** | `go build ./...` | Compile all packages |
| **Build** | `go build -o powerhour ./cmd/powerhour` | Build CLI binary |
| **Test** | `go test ./...` | Run all tests |
| **Test** | `go test ./internal/render/...` | Run a single package's tests |
| **Lint** | `go vet ./...` | Static analysis |
| **Format** | `gofmt -w $(find cmd internal pkg -name '*.go')` | Format all Go files |
| **Docs** | `cd docs && npm run docs:dev` | Start docs dev server (port 5193) |
| **Docs** | `cd docs && npm run docs:build` | Build docs for production |

## Project Structure

```
power-hour-generator/
  cmd/powerhour/              # CLI entry point
    main.go
  internal/
    cli/                      # Cobra commands
    config/                   # YAML config parsing
    cache/                    # Source media caching
    render/                   # FFmpeg filter graphs + execution
    project/                  # Config + CSV resolution
    paths/                    # Project directory layout
    tools/                    # External tool management
    logx/                     # File logging
  pkg/
    csvplan/                  # CSV/TSV loading
  docs/                       # VitePress documentation
  .claude/                    # Claude Code configuration
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `gopkg.in/yaml.v3` | YAML config parsing |

## Smoke Testing

```bash
# Initialize a sample project
go run ./cmd/powerhour init --project sample_project

# Verify config and tools
go run ./cmd/powerhour check --project sample_project --strict

# View parsed plan
go run ./cmd/powerhour status --project sample_project --json

# List tool versions
go run ./cmd/powerhour tools list --json
```
