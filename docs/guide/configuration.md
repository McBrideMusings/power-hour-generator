---
outline: deep
---

# Configuration

The optional `powerhour.yaml` file controls rendering defaults, overlay profiles, and project behavior. All fields are optional — missing values fall back to built-in defaults.

## Video Settings

```yaml
video:
  width: 1920
  height: 1080
  fps: 30
  codec: libx264
  crf: 20
  preset: medium
```

## Audio Settings

```yaml
audio:
  acodec: aac
  bitrate_kbps: 192
  sample_rate: 48000
  channels: 2
  loudnorm:
    enabled: true
    integrated_lufs: -14
    true_peak_db: -1.5
    lra_db: 11
```

The `loudnorm` section enables EBU R128-style loudness normalization. Adjust targets to match your delivery specs or set `enabled: false` to disable.

## File Settings

```yaml
files:
  plan: powerhour.csv
  cookies: cookies.txt
```

Point at a different CSV/TSV plan or supply a cookies text file for `yt-dlp` during fetches.

## Plan Settings

```yaml
plan:
  default_duration_s: 60
  headers:
    duration: ["length"]
    start_time: ["start"]
```

`default_duration_s` supplies a project-wide fallback (default 60 seconds) when the `duration` column is absent or empty. Per-row values override it.

`headers` maps canonical field names to alternate column names. Each field can list multiple acceptable header strings.

## Clips Settings

```yaml
clips:
  overlay_profile: song-main
  song:
    source:
      plan: powerhour.csv
      default_duration_s: 60
    render:
      fade_in_s: 0.5
      fade_out_s: 0.5
    overlays:
      profile: song-main
```

## Output Templates

```yaml
outputs:
  segment_template: "$INDEX_PAD3_$SAFE_TITLE"
```

See [Templates](/guide/templates) for the full list of available tokens.

## Download Templates

```yaml
downloads:
  filename_template: "$INDEX_$ID"
```

Control how cached source files are named. See [Templates](/guide/templates) for available tokens.

## Tool Requirements

```yaml
tools:
  yt-dlp:
    minimum_version: latest
    proxy: socks5://127.0.0.1:9050
```

Set explicit tool version requirements. Use a concrete version string or `latest`. Supply a `proxy` value when `yt-dlp` should route through a specific network proxy.

## Overlay Profiles

See [Overlays](/guide/overlays) for profile configuration.

## Collections

See [Collections](/guide/collections) for multi-CSV project setup.

## Full Example

```yaml
version: 1
video:
  width: 1920
  height: 1080
  fps: 30
  codec: libx264
  crf: 20
  preset: medium
audio:
  acodec: aac
  bitrate_kbps: 192
  sample_rate: 48000
  channels: 2
  loudnorm:
    enabled: true
    integrated_lufs: -14
    true_peak_db: -1.5
    lra_db: 11
outputs:
  segment_template: "$INDEX_PAD3_$SAFE_TITLE"
profiles:
  overlays:
    song-main:
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
clips:
  overlay_profile: song-main
  song:
    source:
      plan: powerhour.csv
      default_duration_s: 60
    render:
      fade_in_s: 0.5
      fade_out_s: 0.5
    overlays:
      profile: song-main
files:
  plan: powerhour.csv
  cookies: cookies.txt
downloads:
  filename_template: "$INDEX_$ID"
plan:
  default_duration_s: 60
tools:
  yt-dlp:
    minimum_version: latest
    proxy: socks5://127.0.0.1:9050
```
