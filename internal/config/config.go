package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config captures the rendering and overlay configuration for a project.
type Config struct {
	Version   int             `yaml:"version"`
	Video     VideoConfig     `yaml:"video"`
	Audio     AudioConfig     `yaml:"audio"`
	Overlays  OverlaysConfig  `yaml:"overlays"`
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
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
	FPS    int `yaml:"fps"`
}

// AudioConfig describes audio encoding parameters.
type AudioConfig struct {
	ACodec      string         `yaml:"acodec"`
	BitrateKbps int            `yaml:"bitrate_kbps"`
	SampleRate  int            `yaml:"sample_rate"`
	Loudnorm    LoudnormConfig `yaml:"loudnorm"`
}

// OverlaysConfig groups overlay styling defaults and individual segments.
type OverlaysConfig struct {
	DefaultStyle TextStyle        `yaml:"default_style"`
	Segments     []OverlaySegment `yaml:"segments"`
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
	return Config{
		Version: 1,
		Video: VideoConfig{
			Width:  1920,
			Height: 1080,
			FPS:    30,
		},
		Audio: AudioConfig{
			ACodec:      "aac",
			BitrateKbps: 192,
			SampleRate:  48000,
			Loudnorm: LoudnormConfig{
				Enabled:        boolPtr(true),
				IntegratedLUFS: floatPtr(-14.0),
				TruePeak:       floatPtr(-1.5),
				LRA:            floatPtr(11.0),
			},
		},
		Overlays: OverlaysConfig{
			DefaultStyle: TextStyle{
				FontFile:     "",
				FontSize:     intPtr(42),
				FontColor:    "white",
				OutlineColor: "black",
				OutlineWidth: intPtr(2),
				LineSpacing:  intPtr(4),
			},
			Segments: []OverlaySegment{
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
			},
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
	if c.Audio.ACodec == "" {
		c.Audio.ACodec = defaults.Audio.ACodec
	}
	if c.Audio.BitrateKbps == 0 {
		c.Audio.BitrateKbps = defaults.Audio.BitrateKbps
	}
	if c.Audio.SampleRate == 0 {
		c.Audio.SampleRate = defaults.Audio.SampleRate
	}
	c.Audio.Loudnorm.applyDefaults(defaults.Audio.Loudnorm)
	if strings.TrimSpace(c.Outputs.SegmentTemplate) == "" {
		c.Outputs.SegmentTemplate = defaults.Outputs.SegmentTemplate
	}
	c.Overlays.DefaultStyle = mergeTextStyle(defaults.Overlays.DefaultStyle, c.Overlays.DefaultStyle)
	if len(c.Overlays.Segments) == 0 {
		c.Overlays.Segments = cloneSegments(defaults.Overlays.Segments)
	}
	if c.Plan.DefaultDurationSec <= 0 {
		c.Plan.DefaultDurationSec = defaults.Plan.DefaultDurationSec
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

func boolPtr(v bool) *bool {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}

func intPtr(v int) *int {
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
