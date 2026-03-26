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

## Built-in Presets

Collections can use built-in overlay presets instead of custom profiles. Specify them in your collection config:

```yaml
collections:
  songs:
    overlays:
      - type: song-info
  interstitials:
    overlays:
      - type: drink
```

### `song-info` Preset

Renders title, artist, an optional credit line, and a persistent index badge (two-layer: thick outline + white fill).

**Default font**: Oswald if installed, otherwise Futura (macOS built-in). Font patterns are resolved to file paths via `fc-match` to guarantee correct weight selection. Each element supports independent font overrides via `title_font`, `artist_font`, and `number_font` options. A legacy `font` option overrides all three.

| Element | Font Weight | Size | Position | Timing |
|---------|-------------|------|----------|--------|
| Title | Bold | 64px | Bottom-left, above artist | First 4s, 0.5s fade |
| Artist (ALL CAPS) | Regular | 32px | Bottom-left, bottom-aligned with number | First 4s, 0.5s fade |
| Credit: {name} | Regular | 32px | Bottom-left, bottom-aligned with number | Last 4s, 0.5s fade |
| Number badge | Bold | 140px | Bottom-right, two-layer (outline + fill) | Persistent |

The credit line only appears when the `name` field is present in the CSV/YAML plan. All elements share a `bottom_margin` (default 40px) for vertical alignment.

**Configurable options:**

| Option | Default |
|--------|---------|
| `title_font` | `Oswald:Bold` or `Futura:Bold` |
| `artist_font` | `Oswald` or `Futura` |
| `number_font` | `Oswald:Bold` or `Futura:Bold` |
| `color` | `white` |
| `outline_color` | `black` |
| `outline_width` | `2` |
| `title_size` | `64` |
| `artist_size` | `32` |
| `number_size` | `140` |
| `number_outline_width` | `8` |
| `show_number` | `true` |
| `info_duration` | `4.0` |
| `fade_duration` | `0.5` |
| `bottom_margin` | `40` |
| `credit_prefix` | `Credit:` |
| `credit_size` | same as `artist_size` |
| `credit_duration` | same as `info_duration` |

### `drink` Preset

Centered "Drink!" text with a shadow effect, persistent for the full clip.

| Option | Default |
|--------|---------|
| `font` | auto-detected (Bold) |
| `text` | `Drink!` |
| `color` | `white` |
| `outline_color` | `black` |
| `outline_width` | `4` |
| `shadow_color` | `yellow` |
| `shadow_offset_x` | `3` |
| `shadow_offset_y` | `3` |
| `size` | `120` |

## Previewing Overlays

Use the `sample` command to extract a single frame and inspect overlays without rendering the full clip:

```bash
# Preview a specific clip
powerhour sample 2s --collection songs --index 1

# What's at the 10-minute mark of the full power hour?
powerhour sample 10m

# Custom output path
powerhour sample 2s --collection songs --index 1 --output preview.png
```

The time argument accepts Go durations (`500ms`, `2s`), timecodes (`0:30`), or raw seconds (`0.5`). Without `--index`, the time is treated as an absolute position in the concatenated timeline.

## Custom Profiles

For full control, define custom profiles under `profiles.overlays`:

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
            offset_y: 80
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
            offset_y: 40
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
