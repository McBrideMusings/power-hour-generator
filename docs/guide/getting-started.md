# Getting Started

Power Hour Generator (`powerhour`) is a cross-platform CLI, written in Go, that orchestrates `yt-dlp` and `ffmpeg` to produce power hour video clip libraries. It ingests a structured CSV describing each clip, manages project-local caches of source footage, and renders normalized MP4 segments with consistent overlays.

## Requirements

- **Go 1.21+** (for building from source)
- **yt-dlp** and **ffmpeg/ffprobe** (auto-managed by the CLI, or bring your own)
- macOS, Windows, or Linux

## Quick Start

### 1. Build the CLI

```bash
git clone https://github.com/McBrideMusings/power-hour-generator.git
cd power-hour-generator
go build -o powerhour ./cmd/powerhour
```

Or run directly without installing:

```bash
go run ./cmd/powerhour
```

### 2. Create a project

```bash
powerhour init --project my-power-hour
```

This creates the project directory with a starter CSV, default YAML config, and the standard directory layout.

### 3. Edit your CSV plan

Open `my-power-hour/powerhour.csv` and add your clips:

```tsv
title	artist	start_time	duration	name	link
CHAMBEA	BAD BUNNY	1:50	65	pierce	https://youtu.be/gpIBmED4oss
Bohemian Rhapsody	Queen	0:50	60	alex	https://youtu.be/fJ9rUzIMcZQ
```

### 4. Fetch source videos

```bash
powerhour fetch --project my-power-hour
```

### 5. Render segments

```bash
powerhour render --project my-power-hour
```

Each row produces an MP4 in `segments/` with scaling, fades, overlays, and audio normalization applied.

### 6. Configure encoding (optional)

```bash
powerhour tools encoding
```

An interactive carousel lets you pick video codec (with hardware acceleration detection), resolution, FPS, CRF, preset, container format, audio codec, bitrate, sample rate, channels, and loudness normalization. These defaults apply globally and can be overridden per-project in `powerhour.yaml`.

### 7. Concatenate segments

```bash
powerhour concat --project my-power-hour
```

Assembles all rendered segments into a single output video following the timeline sequence. Uses stream copy when possible, falling back to re-encoding with your configured encoding defaults.

## Project Layout

```
my-power-hour/
  powerhour.csv          # Clip plan (CSV or TSV)
  powerhour.yaml         # Optional configuration
  cache/                 # Cached source downloads
  segments/              # Rendered clip outputs
  logs/                  # Per-clip render logs
  .powerhour/
    index.json           # Metadata about processed clips
```

## What's Next

- [CLI](/cli) — all available commands and flags
- [Configuration](/guide/configuration) — customize video, audio, and overlay settings
- [Overlays](/guide/overlays) — configure text overlays with profiles
- [Collections](/guide/collections) — organize multiple clip types
- [Templates](/guide/templates) — control output filenames with tokens
