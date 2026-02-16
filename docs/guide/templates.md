---
outline: deep
---

# Templates

Power Hour Generator uses two template systems: `$TOKEN` placeholders for filenames and `{token}` brace syntax for overlay text.

## Segment Filename Tokens

Control rendered segment filenames via `outputs.segment_template`:

```yaml
outputs:
  segment_template: "$INDEX_PAD3_$SAFE_TITLE"
```

The default produces names like `001_teenagers.mp4`.

### Available Tokens

| Token | Description |
|-------|-------------|
| <code v-pre>$INDEX_PAD2</code>, <code v-pre>$INDEX_PAD3</code>, <code v-pre>$INDEX_PAD4</code> | Zero-padded plan index (width 2/3/4) |
| <code v-pre>$INDEX</code>, <code v-pre>$INDEX_RAW</code>, <code v-pre>$ROW_ID</code> | Plan index without padding |
| <code v-pre>$TITLE</code>, <code v-pre>$ARTIST</code>, <code v-pre>$NAME</code>, <code v-pre>$START</code>, <code v-pre>$DURATION</code> | Sanitized values from CSV |
| <code v-pre>$SAFE_TITLE</code>, <code v-pre>$SAFE_ARTIST</code>, <code v-pre>$SAFE_NAME</code> | Lowercased slug variants (hyphen separated) |
| <code v-pre>$ID</code>, <code v-pre>$SAFE_ID</code> | Cache identifier from the resolved source |
| <code v-pre>$SOURCE_BASENAME</code>, <code v-pre>$SAFE_SOURCE_BASENAME</code> | Base name of the cached source file |

Use `$$` to emit a literal dollar sign. When a token resolves to an empty string it's omitted; repeated separators are collapsed.

**Example**: <code v-pre>segment_template: "$ID_$INDEX_$TITLE_$NAME"</code> produces names like `0J3vgcE5i2o_028_Chic_C_est_La_Vie_Madison.mp4`.

## Download Filename Tokens

Control cached source filenames via `downloads.filename_template`:

```yaml
downloads:
  filename_template: "$INDEX_$ID"
```

### Available Tokens

| Token | Description |
|-------|-------------|
| <code v-pre>$ID</code> | Remote: yt-dlp media ID; Local: sanitized source basename |
| <code v-pre>$INDEX</code> / <code v-pre>$INDEX_PAD3</code> | Zero-padded plan index |
| <code v-pre>$INDEX_RAW</code> / <code v-pre>$ROW_ID</code> | Unpadded plan index |
| <code v-pre>$HASH</code> / <code v-pre>$HASH10</code> / <code v-pre>$KEY</code> / <code v-pre>$KEY10</code> | SHA-256 hash of source identifier (full or first 10 chars) |
| <code v-pre>$TITLE</code>, <code v-pre>$ARTIST</code>, <code v-pre>$NAME</code>, <code v-pre>$START</code>, <code v-pre>$DURATION</code> | Sanitized CSV values |
| <code v-pre>$SOURCE_HOST</code>, <code v-pre>$SOURCE_ID</code> | Source URL hostname and identifier |

Run `powerhour validate filenames --project <dir>` to audit and rename existing cache files to the current template.

## Overlay Text Tokens

Overlay templates use `{token}` brace syntax (case-insensitive):

```yaml
segments:
  - name: intro-title
    template: '{title}'
  - name: intro-artist
    template: '{artist}'
  - name: index-badge
    template: '{index}'
```

### Standard Tokens

| Token | Description |
|-------|-------------|
| `{title}` | Song/video title |
| `{artist}` | Artist name |
| `{name}` | End-credit text |
| `{index}` | 1-based row number |

### Dynamic Tokens

Any CSV column automatically becomes available in both template systems. With [collections](/guide/collections), custom headers are mapped to tokens:

```csv
link,timestamp,length,song_name,performer,dedication
```

This creates tokens: `{song_name}`, `{performer}`, `{dedication}` for overlays, and <code v-pre>$SONG_NAME</code>, <code v-pre>$PERFORMER</code>, <code v-pre>$DEDICATION</code> for filenames.
