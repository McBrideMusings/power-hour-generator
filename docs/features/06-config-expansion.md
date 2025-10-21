06_config_expansion.md

Objective
- Let the YAML config fully drive render defaults (video, audio, clip behaviour, overlays) and expose tooling to inspect/edit the effective configuration.

Configuration model
- Video (global, applies to the combined output)
  - Existing `width`, `height`, `fps`
  - New fields: `codec` (string, default `libx264`), `crf` (int, default `20`, range `0-51`), `preset` (string, default `medium`, allowed x264 presets)
- Audio (global)
  - Existing `acodec`, `bitrate_kbps`
  - New fields: `sample_rate` (int, default `48000`, allowed `44100`/`48000`), `channels` (int, default `2`, allowed `1`/`2`)
  - Keep loudnorm block (`enabled`, `integrated_lufs`, `true_peak_db`, `lra_db`)
- Profiles (reusable styling)
  - `profiles.overlays.<name>` encapsulates
    - `default_style`: font file, font size, colors, outline width, line spacing, optional letter spacing
    - `segments[]`: current overlay schema (`name`, `template`, optional `transform`/`disabled`, `style` overrides, `position`, `timing` with `fade_in_s`/`fade_out_s`)
- Clips block orchestrates clip types and overrides:
  ```yaml
  clips:
    overlay_profile: song-main            # global fallback profile name
    song:
      source:
        plan: powerhour.csv               # primary song list
        default_duration_s: 60
      render:
        fade_in_s: 0.5
        fade_out_s: 0.5
      overlays:
        profile: song-main                # optional but explicit
    interstitial:
      source:
        plan: interstitials.csv
        default_duration_s: 5
      render:
        fade_in_s: 0.3
        fade_out_s: 0.3
      overlays:
        profile: interstitial-drink
    intro:
      source:
        media: intro.mp4                  # static video, no overlays by default
    outro:
      source:
        media: outro.mp4
    overrides:
      - match:
          clip_type: song
          index: 5
        render:
          duration_s: 75
        overlays:
          segments:
            - name: index-badge
              disabled: true
            - name: intro-title
              template: "{title} (extended cut)"
  profiles:
    overlays:
      song-main:
        default_style:
          font_file: "/Users/pierce/Library/Fonts/Oswald.ttf"
          font_size: 42
          font_color: white
          outline_color: black
          outline_width: 2
          line_spacing: 4
        segments:
          - name: intro-title
            template: '{title}'
            ...
      interstitial-drink:
        default_style:
          font_size: 120
          font_color: yellow
          ...
        segments:
          - name: drink-callout
            template: 'DRINK'
            ...
  ```
- Clip type semantics:
  - `song` rows come from `powerhour.csv`, default 60 s duration, 0.5 s fades, uses `song-main` overlay profile.
  - `interstitial` rows load from a separate CSV, default 5 s, uses `interstitial-drink`.
  - `intro`/`outro` (and later intermission) reference external media files; overlays optional.
  - `clips.overrides[]` allow targeted adjustments by clip type/index (future: match by name/id) for duration, fades, disabling segments, tweaking templates, etc.

Tasks
- Update `internal/config` schema to the new structure (video/audio additions, profiles map, clips block with typed substructures).
- Apply defaults: global overlay profile, per-type defaults, merge logic for overrides (profile + per-clip tweaks).
- Update render pipeline:
  - Respect video/audio codec settings when building ffmpeg command.
  - Build drawtext filters from resolved clip-specific overlay profile + overrides.
  - Honour per-clip fade/duration overrides.
- Extend CSV planner / renderer to recognise clip types (song/interstitial/etc.), load multiple plans where configured, and schedule appropriate behaviour.
- Implement CLI commands:
  - `powerhour config show` dumps effective config (merged defaults) in YAML.
  - `powerhour config edit` opens project config via `$EDITOR`.
- Refresh documentation and sample project (`powerhour.yaml`, README) to illustrate the new layout.

Acceptance
- Video renders honour YAML-controlled `codec`, `crf`, `preset`.
- Audio renders honour YAML-controlled `sample_rate`, `channels`, loudnorm settings.
- Clip types (`song`, `interstitial`, `intro`, `outro`) behave according to their config: source selection, default duration, fades, overlay profile.
- Per-clip overrides change only the targeted clip (duration/fades/overlay tweaks) without affecting others.
- `powerhour config show` outputs the resolved configuration; `config edit` opens the editable YAML.
- Existing projects with minimal configs still render using default profiles and clip definitions.
