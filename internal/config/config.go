package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultOverlayProfileName = "default"

var allowedVideoPresets = map[string]struct{}{
	"ultrafast": {},
	"superfast": {},
	"veryfast":  {},
	"faster":    {},
	"fast":      {},
	"medium":    {},
	"slow":      {},
	"slower":    {},
	"veryslow":  {},
	"placebo":   {},
}

// Config captures the rendering and overlay configuration for a project.
type Config struct {
	Version   int             `yaml:"version"`
	Video     VideoConfig     `yaml:"video"`
	Audio     AudioConfig     `yaml:"audio"`
	Profiles  ProfilesConfig  `yaml:"profiles"`
	Clips     ClipsConfig     `yaml:"clips"`
	Outputs   OutputConfig    `yaml:"outputs"`
	Plan      PlanConfig      `yaml:"plan"`
	Files     FileOverrides   `yaml:"files"`
	Tools     ToolPins        `yaml:"tools"`
	Downloads DownloadsConfig `yaml:"downloads"`
}

// ToolPins captures optional version pinning for managed external tools.
type ToolPins map[string]ToolPin

// ToolPin represents overrides for an individual tool.
type ToolPin struct {
	Version        string `yaml:"version"`
	MinimumVersion string `yaml:"minimum_version"`
	Proxy          string `yaml:"proxy"`
}

// DownloadsConfig controls caching/downloading behaviour.
type DownloadsConfig struct {
	FilenameTemplate string `yaml:"filename_template"`
}

// VideoConfig contains video sizing and framerate information.
type VideoConfig struct {
	Width  int    `yaml:"width"`
	Height int    `yaml:"height"`
	FPS    int    `yaml:"fps"`
	Codec  string `yaml:"codec"`
	CRF    int    `yaml:"crf"`
	Preset string `yaml:"preset"`
}

// AudioConfig describes audio encoding parameters.
type AudioConfig struct {
	ACodec      string         `yaml:"acodec"`
	BitrateKbps int            `yaml:"bitrate_kbps"`
	SampleRate  int            `yaml:"sample_rate"`
	Channels    int            `yaml:"channels"`
	Loudnorm    LoudnormConfig `yaml:"loudnorm"`
}

// ProfilesConfig captures reusable overlay styling definitions.
type ProfilesConfig struct {
	Overlays map[string]OverlayProfile `yaml:"overlays"`
}

// OverlayProfile defines a named overlay style composed of segments and defaults.
type OverlayProfile struct {
	DefaultStyle TextStyle        `yaml:"default_style"`
	Segments     []OverlaySegment `yaml:"segments"`
}

// ClipsConfig orchestrates clip type defaults, profiles, and overrides.
type ClipsConfig struct {
	OverlayProfile string         `yaml:"overlay_profile"`
	Song           ClipTypeConfig `yaml:"song"`
	Interstitial   ClipTypeConfig `yaml:"interstitial"`
	Intro          ClipTypeConfig `yaml:"intro"`
	Outro          ClipTypeConfig `yaml:"outro"`
	Overrides      []ClipOverride `yaml:"overrides"`
}

// ClipTypeConfig encapsulates source/render defaults and overlay selection.
type ClipTypeConfig struct {
	Source   ClipSourceConfig  `yaml:"source"`
	Render   ClipRenderConfig  `yaml:"render"`
	Overlays ClipOverlayConfig `yaml:"overlays"`
}

// ClipSourceConfig describes where clips of a type originate.
type ClipSourceConfig struct {
	Plan               string `yaml:"plan"`
	Media              string `yaml:"media"`
	DefaultDurationSec int    `yaml:"default_duration_s"`
}

// ClipRenderConfig controls fallback render parameters for a clip type.
type ClipRenderConfig struct {
	DurationSec *int     `yaml:"duration_s,omitempty"`
	FadeInSec   *float64 `yaml:"fade_in_s,omitempty"`
	FadeOutSec  *float64 `yaml:"fade_out_s,omitempty"`
}

// ClipOverlayConfig selects which overlay profile applies to the clip type.
type ClipOverlayConfig struct {
	Profile string `yaml:"profile"`
}

// ClipOverride applies targeted adjustments to specific clips.
type ClipOverride struct {
	Match    ClipMatch           `yaml:"match"`
	Render   ClipRenderOverride  `yaml:"render"`
	Overlays ClipOverlayOverride `yaml:"overlays"`
}

// ClipMatch identifies which clip(s) receive an override.
type ClipMatch struct {
	ClipType string `yaml:"clip_type"`
	Index    *int   `yaml:"index,omitempty"`
	Name     string `yaml:"name,omitempty"`
	ID       string `yaml:"id,omitempty"`
}

// ClipRenderOverride modifies render characteristics for a clip.
type ClipRenderOverride struct {
	DurationSec *int     `yaml:"duration_s,omitempty"`
	FadeInSec   *float64 `yaml:"fade_in_s,omitempty"`
	FadeOutSec  *float64 `yaml:"fade_out_s,omitempty"`
}

// ClipOverlayOverride tweaks overlay usage for a clip.
type ClipOverlayOverride struct {
	Profile  string                         `yaml:"profile,omitempty"`
	Segments []ClipOverlaySegmentAdjustment `yaml:"segments,omitempty"`
}

// ClipOverlaySegmentAdjustment customizes a single overlay segment for a clip.
type ClipOverlaySegmentAdjustment struct {
	Name     string     `yaml:"name"`
	Template *string    `yaml:"template,omitempty"`
	Disabled *bool      `yaml:"disabled,omitempty"`
	Style    *TextStyle `yaml:"style,omitempty"`
}

// TextStyle captures font and layout styling options for drawtext overlays.
type TextStyle struct {
	FontFile      string `yaml:"font_file"`
	FontSize      *int   `yaml:"font_size,omitempty"`
	FontColor     string `yaml:"font_color"`
	OutlineColor  string `yaml:"outline_color"`
	OutlineWidth  *int   `yaml:"outline_width,omitempty"`
	LineSpacing   *int   `yaml:"line_spacing,omitempty"`
	LetterSpacing *int   `yaml:"letter_spacing,omitempty"`
}

// OverlaySegment describes a single text overlay instance.
type OverlaySegment struct {
	Name      string       `yaml:"name"`
	Template  string       `yaml:"template"`
	Transform string       `yaml:"transform"`
	Disabled  bool         `yaml:"disabled"`
	Style     TextStyle    `yaml:"style"`
	Position  PositionSpec `yaml:"position"`
	Timing    TimingSpec   `yaml:"timing"`
}

// PositionSpec describes how to place a text overlay on screen.
type PositionSpec struct {
	Origin  string  `yaml:"origin"`
	OffsetX float64 `yaml:"offset_x"`
	OffsetY float64 `yaml:"offset_y"`
	XExpr   string  `yaml:"x"`
	YExpr   string  `yaml:"y"`
}

// TimingSpec controls when an overlay is visible.
type TimingSpec struct {
	Start   TimePointSpec `yaml:"start"`
	End     TimePointSpec `yaml:"end"`
	FadeIn  float64       `yaml:"fade_in_s"`
	FadeOut float64       `yaml:"fade_out_s"`
}

// TimePointSpec defines a timing anchor relative to the clip.
type TimePointSpec struct {
	Type      string  `yaml:"type"`
	OffsetSec float64 `yaml:"offset_s"`
}

// OutputConfig captures naming templates for generated assets.
type OutputConfig struct {
	SegmentTemplate string `yaml:"segment_template"`
}

// LoudnormConfig controls optional EBU R128 loudness normalization.
type LoudnormConfig struct {
	Enabled        *bool    `yaml:"enabled,omitempty"`
	IntegratedLUFS *float64 `yaml:"integrated_lufs,omitempty"`
	TruePeak       *float64 `yaml:"true_peak_db,omitempty"`
	LRA            *float64 `yaml:"lra_db,omitempty"`
}

// EnabledValue returns the effective enabled flag applying defaults.
func (l LoudnormConfig) EnabledValue() bool {
	if l.Enabled == nil {
		return false
	}
	return *l.Enabled
}

// IntegratedLUFSValue returns the configured Integrated LUFS target.
func (l LoudnormConfig) IntegratedLUFSValue() float64 {
	if l.IntegratedLUFS == nil {
		return 0
	}
	return *l.IntegratedLUFS
}

// TruePeakValue returns the configured true peak ceiling in dB.
func (l LoudnormConfig) TruePeakValue() float64 {
	if l.TruePeak == nil {
		return 0
	}
	return *l.TruePeak
}

// LRAValue returns the allowed loudness range.
func (l LoudnormConfig) LRAValue() float64 {
	if l.LRA == nil {
		return 0
	}
	return *l.LRA
}

func (l *LoudnormConfig) applyDefaults(defaults LoudnormConfig) {
	if l == nil {
		return
	}
	if l.Enabled == nil && defaults.Enabled != nil {
		l.Enabled = boolPtr(defaults.EnabledValue())
	}
	if l.IntegratedLUFS == nil && defaults.IntegratedLUFS != nil {
		l.IntegratedLUFS = floatPtr(*defaults.IntegratedLUFS)
	}
	if l.TruePeak == nil && defaults.TruePeak != nil {
		l.TruePeak = floatPtr(*defaults.TruePeak)
	}
	if l.LRA == nil && defaults.LRA != nil {
		l.LRA = floatPtr(*defaults.LRA)
	}
}

// FileOverrides captures optional alternate project file locations.
type FileOverrides struct {
	Plan    string `yaml:"plan"`
	Cookies string `yaml:"cookies"`
}

// PlanConfig captures plan-specific overrides such as alternate headers.
type PlanConfig struct {
	Headers            map[string][]string `yaml:"headers"`
	DefaultDurationSec int                 `yaml:"default_duration_s"`
}

// Default returns the baseline configuration.
func Default() Config {
	defaultStyle := TextStyle{
		FontFile:     "",
		FontSize:     intPtr(42),
		FontColor:    "white",
		OutlineColor: "black",
		OutlineWidth: intPtr(2),
		LineSpacing:  intPtr(4),
	}

	defaultSegments := []OverlaySegment{
		{
			Name:     "intro-title",
			Template: "{title}",
			Style: TextStyle{
				FontSize: intPtr(64),
			},
			Position: PositionSpec{
				Origin:  "bottom-left",
				OffsetX: 40,
				OffsetY: 220,
			},
			Timing: TimingSpec{
				Start: TimePointSpec{
					Type:      "from_start",
					OffsetSec: 0,
				},
				End: TimePointSpec{
					Type:      "from_start",
					OffsetSec: 4,
				},
				FadeIn:  0.5,
				FadeOut: 0.5,
			},
		},
		{
			Name:      "intro-artist",
			Template:  "{artist}",
			Transform: "uppercase",
			Style: TextStyle{
				FontSize: intPtr(32),
			},
			Position: PositionSpec{
				Origin:  "bottom-left",
				OffsetX: 40,
				OffsetY: 160,
			},
			Timing: TimingSpec{
				Start: TimePointSpec{
					Type:      "from_start",
					OffsetSec: 0,
				},
				End: TimePointSpec{
					Type:      "from_start",
					OffsetSec: 4,
				},
				FadeIn:  0.5,
				FadeOut: 0.5,
			},
		},
		{
			Name:     "outro-name",
			Template: "{name}",
			Position: PositionSpec{
				Origin:  "bottom-left",
				OffsetX: 40,
				OffsetY: 40,
			},
			Timing: TimingSpec{
				Start: TimePointSpec{
					Type:      "from_end",
					OffsetSec: 4,
				},
				End: TimePointSpec{
					Type:      "from_end",
					OffsetSec: 0,
				},
			},
		},
		{
			Name:     "index-badge",
			Template: "{index}",
			Style: TextStyle{
				FontSize: intPtr(140),
			},
			Position: PositionSpec{
				Origin:  "bottom-right",
				OffsetX: 40,
				OffsetY: 40,
			},
			Timing: TimingSpec{
				Start: TimePointSpec{
					Type:      "from_start",
					OffsetSec: 0,
				},
				End: TimePointSpec{
					Type: "persistent",
				},
			},
		},
	}

	return Config{
		Version: 1,
		Video: VideoConfig{
			Width:  1920,
			Height: 1080,
			FPS:    30,
			Codec:  "libx264",
			CRF:    20,
			Preset: "medium",
		},
		Audio: AudioConfig{
			ACodec:      "aac",
			BitrateKbps: 192,
			SampleRate:  48000,
			Channels:    2,
			Loudnorm: LoudnormConfig{
				Enabled:        boolPtr(true),
				IntegratedLUFS: floatPtr(-14.0),
				TruePeak:       floatPtr(-1.5),
				LRA:            floatPtr(11.0),
			},
		},
		Profiles: ProfilesConfig{
			Overlays: map[string]OverlayProfile{
				defaultOverlayProfileName: {
					DefaultStyle: cloneTextStyle(defaultStyle),
					Segments:     cloneSegments(defaultSegments),
				},
			},
		},
		Clips: ClipsConfig{
			OverlayProfile: defaultOverlayProfileName,
			Song: ClipTypeConfig{
				Source: ClipSourceConfig{
					DefaultDurationSec: 60,
				},
				Render: ClipRenderConfig{
					FadeInSec:  floatPtr(0.5),
					FadeOutSec: floatPtr(0.5),
				},
				Overlays: ClipOverlayConfig{
					Profile: defaultOverlayProfileName,
				},
			},
			Interstitial: ClipTypeConfig{
				Source: ClipSourceConfig{
					DefaultDurationSec: 5,
				},
				Render: ClipRenderConfig{
					FadeInSec:  floatPtr(0.3),
					FadeOutSec: floatPtr(0.3),
				},
				Overlays: ClipOverlayConfig{
					Profile: "",
				},
			},
			Intro: ClipTypeConfig{},
			Outro: ClipTypeConfig{},
		},
		Files: FileOverrides{},
		Outputs: OutputConfig{
			SegmentTemplate: "$INDEX_PAD3_$SAFE_TITLE",
		},
		Plan: PlanConfig{
			DefaultDurationSec: 60,
		},
		Tools: ToolPins{},
		Downloads: DownloadsConfig{
			FilenameTemplate: "$ID",
		},
	}
}

// Load reads the YAML configuration from disk if it exists, otherwise returns
// the default configuration.
func Load(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := Default()
			cfg.ApplyDefaults()
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(contents, &cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

// ApplyDefaults ensures nested fields fall back to sensible defaults when the
// YAML omits them.
func (c *Config) ApplyDefaults() {
	defaults := Default()

	if c.Tools == nil {
		c.Tools = ToolPins{}
	}

	if c.Version == 0 {
		c.Version = defaults.Version
	}
	if c.Video.Width == 0 {
		c.Video.Width = defaults.Video.Width
	}
	if c.Video.Height == 0 {
		c.Video.Height = defaults.Video.Height
	}
	if c.Video.FPS == 0 {
		c.Video.FPS = defaults.Video.FPS
	}
	c.Video.Codec = strings.TrimSpace(c.Video.Codec)
	if c.Video.Codec == "" {
		c.Video.Codec = defaults.Video.Codec
	}
	if c.Video.CRF < 0 || c.Video.CRF > 51 {
		c.Video.CRF = defaults.Video.CRF
	}
	if c.Video.CRF == 0 {
		c.Video.CRF = defaults.Video.CRF
	}
	preset := strings.ToLower(strings.TrimSpace(c.Video.Preset))
	if preset == "" {
		c.Video.Preset = defaults.Video.Preset
	} else {
		if _, ok := allowedVideoPresets[preset]; ok {
			c.Video.Preset = preset
		} else {
			c.Video.Preset = defaults.Video.Preset
		}
	}
	if strings.TrimSpace(c.Audio.ACodec) == "" {
		c.Audio.ACodec = defaults.Audio.ACodec
	}
	if c.Audio.BitrateKbps <= 0 {
		c.Audio.BitrateKbps = defaults.Audio.BitrateKbps
	}
	if c.Audio.SampleRate == 0 {
		c.Audio.SampleRate = defaults.Audio.SampleRate
	}
	if c.Audio.SampleRate != 0 && c.Audio.SampleRate != 44100 && c.Audio.SampleRate != 48000 {
		c.Audio.SampleRate = defaults.Audio.SampleRate
	}
	if c.Audio.Channels == 0 {
		c.Audio.Channels = defaults.Audio.Channels
	}
	if c.Audio.Channels != 1 && c.Audio.Channels != 2 {
		c.Audio.Channels = defaults.Audio.Channels
	}
	c.Audio.Loudnorm.applyDefaults(defaults.Audio.Loudnorm)
	if strings.TrimSpace(c.Outputs.SegmentTemplate) == "" {
		c.Outputs.SegmentTemplate = defaults.Outputs.SegmentTemplate
	}
	if c.Profiles.Overlays == nil {
		c.Profiles.Overlays = map[string]OverlayProfile{}
	}
	if len(c.Profiles.Overlays) == 0 {
		for name, profile := range defaults.Profiles.Overlays {
			c.Profiles.Overlays[name] = cloneOverlayProfile(profile)
		}
	} else {
		for name, profile := range c.Profiles.Overlays {
			if base, ok := defaults.Profiles.Overlays[name]; ok {
				profile.DefaultStyle = mergeTextStyle(base.DefaultStyle, profile.DefaultStyle)
			} else {
				profile.DefaultStyle = cloneTextStyle(profile.DefaultStyle)
			}
			profile.Segments = cloneSegments(profile.Segments)
			c.Profiles.Overlays[name] = profile
		}
	}
	if strings.TrimSpace(c.Clips.OverlayProfile) == "" || !profileExists(c.Profiles.Overlays, strings.TrimSpace(c.Clips.OverlayProfile)) {
		c.Clips.OverlayProfile = pickDefaultProfileName(c.Profiles.Overlays)
	} else {
		c.Clips.OverlayProfile = strings.TrimSpace(c.Clips.OverlayProfile)
	}
	c.Clips.Song = mergeClipTypeConfig(defaults.Clips.Song, c.Clips.Song)
	c.Clips.Interstitial = mergeClipTypeConfig(defaults.Clips.Interstitial, c.Clips.Interstitial)
	c.Clips.Intro = mergeClipTypeConfig(defaults.Clips.Intro, c.Clips.Intro)
	c.Clips.Outro = mergeClipTypeConfig(defaults.Clips.Outro, c.Clips.Outro)
	if c.Clips.Song.Source.DefaultDurationSec <= 0 {
		if c.Plan.DefaultDurationSec > 0 {
			c.Clips.Song.Source.DefaultDurationSec = c.Plan.DefaultDurationSec
		} else {
			c.Clips.Song.Source.DefaultDurationSec = defaults.Clips.Song.Source.DefaultDurationSec
		}
	}
	if c.Clips.Interstitial.Source.DefaultDurationSec <= 0 {
		c.Clips.Interstitial.Source.DefaultDurationSec = defaults.Clips.Interstitial.Source.DefaultDurationSec
	}
	propagateClipOverlayDefaults(&c.Clips, c.Clips.OverlayProfile)
	normalizeClipOverrides(&c.Clips)
	if c.Plan.DefaultDurationSec <= 0 {
		c.Plan.DefaultDurationSec = c.Clips.Song.Source.DefaultDurationSec
		if c.Plan.DefaultDurationSec <= 0 {
			c.Plan.DefaultDurationSec = defaults.Plan.DefaultDurationSec
		}
	}
	if strings.TrimSpace(c.Downloads.FilenameTemplate) == "" {
		c.Downloads.FilenameTemplate = defaults.Downloads.FilenameTemplate
	}
}

// ToolVersion returns the pinned version for a given tool name when defined.
func (c Config) ToolVersion(tool string) string {
	if c.Tools == nil {
		return ""
	}
	if pin, ok := c.Tools[tool]; ok {
		return strings.TrimSpace(pin.Version)
	}
	return ""
}

// ToolMinimum returns the minimum version override for a given tool name when defined.
func (c Config) ToolMinimum(tool string) string {
	if c.Tools == nil {
		return ""
	}
	if pin, ok := c.Tools[tool]; ok {
		return strings.TrimSpace(pin.MinimumVersion)
	}
	return ""
}

// ToolProxy returns the proxy override for a given tool name when defined.
func (c Config) ToolProxy(tool string) string {
	if c.Tools == nil {
		return ""
	}
	if pin, ok := c.Tools[tool]; ok {
		return strings.TrimSpace(pin.Proxy)
	}
	return ""
}

// DownloadFilenameTemplate returns the configured filename template for downloads.
func (c Config) DownloadFilenameTemplate() string {
	return strings.TrimSpace(c.Downloads.FilenameTemplate)
}

// SegmentFilenameTemplate returns the configured template for rendered segments.
func (c Config) SegmentFilenameTemplate() string {
	return strings.TrimSpace(c.Outputs.SegmentTemplate)
}

// PlanDefaultDuration returns the default clip duration in seconds, falling back to 60.
func (c Config) PlanDefaultDuration() int {
	if c.Plan.DefaultDurationSec <= 0 {
		return 60
	}
	return c.Plan.DefaultDurationSec
}

// HeaderAliases returns normalized header alias definitions for the plan loader.
func (c Config) HeaderAliases() map[string][]string {
	if len(c.Plan.Headers) == 0 {
		return nil
	}

	aliases := make(map[string][]string, len(c.Plan.Headers))
	for key, values := range c.Plan.Headers {
		canonical := normalizePlanHeaderKey(key)
		if canonical == "" {
			continue
		}
		var cleaned []string
		for _, alias := range values {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			cleaned = append(cleaned, alias)
		}
		if len(cleaned) == 0 {
			continue
		}
		aliases[canonical] = cleaned
	}

	if len(aliases) == 0 {
		return nil
	}
	return aliases
}

// PlanFile returns the trimmed plan file override when provided.
func (c Config) PlanFile() string {
	return strings.TrimSpace(c.Files.Plan)
}

// CookiesFile returns the trimmed cookies file override when provided.
func (c Config) CookiesFile() string {
	return strings.TrimSpace(c.Files.Cookies)
}

// ToolMinimums returns a copy of all configured minimum version overrides.
func (c Config) ToolMinimums() map[string]string {
	if len(c.Tools) == 0 {
		return nil
	}
	mins := make(map[string]string, len(c.Tools))
	for name, pin := range c.Tools {
		if v := strings.TrimSpace(pin.MinimumVersion); v != "" {
			mins[name] = v
		}
	}
	if len(mins) == 0 {
		return nil
	}
	return mins
}

// Marshal returns the YAML encoding of the configuration.
func (c Config) Marshal() ([]byte, error) {
	buf, err := yaml.Marshal(&c)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return buf, nil
}

func profileExists(profiles map[string]OverlayProfile, name string) bool {
	if len(profiles) == 0 {
		return false
	}
	_, ok := profiles[strings.TrimSpace(name)]
	return ok
}

func pickDefaultProfileName(profiles map[string]OverlayProfile) string {
	if len(profiles) == 0 {
		return defaultOverlayProfileName
	}
	if _, ok := profiles[defaultOverlayProfileName]; ok {
		return defaultOverlayProfileName
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names[0]
}

func mergeClipTypeConfig(base, override ClipTypeConfig) ClipTypeConfig {
	result := ClipTypeConfig{
		Source:   mergeClipSourceConfig(base.Source, override.Source),
		Render:   mergeClipRenderConfig(base.Render, override.Render),
		Overlays: mergeClipOverlayConfig(base.Overlays, override.Overlays),
	}
	return result
}

func mergeClipSourceConfig(base, override ClipSourceConfig) ClipSourceConfig {
	result := base
	if plan := strings.TrimSpace(override.Plan); plan != "" {
		result.Plan = plan
	}
	if media := strings.TrimSpace(override.Media); media != "" {
		result.Media = media
	}
	if override.DefaultDurationSec > 0 {
		result.DefaultDurationSec = override.DefaultDurationSec
	}
	return result
}

func mergeClipRenderConfig(base, override ClipRenderConfig) ClipRenderConfig {
	result := cloneClipRenderConfig(base)
	if override.DurationSec != nil {
		result.DurationSec = intPtr(*override.DurationSec)
	}
	if override.FadeInSec != nil {
		result.FadeInSec = floatPtr(*override.FadeInSec)
	}
	if override.FadeOutSec != nil {
		result.FadeOutSec = floatPtr(*override.FadeOutSec)
	}
	return result
}

func mergeClipOverlayConfig(base, override ClipOverlayConfig) ClipOverlayConfig {
	result := base
	if name := strings.TrimSpace(override.Profile); name != "" {
		result.Profile = name
	}
	return result
}

func cloneClipRenderConfig(cfg ClipRenderConfig) ClipRenderConfig {
	return ClipRenderConfig{
		DurationSec: copyIntPtr(cfg.DurationSec),
		FadeInSec:   copyFloatPtr(cfg.FadeInSec),
		FadeOutSec:  copyFloatPtr(cfg.FadeOutSec),
	}
}

func propagateClipOverlayDefaults(cfg *ClipsConfig, fallback string) {
	if cfg == nil {
		return
	}
	fallback = strings.TrimSpace(fallback)
	apply := func(ct *ClipTypeConfig) {
		if ct == nil {
			return
		}
		if name := strings.TrimSpace(ct.Overlays.Profile); name != "" {
			ct.Overlays.Profile = name
			return
		}
		ct.Overlays.Profile = fallback
	}
	apply(&cfg.Song)
	apply(&cfg.Interstitial)
	apply(&cfg.Intro)
	apply(&cfg.Outro)
}

func normalizeClipOverrides(cfg *ClipsConfig) {
	if cfg == nil {
		return
	}
	for i := range cfg.Overrides {
		override := &cfg.Overrides[i]

		override.Match.ClipType = strings.TrimSpace(strings.ToLower(override.Match.ClipType))
		if override.Match.Index != nil && *override.Match.Index <= 0 {
			override.Match.Index = nil
		}
		override.Match.Name = strings.TrimSpace(override.Match.Name)
		override.Match.ID = strings.TrimSpace(override.Match.ID)

		override.Overlays.Profile = strings.TrimSpace(override.Overlays.Profile)

		for j := range override.Overlays.Segments {
			segment := &override.Overlays.Segments[j]
			segment.Name = strings.TrimSpace(segment.Name)
			if segment.Template != nil {
				trimmed := strings.TrimSpace(*segment.Template)
				segment.Template = stringPtr(trimmed)
			}
			if segment.Style != nil {
				style := cloneTextStyle(*segment.Style)
				segment.Style = &style
			}
		}
	}
}

func mergeTextStyle(base, override TextStyle) TextStyle {
	result := cloneTextStyle(base)

	if strings.TrimSpace(override.FontFile) != "" {
		result.FontFile = override.FontFile
	}
	if override.FontSize != nil {
		result.FontSize = intPtr(*override.FontSize)
	}
	if strings.TrimSpace(override.FontColor) != "" {
		result.FontColor = override.FontColor
	}
	if strings.TrimSpace(override.OutlineColor) != "" {
		result.OutlineColor = override.OutlineColor
	}
	if override.OutlineWidth != nil {
		result.OutlineWidth = intPtr(*override.OutlineWidth)
	}
	if override.LineSpacing != nil {
		result.LineSpacing = intPtr(*override.LineSpacing)
	}
	if override.LetterSpacing != nil {
		result.LetterSpacing = intPtr(*override.LetterSpacing)
	}

	return result
}

func cloneTextStyle(style TextStyle) TextStyle {
	clone := style
	if style.FontSize != nil {
		clone.FontSize = intPtr(*style.FontSize)
	}
	if style.OutlineWidth != nil {
		clone.OutlineWidth = intPtr(*style.OutlineWidth)
	}
	if style.LineSpacing != nil {
		clone.LineSpacing = intPtr(*style.LineSpacing)
	}
	if style.LetterSpacing != nil {
		clone.LetterSpacing = intPtr(*style.LetterSpacing)
	}
	return clone
}

func cloneSegments(segments []OverlaySegment) []OverlaySegment {
	if len(segments) == 0 {
		return nil
	}
	clones := make([]OverlaySegment, len(segments))
	for i, segment := range segments {
		clones[i] = segment
		clones[i].Style = cloneTextStyle(segment.Style)
	}
	return clones
}

func cloneOverlayProfile(profile OverlayProfile) OverlayProfile {
	return OverlayProfile{
		DefaultStyle: cloneTextStyle(profile.DefaultStyle),
		Segments:     cloneSegments(profile.Segments),
	}
}

func copyIntPtr(src *int) *int {
	if src == nil {
		return nil
	}
	return intPtr(*src)
}

func copyFloatPtr(src *float64) *float64 {
	if src == nil {
		return nil
	}
	return floatPtr(*src)
}

func boolPtr(v bool) *bool {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}

func intPtr(v int) *int {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func normalizePlanHeaderKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ToLower(value)
	replacer := strings.NewReplacer(
		" ", "_",
		"-", "_",
		".", "_",
		"/", "_",
	)
	value = replacer.Replace(value)
	value = strings.Trim(value, "_")
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	return value
}
