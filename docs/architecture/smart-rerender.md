# Smart Re-rendering

The smart re-rendering system in `internal/render/state/` tracks render inputs and detects changes so unchanged segments can be skipped. This avoids redundant FFmpeg work when only a few clips change between runs.

## Overview

The system computes deterministic hashes of all inputs that affect a segment's rendered output. On each render, it compares current hashes against stored state to decide which segments need re-rendering.

## Hash Levels

### Global Config Hash

Computed from video settings, audio settings, and encoding config. If this hash changes, every segment is stale and must re-render.

Inputs: `VideoConfig` (width, height, fps, codec, crf, preset), `AudioConfig` (codec, bitrate, sample rate, channels, loudnorm params), `EncodingConfig` overrides.

### Segment Input Hash

Computed per-segment from:

- **CSV row fields** — link (identifier only, not file content), start_time, duration, title, artist, name, custom fields (sorted by key)
- **Resolved overlay profile** — template strings, text styles, positions, timing, fade durations
- **Clip metadata** — fade in/out durations, filename template

Both hashes use canonical JSON serialization (sorted keys) passed through SHA256, producing `"sha256:<hex>"` strings.

## State Storage

Render state persists in `.powerhour/render-state.json`:

```json
{
  "global_config_hash": "sha256:def456...",
  "segments": {
    "segments/songs/001_bohemian_rhapsody.mp4": {
      "input_hash": "sha256:abc123...",
      "rendered_at": "2026-02-11T12:00:00Z",
      "source_path": "cache/abc123.webm",
      "duration_s": 60
    }
  }
}
```

Segment keys are output paths relative to the project root for portability. Missing or corrupt state files are treated as empty state (everything renders). Writes use atomic temp-file-and-rename.

## Change Detection

The detection flow for each render invocation:

1. If `--force` is set, all segments render (reason: "forced")
2. Compute current global config hash — if it differs from stored, all segments render (reason: "config changed")
3. Per segment: compute input hash and compare to stored state
   - No prior entry → render (reason: "new segment")
   - Hash differs → render (reason: "input changed")
   - Output file missing from disk → render (reason: "output missing")
   - Otherwise → skip (reason: "up to date")
4. After rendering, prune state entries for segments no longer in the plan (handles removed rows)

## Render Integration

The render service loads state before processing, runs change detection, renders only stale segments, updates state entries after each successful render, and saves state when complete. Progress reporting shows rendered/skipped/failed counts.

## Dry-Run Mode

The `--dry-run` flag runs change detection without executing FFmpeg. Output shows each segment's action and reason:

```
DRY RUN: 3 segments would be rendered, 57 would be skipped

  RENDER  003  teenagers          (start_time changed)
  RENDER  015  chambea            (new segment)
  SKIP    001  bohemian_rhapsody  (up to date)
```

With `--json`, the actions are emitted as a JSON array.

## Package Structure

```
internal/render/state/
├── hash.go      — GlobalConfigHash(), SegmentInputHash()
├── store.go     — RenderState, SegmentState, Load(), Save()
└── detect.go    — DetectChanges() → []SegmentAction
```
