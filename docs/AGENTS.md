CONTEXT.md

## Goals
- CLI program (`powerhour`) written in Go that orchestrates external `yt-dlp` and `ffmpeg/ffprobe`.
- Input is a CSV file describing clips. Output is discrete, per-clip video files (no concatenation in v1).
- Overlays: bottom-left title+artist at the beginning with fade in/out; optional bottom-left name near the end with fade in/out; persistent index number at bottom-right for entire clip.
- Project model: treat a directory as a project with standardized filenames and a project-local cache of source videos to avoid re-downloading.
- Optional YAML config controls text/font/overlay positions, colors, fade durations, rendering parameters.
- Cross-platform: macOS, Windows, Linux. Single self-contained binary for the CLI. External tools are managed/located/downloaded to per-user cache as needed.

## CSV schema (order = playback/order index)
Columns:
- title (string) – Song/video title.
- artist (string) – Artist.
- start_time (string) – H:MM:SS[.ms] or M:SS[.ms].
- duration (int) – Duration in seconds.
- name (string) – Optional end credit text.
- link (string) – URL or local path to source media.

Example (TSV shown):
title	artist	start_time	duration	name	link
CHAMBEA	BAD BUNNY	1:50	65	pierce	https://youtu.be/gpIBmED4oss

## Project structure
project-root/
  powerhour.csv
  powerhour.yaml   # optional
  .powerhour/
    src/
    segments/
    logs/
    index.json

## Rendering (v1)
- For each row, output .powerhour/segments/{index:03}_{safe-title}.mp4
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
  font_file: ""
  font_size_main: 42
  font_size_index: 36
  color: "white"
  outline_color: "black"
  begin_text:
    template: "{title} — {artist}"
    duration_s: 4.0
    fade_in_s: 0.5
    fade_out_s: 0.5
  end_text:
    template: "{name}"
    offset_from_end_s: 4.0
    duration_s: 4.0
  index_badge:
    template: "{index}"
    persistent: true