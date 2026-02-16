# Config System

Configuration is handled by `internal/config/config.go`, which parses `powerhour.yaml` into strongly-typed Go structs with defaults.

## Structure

The config is organized into sections:

- **Video** — resolution, codec, quality settings
- **Audio** — codec, bitrate, sample rate, loudnorm parameters
- **Profiles** — reusable overlay profile definitions
- **Clips** — clip type definitions with source, render, and overlay settings
- **Collections** — multi-CSV project configuration
- **Outputs** — segment filename template
- **Downloads** — source cache filename template
- **Files** — plan file path, cookies file
- **Plan** — default duration, header aliases
- **Tools** — version requirements and proxy settings

## Defaults

Every field has a built-in default. When `powerhour.yaml` is absent or a field is omitted, defaults apply. The `config show` command displays the fully resolved configuration.

## Profiles

Overlay profiles under `profiles.overlays` define reusable overlay segment collections. Each profile has a `default_style` that segments inherit from, plus an array of overlay segments with their own style, position, and timing overrides.

Profiles are referenced by name from `clips.overlay_profile`, `clips.<type>.overlays.profile`, or collection `profile` fields.

## Collections vs Legacy Clips

Collections and `clips.song` are mutually exclusive. When `collections` is defined in the config, collection-aware code paths are used. The legacy clips architecture is being removed (see [PH-1](/tickets/PH-1)).

## Validation

Config validation ensures:
- Profile names referenced by clips/collections exist in `profiles.overlays`
- Plan file paths exist on disk
- Segment template tokens are valid
- No conflicting configuration (collections + legacy clips)
