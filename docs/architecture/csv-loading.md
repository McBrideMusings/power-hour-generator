# CSV Loading

CSV/TSV plan files are loaded by `pkg/csvplan/`, which provides both standard and collection-specific loading.

## Standard Loader (`loader.go`)

Loads plans with the standard column schema:

| Column | Type | Required | Description |
|--------|------|----------|-------------|
| `title` | string | Yes | Song/video title |
| `artist` | string | Yes | Artist name |
| `start_time` | string | Yes | Trim start (`H:MM:SS[.ms]` or `M:SS[.ms]`) |
| `duration` | int | No | Clip length in seconds (falls back to plan default) |
| `name` | string | No | End-credit text |
| `link` | string | Yes | Media source URL or local file path |

### Auto-Detection

The loader auto-detects CSV vs TSV format based on the delimiter present in the header row.

### Validation

- Errors include row numbers for easy debugging
- Row order is preserved; 1-based indices are assigned
- Start time formats are parsed and validated
- Duration must be a positive integer when present

## Collection Loader (`collection.go`)

Handles collection-specific CSV loading with configurable header mappings. Collections can map custom column names to the required fields:

```yaml
collections:
  songs:
    link_header: video_url
    start_header: timestamp
    duration_header: length
```

### Custom Fields

All CSV columns — standard or custom — are captured in a `CustomFields` map on each row. These fields become available as dynamic template tokens in both filename templates and overlay text.

## Protected Headers

`index` and `id` are reserved and cannot be used as CSV column names. These are auto-generated: `index` is the 1-based row number, `id` is derived from the cache identifier.
