package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// CollectionConfig defines a collection of clips with configurable CSV headers.
type CollectionConfig struct {
	Plan           string `yaml:"plan"`
	OutputDir      string `yaml:"output_dir"`
	Profile        string `yaml:"profile"`
	LinkHeader     string `yaml:"link_header"`
	StartHeader    string `yaml:"start_header"`
	DurationHeader string `yaml:"duration_header"`
}

// TimelineConfig defines the playback sequence for the power hour.
type TimelineConfig struct {
	Sequence []SequenceEntry `yaml:"sequence"`
}

// SequenceEntry defines how a single collection appears in the timeline.
type SequenceEntry struct {
	Collection string            `yaml:"collection"`
	Count      int               `yaml:"count,omitempty"` // 0 = play all
	Interleave *InterleaveConfig `yaml:"interleave,omitempty"`
}

// InterleaveConfig describes how to splice a second collection into a sequence entry.
type InterleaveConfig struct {
	Collection string `yaml:"collection"`
	Every      int    `yaml:"every"`
}

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
	Version         int                        `yaml:"version"`
	Video           VideoConfig                `yaml:"video"`
	Audio           AudioConfig                `yaml:"audio"`
	Profiles        ProfilesConfig             `yaml:"profiles"`
	Collections     map[string]CollectionConfig `yaml:"collections"`
	Timeline        TimelineConfig              `yaml:"timeline"`
	Outputs         OutputConfig               `yaml:"outputs"`
	Plan            PlanConfig                 `yaml:"plan"`
	Files           FileOverrides              `yaml:"files"`
	Tools           ToolPins                   `yaml:"tools"`
	Downloads       DownloadsConfig            `yaml:"downloads"`
	SegmentsBaseDir string                     `yaml:"segments_base_dir"`
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
	GlobalCache      *bool  `yaml:"global_cache,omitempty"` // nil = true (default on)
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
type ProfilesConfig map[string]OverlayProfile

// OverlayProfile defines a named overlay style composed of segments and defaults.
type OverlayProfile struct {
	DefaultStyle       TextStyle        `yaml:"default_style"`
	Segments           []OverlaySegment `yaml:"segments"`
	DefaultDurationSec *int             `yaml:"default_duration_s,omitempty"`
	FadeInSec          *float64         `yaml:"fade_in_s,omitempty"`
	FadeOutSec         *float64         `yaml:"fade_out_s,omitempty"`
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
	ZIndex    *int         `yaml:"z_index,omitempty"` // Optional: controls draw order (higher = on top)
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
	// Song profile - title/artist intro + index badge
	songStyle := TextStyle{
		FontFile:     "",
		FontSize:     intPtr(42),
		FontColor:    "white",
		OutlineColor: "black",
		OutlineWidth: intPtr(2),
		LineSpacing:  intPtr(4),
	}

	songSegments := []OverlaySegment{
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

	// Interstitial profile - "Drink!" with yellow text, black outline, yellow drop shadow
	interstitialStyle := TextStyle{
		FontFile:     "",
		FontSize:     intPtr(120),
		FontColor:    "yellow",
		OutlineColor: "black",
		OutlineWidth: intPtr(4),
		LineSpacing:  intPtr(4),
	}

	interstitialSegments := []OverlaySegment{
		{
			Name:     "drink-shadow",
			Template: "Drink!",
			Style: TextStyle{
				FontSize:     intPtr(120),
				FontColor:    "yellow@0.6", // Semi-transparent yellow for shadow
				OutlineColor: "black@0",    // No outline on shadow
				OutlineWidth: intPtr(0),
			},
			Position: PositionSpec{
				Origin:  "bottom-center",
				OffsetX: 8,  // Shadow offset right
				OffsetY: 192, // Shadow offset up (200 - 8)
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
		{
			Name:     "drink-text",
			Template: "Drink!",
			Style: TextStyle{
				FontSize: intPtr(120),
			},
			Position: PositionSpec{
				Origin:  "bottom-center",
				OffsetX: 0,
				OffsetY: 200,
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
			"song-main": {
				DefaultStyle: cloneTextStyle(songStyle),
				Segments:     cloneSegments(songSegments),
			},
			"interstitial-drink": {
				DefaultStyle: cloneTextStyle(interstitialStyle),
				Segments:     cloneSegments(interstitialSegments),
			},
		},
		Collections: map[string]CollectionConfig{
			"songs": {
				Plan:           "powerhour.csv",
				OutputDir:      "songs",
				Profile:        "song-main",
				LinkHeader:     "link",
				StartHeader:    "start_time",
				DurationHeader: "duration",
			},
			"interstitials": {
				Plan:           "interstitials.csv",
				OutputDir:      "interstitials",
				Profile:        "interstitial-drink",
				LinkHeader:     "link",
				StartHeader:    "start_time",
				DurationHeader: "duration",
			},
		},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{
					Collection: "songs",
					Interleave: &InterleaveConfig{
						Collection: "interstitials",
						Every:      1,
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
		Tools:           ToolPins{},
		Downloads:       DownloadsConfig{FilenameTemplate: "$ID"},
		SegmentsBaseDir: "segments",
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
	if c.Profiles == nil {
		c.Profiles = map[string]OverlayProfile{}
	}
	if len(c.Profiles) == 0 {
		for name, profile := range defaults.Profiles {
			c.Profiles[name] = cloneOverlayProfile(profile)
		}
	} else {
		for name, profile := range c.Profiles {
			if base, ok := defaults.Profiles[name]; ok {
				profile.DefaultStyle = mergeTextStyle(base.DefaultStyle, profile.DefaultStyle)
			} else {
				profile.DefaultStyle = cloneTextStyle(profile.DefaultStyle)
			}
			profile.Segments = cloneSegments(profile.Segments)
			c.Profiles[name] = profile
		}
	}
	if c.Plan.DefaultDurationSec <= 0 {
		c.Plan.DefaultDurationSec = defaults.Plan.DefaultDurationSec
	}
	if strings.TrimSpace(c.Downloads.FilenameTemplate) == "" {
		c.Downloads.FilenameTemplate = defaults.Downloads.FilenameTemplate
	}
	if strings.TrimSpace(c.SegmentsBaseDir) == "" {
		c.SegmentsBaseDir = "segments"
	}
	c.applyCollectionDefaults()
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

// GlobalCacheEnabled returns true when the global cache should be used.
// Defaults to true when the field is nil (not set in config).
func (c Config) GlobalCacheEnabled() bool {
	if c.Downloads.GlobalCache == nil {
		return true
	}
	return *c.Downloads.GlobalCache
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
		DefaultStyle:       cloneTextStyle(profile.DefaultStyle),
		Segments:           cloneSegments(profile.Segments),
		DefaultDurationSec: copyIntPtr(profile.DefaultDurationSec),
		FadeInSec:          copyFloatPtr(profile.FadeInSec),
		FadeOutSec:         copyFloatPtr(profile.FadeOutSec),
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

func normalizeHeaderName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToLower(value)
}

func (c *Config) applyCollectionDefaults() {
	if c.Collections == nil {
		return
	}

	for name, collection := range c.Collections {
		// Apply default header names
		if normalizeHeaderName(collection.LinkHeader) == "" {
			collection.LinkHeader = "link"
		} else {
			collection.LinkHeader = normalizeHeaderName(collection.LinkHeader)
		}

		if normalizeHeaderName(collection.StartHeader) == "" {
			collection.StartHeader = "start_time"
		} else {
			collection.StartHeader = normalizeHeaderName(collection.StartHeader)
		}

		if collection.DurationHeader != "" {
			collection.DurationHeader = normalizeHeaderName(collection.DurationHeader)
		} else {
			collection.DurationHeader = "duration"
		}

		// Apply default output directory
		if strings.TrimSpace(collection.OutputDir) == "" {
			collection.OutputDir = name
		}

		// Normalize profile name
		collection.Profile = strings.TrimSpace(collection.Profile)

		c.Collections[name] = collection
	}
}

// ValidateCollections validates collection configurations and returns errors.
func (c Config) ValidateCollections() error {
	if c.Collections == nil {
		return nil
	}

	protectedHeaders := map[string]bool{
		"index": true,
		"id":    true,
	}

	for name, collection := range c.Collections {
		// Check for protected header names
		if protectedHeaders[normalizeHeaderName(collection.LinkHeader)] {
			return fmt.Errorf("collection %q: link_header cannot be %q (protected name)", name, collection.LinkHeader)
		}
		if protectedHeaders[normalizeHeaderName(collection.StartHeader)] {
			return fmt.Errorf("collection %q: start_header cannot be %q (protected name)", name, collection.StartHeader)
		}
		if collection.DurationHeader != "" && protectedHeaders[normalizeHeaderName(collection.DurationHeader)] {
			return fmt.Errorf("collection %q: duration_header cannot be %q (protected name)", name, collection.DurationHeader)
		}

		// Validate profile exists if specified
		if collection.Profile != "" {
			if !profileExists(c.Profiles, collection.Profile) {
				return fmt.Errorf("collection %q: profile %q does not exist", name, collection.Profile)
			}
		}

		// Validate plan file is specified
		if strings.TrimSpace(collection.Plan) == "" {
			return fmt.Errorf("collection %q: plan file path is required", name)
		}
	}

	return nil
}
