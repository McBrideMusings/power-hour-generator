---
outline: deep
---

# Overlays

Overlays are text elements burned into rendered segments — title cards, artist credits, index badges, and custom text. They're configured through **profiles**, which define reusable collections of overlay segments.

## Profile Structure

Profiles live under `profiles.overlays` in your YAML config:

```yaml
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
          # ...
```

Each profile has:
- **default_style** — inherited by all segments unless overridden
- **segments** — the individual text overlays

## Segments

Each segment defines one text overlay with its own template, style, position, and timing:

```yaml
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
```

### Templates

Overlay text uses `{token}` brace syntax (case-insensitive). Standard tokens:

| Token | Description |
|-------|-------------|
| `{title}` | Song/video title from CSV |
| `{artist}` | Artist name |
| `{name}` | End-credit text |
| `{index}` | 1-based row number |

Any CSV column automatically becomes available as a token. See [Templates](/guide/templates) for details.

### Transform

Apply text transformations without modifying the source CSV:

```yaml
- name: intro-artist
  template: '{artist}'
  transform: uppercase
```

Accepts `uppercase` or `lowercase`.

### Style Properties

Each segment inherits from `default_style` and can override:

| Property | Description |
|----------|-------------|
| `font_file` | Path to TrueType/OpenType font (empty = ffmpeg default) |
| `font_size` | Font size in pixels |
| `font_color` | Text color |
| `outline_color` | Outline/stroke color |
| `outline_width` | Outline thickness |
| `line_spacing` | Space between lines |

Font paths: on macOS, point at fonts in `/System/Library/Fonts`, `/Library/Fonts`, or `~/Library/Fonts`. Similar platform-specific paths work on other OSes.

### Position

Position helpers compute `drawtext` expressions:

```yaml
position:
  origin: bottom-left    # anchor point
  offset_x: 40           # pixels from edge
  offset_y: 220          # pixels from edge
```

Available origins: `top-left`, `top-right`, `bottom-left`, `bottom-right`, `center`.

For advanced layouts, provide explicit `x`/`y` expressions instead.

### Timing Anchors

Each segment supports independent timing:

| Anchor | Description |
|--------|-------------|
| `from_start` | Offset from clip start |
| `from_end` | Offset from clip end |
| `absolute` | Fixed timestamp within the clip |
| `persistent` | Visible for the entire clip duration |

```yaml
timing:
  start:
    type: from_start
    offset_s: 0
  end:
    type: from_start
    offset_s: 4
  fade_in_s: 0.5
  fade_out_s: 0.5
```

## Common Profile Example

A typical power hour profile with title, artist, and persistent index badge:

```yaml
profiles:
  overlays:
    song-main:
      default_style:
        font_size: 42
        font_color: white
        outline_color: black
        outline_width: 2
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
            start: { type: from_start, offset_s: 0 }
            end: { type: from_start, offset_s: 4 }
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
            start: { type: from_start, offset_s: 0 }
            end: { type: from_start, offset_s: 4 }
            fade_in_s: 0.5
            fade_out_s: 0.5
        - name: index-badge
          template: '{index}'
          style:
            font_size: 140
          position:
            origin: bottom-right
            offset_x: 40
            offset_y: 40
          timing:
            start: { type: from_start, offset_s: 0 }
            end: { type: persistent }
```
