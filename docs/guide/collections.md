---
outline: deep
---

# Collections

Collections organize multiple types of clips (songs, interstitials, bumpers, outros, etc.) with customizable CSV headers and independent output directories. When `collections` is defined in your config, the tool processes all collections instead of using the legacy `clips.song` configuration.

## Basic Setup

```yaml
segments_base_dir: segments

collections:
  songs:
    plan: powerhour.csv
    output_dir: songs
    profile: song-main

  interstitials:
    plan: bumpers.csv
    output_dir: interstitials
    profile: bumper-overlay
    link_header: video_url
    start_header: timestamp
    duration_header: length
```

## Configuration Fields

Each collection supports:

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `plan` | Yes | — | Path to CSV/TSV file (relative to project root or absolute) |
| `output_dir` | No | collection name | Output directory relative to `segments_base_dir` |
| `profile` | No | — | Overlay profile name; omit to skip overlays |
| `link_header` | No | `"link"` | CSV column name for video link |
| `start_header` | No | `"start_time"` | CSV column name for start time |
| `duration_header` | No | `"duration"` | CSV column name for duration |

## Project Layout with Collections

```
project-root/
  powerhour.csv           # Songs plan
  bumpers.csv             # Interstitials plan
  powerhour.yaml          # Configuration
  cache/                  # Shared cache for all collections
  segments/
    songs/                # Song outputs
    interstitials/        # Interstitial outputs
  logs/
  .powerhour/
    index.json            # Shared metadata
```

All collections share the same `cache/` directory to prevent re-downloading identical videos.

## Dynamic Field Support

All CSV columns automatically become available as template tokens:

- **Segment filenames**: <code v-pre>$COLUMN_NAME</code> and <code v-pre>$SAFE_COLUMN_NAME</code>
- **Overlay templates**: `{column_name}` (case-insensitive)

Example CSV with custom fields:

```csv
link,timestamp,length,song_name,performer,dedication
https://youtu.be/abc123,1:30,60,Chambea,Bad Bunny,For Sarah
```

Use custom fields in your config:

```yaml
outputs:
  segment_template: "$INDEX_PAD3_$SAFE_SONG_NAME_$SAFE_PERFORMER"

profiles:
  overlays:
    song-main:
      segments:
        - name: title
          template: "{song_name}"
        - name: artist
          template: "{performer}"
        - name: dedication
          template: "Dedicated to: {dedication}"
```

## Protected Header Names

These header names are reserved and cannot be used in your CSV:

| Header | Reason |
|--------|--------|
| `index` | Auto-generated 1-based row number |
| `id` | Auto-generated cache identifier |

## CLI Usage

When collections are configured, commands automatically process all collections:

```bash
# Fetch all collections
powerhour fetch --project myproject

# Fetch specific collection only
powerhour fetch --project myproject --collection songs

# Render all collections
powerhour render --project myproject

# Render specific collection
powerhour render --project myproject --collection interstitials

# Filter by index within a collection
powerhour fetch --project myproject --collection songs --index 1-5
```
