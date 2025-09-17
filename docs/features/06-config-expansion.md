06_config_expansion.md

Objective
- Support YAML config overrides for rendering, overlays, fonts, fades, etc.

Tasks
- Extend internal/config schema with:
  - video (width, height, fps, codec, crf, preset, pix_fmt)
  - audio (acodec, bitrate_kbps, sample_rate, channels, loudnorm block)
  - overlays:
    - font_file
    - font_size_main, font_size_index
    - color, outline_color, outline_px
    - box settings
    - begin_text, end_text, index_badge (templates, alignment, margins, fade timings)
- Update render pipeline to apply config values
- Add powerhour config show to dump effective config (YAML)
- Add powerhour config edit to open in editor

Acceptance
- Users can change font, colors, text durations via YAML
- render command respects overrides