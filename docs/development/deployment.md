# Deployment

## Building for Distribution

Build a static binary for the current platform:

```bash
go build -o powerhour ./cmd/powerhour
```

### Cross-Compilation

Go supports cross-compilation to produce binaries for other platforms:

```bash
# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o powerhour-darwin-arm64 ./cmd/powerhour

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o powerhour-darwin-amd64 ./cmd/powerhour

# Linux
GOOS=linux GOARCH=amd64 go build -o powerhour-linux-amd64 ./cmd/powerhour

# Windows
GOOS=windows GOARCH=amd64 go build -o powerhour-windows-amd64.exe ./cmd/powerhour
```

## CI/CD

CI/CD pipeline and release packaging are still being defined. Cross-compilation and automated release builds will produce static binaries per platform.

## External Tool Distribution

The CLI auto-manages `yt-dlp` and `ffmpeg` — users don't need to install these separately. Tool binaries are cached per-user:

| Platform | Cache path |
|----------|-----------|
| macOS | `~/Library/Application Support/PowerHour/bin` |
| Linux | `~/.local/share/powerhour/bin` |
| Windows | `%LOCALAPPDATA%\PowerHour\bin` |

Override with `POWERHOUR_TOOLS_DIR` environment variable.
