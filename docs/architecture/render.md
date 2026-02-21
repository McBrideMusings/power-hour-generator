# Render Pipeline

The render system in `internal/render/` builds FFmpeg commands with filter graphs and executes them in parallel.

## Components

### Filter Graph (`filters.go`)

Constructs the FFmpeg filter chain for each clip:

1. **Scale/pad** — fit source to target resolution (e.g., 1920x1080)
2. **FPS** — normalize framerate
3. **Fade** — video fade in/out
4. **Drawtext** — overlay text segments (title, artist, index badge, etc.)
5. **Loudnorm** — EBU R128 audio normalization

Each overlay segment from the resolved profile becomes a `drawtext` filter with computed position, timing, and style expressions.

### Templates (`templates.go`)

Handles `$TOKEN`-based filename expansion for segment output paths. Tokens are replaced with sanitized values from the clip metadata; empty tokens are omitted and repeated separators are collapsed.

### Service (`service.go`)

Orchestrates the render pipeline:

1. Resolve clips from project config
2. Build FFmpeg command for each clip
3. Run workers in parallel (configurable concurrency)
4. Track progress and report results
5. Log FFmpeg stderr to per-clip log files

## FFmpeg Command Structure

Each render invocation roughly follows:

```
ffmpeg -ss <start> -t <duration> -i <source>
  -vf "scale=...,pad=...,fps=...,fade=...,drawtext=...,drawtext=..."
  -af "aresample=...,loudnorm=..."
  -c:v libx264 -crf 20 -preset medium
  -c:a aac -b:a 192k
  -y <output.mp4>
```

## Parallel Execution

The service runs multiple FFmpeg processes concurrently, limited by `--concurrency N`. Each worker:

- Picks the next unprocessed clip
- Builds the FFmpeg command
- Executes and captures stderr to `logs/`
- Reports success or failure

## Concatenation (`concat.go`)

After segments are rendered, `concat.go` handles assembling them into a final output video:

1. **Timeline resolution** — walks the timeline config to determine segment order, respecting interleave rules. Interleave clips cycle when exhausted (e.g., a single interstitial repeats between every song). Falls back to sorted glob of `*.mp4` when no timeline is configured.
2. **Concat list** — writes an ffmpeg concat demuxer file, verifying each segment exists.
3. **Execution** — tries stream copy first (fast, lossless when all segments share codecs). If stream copy fails, falls back to re-encoding using the resolved encoding settings (video codec, bitrate, audio codec, sample rate, channels, preset).

The re-encode path passes `-ar` (sample rate) and `-ac` (channels) alongside codec and bitrate flags to ffmpeg.

## Test Helpers

`test_helpers_test.go` provides shared utilities for render package tests, supporting the table-driven test pattern used throughout the codebase.
