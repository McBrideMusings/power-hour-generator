05_render_segments.md

Objective
- Render each cached source video into a per-clip output file with trim, fades, and text overlays.

Rules
- Input: project-local cache (.powerhour/cache/) + CSV rows.
- Output: .powerhour/segments/{index:03}_{safe-title}.mp4
- Steps per row:
  - ffmpeg trim: -ss {start_time} -t {duration}
  - Video filter chain:
    - scale/pad to config video width/height
    - fps set to config.fps
    - fade in/out video
    - drawtext: title+artist (begin_text), name (end_text), index badge (persistent)
  - Audio:
    - resample to config sample_rate
    - optional loudnorm if enabled
  - Encoding: vcodec/preset/crf, acodec/bitrate from config

Tasks
- internal/render:
  - BuildFilterGraph(row, config) -> string
  - BuildFFmpegCmd(row, cachedPath, outPath, config) -> []string
  - Run per-row jobs with concurrency limit
  - Log stderr for each run into .powerhour/logs/{index}_{title}.log
- Command: powerhour render [--concurrency N]

Acceptance
- Each row generates a valid mp4 in segments/
- Index badge visible full duration
- Title/artist fade in/out at beginning
- Name (if present) fade in/out near end
