CONTEXT.md

> NOTE (AI assistants): keep this document accurate whenever you change behaviour or flags so future agents have up-to-date guidance.

## Goals
- CLI program (`powerhour`) written in Go that orchestrates external `yt-dlp` and `ffmpeg/ffprobe`.
- Input is a CSV file describing clips. Output is discrete, per-clip video files (no concatenation in v1).
- Overlays: defined through a unified segment list. Defaults place the title+artist bottom-left for the first four seconds with fades, optional outro name near the end, and a persistent index badge bottom-right.
- Project model: treat a directory as a project with standardized filenames and a project-local cache of source videos to avoid re-downloading.
- Optional YAML config controls text/font/overlay positions, colors, fade durations, rendering parameters.
- Cross-platform: macOS, Windows, Linux. Single self-contained binary for the CLI. External tools are managed/located/downloaded to per-user cache as needed.

## Guidance for Assistants
- Avoid creating multiple sources of truth: the CSV plan and YAML config are authoritative inputs, and caches should be derived from them rather than storing duplicate state elsewhere.
- Cached source media lives in `cache/`; assume callers keep that directory up to date if they restructure existing projects.
- Interactive progress tables (fetch/render) rely on ANSI escapes. The render table currently emits an initial “pending” snapshot before the live updates; treat the second table as the authoritative state until the UI bug is cleaned up.

## CSV schema (order = playback/order index)
Columns:
- title (string) – Song/video title.
- artist (string) – Artist.
- start_time (string) – H:MM:SS[.ms] or M:SS[.ms].
- duration (int) – Duration in seconds (optional; falls back to the plan default, 60s unless overridden).
- name (string) – Optional end credit text.
- link (string) – URL or local path to source media.

Example (TSV shown):
title	artist	start_time	duration	name	link
CHAMBEA	BAD BUNNY	1:50	65	pierce	https://youtu.be/gpIBmED4oss

## Project structure
project-root/
  powerhour.csv
  powerhour.yaml   # optional
  cache/
  segments/
  logs/
  .powerhour/
    index.json

## Rendering (v1)
- For each row, output segments/{index:03}_{safe-title}.mp4
- ffmpeg: trim, scale/pad, fade in/out, overlays, audio normalized.
- Default encoding: H.264 CRF 20, veryfast, yuv420p, AAC 192k, 48kHz.

## Config (YAML)
version: 1
video:
  width: 1920
  height: 1080
  fps: 30
audio:
  acodec: aac
  bitrate_kbps: 192
overlays:
  default_style:
    font_file: ""
    font_size: 42
    font_color: white
    outline_color: black
    outline_width: 2
    line_spacing: 4
  segments:
    - name: intro-title
      template: '{title}'
      style:
        font_size: 64
      position:
        origin: bottom-left
        offset_x: 40
        offset_y: 220
      timing:
        start:
          type: from_start
          offset_s: 0
        end:
          type: from_start
          offset_s: 4
         fade_in_s: 0.5
         fade_out_s: 0.5
     - name: intro-artist
       template: '{artist}'
       transform: uppercase
       style:
         font_size: 32
       position:
         origin: bottom-left
         offset_x: 40
         offset_y: 160
       timing:
         start:
           type: from_start
           offset_s: 0
         end:
           type: from_start
           offset_s: 4
         fade_in_s: 0.5
         fade_out_s: 0.5
     - name: outro-name
       template: '{name}'
       position:
         origin: bottom-left
         offset_x: 40
         offset_y: 40
       timing:
         start:
           type: from_end
           offset_s: 4
         end:
           type: from_end
           offset_s: 0
     - name: index-badge
       template: '{index}'
       style:
         font_size: 140
       position:
         origin: bottom-right
         offset_x: 40
         offset_y: 40
       timing:
         start:
           type: from_start
           offset_s: 0
         end:
           type: persistent
  # additional segments override defaults per entry

Segments inherit `default_style` unless they set their own fields. Timing supports `from_start`, `from_end`, `absolute`, and `persistent` anchors. `transform` accepts `uppercase` / `lowercase` (default is none).

## CLI notes
- `powerhour fetch --index` accepts single values or ranges (e.g. `5` or `2-4`).
- `powerhour render --index` mirrors fetch, allowing targeted renders without editing the plan.
- Set `GOCACHE=$(mktemp -d)` if tests need an isolated cache in constrained environments.
