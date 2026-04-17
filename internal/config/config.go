package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// OverlayEntry specifies a single overlay preset or raw filter for a collection.
type OverlayEntry struct {
	Type    string            `yaml:"type"`
	Options map[string]string `yaml:",inline"`
	Filters []string          `yaml:"filters,omitempty"`
}

// CollectionConfig defines a collection of clips with configurable CSV headers.
type CollectionConfig struct {
	Plan               string         `yaml:"plan"`
	File               string         `yaml:"file,omitempty"`
	Duration           int            `yaml:"duration,omitempty"`
	OutputDir          string         `yaml:"output_dir"`
	Fade               float64        `yaml:"fade,omitempty"`
	FadeIn             float64        `yaml:"fade_in,omitempty"`
	FadeOut            float64        `yaml:"fade_out,omitempty"`
	Overlays           []OverlayEntry `yaml:"overlays,omitempty"`
	LinkHeader         string         `yaml:"link_header"`
	StartHeader        string         `yaml:"start_header"`
	DurationHeader     string         `yaml:"duration_header"`
	CacheSearchProfile string         `yaml:"cache_search_profile,omitempty"`
}

// TimelineConfig defines the playback sequence for the power hour.
type TimelineConfig struct {
	Sequence []SequenceEntry `yaml:"sequence"`
}

// SequenceEntry defines how a single collection or inline file appears in the timeline.
// Exactly one of Collection or File must be set.
type SequenceEntry struct {
	Collection string            `yaml:"collection,omitempty"`
	Count      int               `yaml:"count,omitempty"`      // 0 = play all; only valid with Collection
	File       string            `yaml:"file,omitempty"`       // inline file path; mutually exclusive with Collection
	Interleave *InterleaveConfig `yaml:"interleave,omitempty"` // only valid with Collection
	Fade       float64           `yaml:"fade,omitempty"`
	FadeIn     float64           `yaml:"fade_in,omitempty"`
	FadeOut    float64           `yaml:"fade_out,omitempty"`
}

// ResolveFade computes effective fade-in and fade-out durations from the three
// fade fields. fade is a shorthand that splits evenly; individual values
// override the split when set.
func ResolveFade(fade, fadeIn, fadeOut float64) (in, out float64) {
	in, out = fadeIn, fadeOut
	if fade > 0 {
		if in == 0 {
			in = fade / 2
		}
		if out == 0 {
			out = fade / 2
		}
	}
	return
}

// InterleaveConfig describes how to splice a second collection into a sequence entry.
type InterleaveConfig struct {
	Collection string `yaml:"collection"`
	Every      int    `yaml:"every"`
	// Placement controls where interstitials appear relative to the primary clip groups.
	// Valid values: "between" (default), "after", "before", "around".
	//   between - interstitials play between groups, not before the first or after the last
	//   after   - interstitials play after every Nth group, including the last (legacy behavior)
	//   before  - interstitials play before every Nth group, including the first
	//   around  - interstitials play before every group AND after the last primary
	Placement string `yaml:"placement,omitempty"`
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

// EncodingConfig captures concat encoding settings for a project.
// All fields are optional; the concat command merges project overrides >
// global defaults > built-in fallback. Mirrors tools.EncodingDefaults.
type EncodingConfig struct {
	// Video
	VideoCodec   string `yaml:"video_codec,omitempty"`
	Width        int    `yaml:"width,omitempty"`
	Height       int    `yaml:"height,omitempty"`
	FPS          int    `yaml:"fps,omitempty"`
	CRF          int    `yaml:"crf,omitempty"`
	Preset       string `yaml:"preset,omitempty"`
	VideoBitrate string `yaml:"video_bitrate,omitempty"`
	Container    string `yaml:"container,omitempty"`

	// Audio
	AudioCodec   string `yaml:"audio_codec,omitempty"`
	AudioBitrate string `yaml:"audio_bitrate,omitempty"`
	SampleRate   int    `yaml:"sample_rate,omitempty"`
	Channels     int    `yaml:"channels,omitempty"`

	// Loudness normalization
	LoudnormEnabled  *bool    `yaml:"loudnorm_enabled,omitempty"`
	LoudnormLUFS     *float64 `yaml:"loudnorm_lufs,omitempty"`
	LoudnormTruePeak *float64 `yaml:"loudnorm_true_peak_db,omitempty"`
	LoudnormLRA      *float64 `yaml:"loudnorm_lra_db,omitempty"`
}

// Config captures the rendering and overlay configuration for a project.
type Config struct {
	Version         int                         `yaml:"version"`
	Video           VideoConfig                 `yaml:"video"`
	Audio           AudioConfig                 `yaml:"audio"`
	CollectionFiles []string                    `yaml:"collection_files,omitempty"`
	Collections     map[string]CollectionConfig `yaml:"collections"`
	Timeline        TimelineConfig              `yaml:"timeline"`
	Outputs         OutputConfig                `yaml:"outputs"`
	Plan            PlanConfig                  `yaml:"plan"`
	Files           FileOverrides               `yaml:"files"`
	Tools           ToolPins                    `yaml:"tools"`
	Downloads       DownloadsConfig             `yaml:"downloads"`
	Cache           CacheConfig                 `yaml:"cache"`
	Library         LibraryConfig               `yaml:"library"`
	SegmentsBaseDir string                      `yaml:"segments_base_dir"`
	Encoding        EncodingConfig              `yaml:"encoding,omitempty"`
}

// CacheConfig controls how cache metadata is displayed and searched in the TUI.
type CacheConfig struct {
	View           CacheViewConfig               `yaml:"view"`
	SearchProfiles map[string]CacheSearchProfile `yaml:"search_profiles,omitempty"`
}

// CacheViewConfig controls the cache tab's metadata columns via ordered fallbacks.
type CacheViewConfig struct {
	PrimaryFields   []string `yaml:"primary_fields,omitempty"`
	SecondaryFields []string `yaml:"secondary_fields,omitempty"`
}

// CacheSearchProfile controls add-slot fuzzy search and row fill behavior.
type CacheSearchProfile struct {
	SearchFields []string        `yaml:"search_fields,omitempty"`
	Fill         CacheFillConfig `yaml:"fill,omitempty"`
}

// CacheFillConfig controls which cache metadata fills row fields, in order.
type CacheFillConfig struct {
	TitleFields  []string `yaml:"title_fields,omitempty"`
	ArtistFields []string `yaml:"artist_fields,omitempty"`
	LinkFields   []string `yaml:"link_fields,omitempty"`
}

// ToolPins captures optional version pinning for managed external tools.
type ToolPins map[string]ToolPin

// ToolPin represents overrides for an individual tool.
type ToolPin struct {
	Version        string `yaml:"version"`
	MinimumVersion string `yaml:"minimum_version"`
	Proxy          string `yaml:"proxy"`
	SourceAddress  string `yaml:"source_address"`
}

// DownloadsConfig controls caching/downloading behaviour.
type DownloadsConfig struct {
	FilenameTemplate string `yaml:"filename_template"`
}

// LibraryConfig controls the shared media library.
type LibraryConfig struct {
	Mode string `yaml:"mode,omitempty"` // "shared" (default) or "local"
	Path string `yaml:"path,omitempty"` // custom library root path override
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
		Collections: map[string]CollectionConfig{
			"songs": {
				Plan:               "songs.yaml",
				OutputDir:          "songs",
				Overlays:           []OverlayEntry{{Type: "song-info"}},
				LinkHeader:         "link",
				StartHeader:        "start_time",
				DurationHeader:     "duration",
				CacheSearchProfile: "song_lookup",
			},
			"interstitials": {
				Plan:           "interstitials.yaml",
				OutputDir:      "interstitials",
				Overlays:       []OverlayEntry{{Type: "drink"}},
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
		Tools:     ToolPins{},
		Downloads: DownloadsConfig{FilenameTemplate: "$ID"},
		Cache: CacheConfig{
			View: CacheViewConfig{
				PrimaryFields:   []string{"title", "track"},
				SecondaryFields: []string{"artist", "uploader", "channel"},
			},
			SearchProfiles: map[string]CacheSearchProfile{
				"song_lookup": {
					SearchFields: []string{"title", "artist"},
					Fill: CacheFillConfig{
						TitleFields:  []string{"title", "track"},
						ArtistFields: []string{"artist", "uploader", "channel"},
						LinkFields:   []string{"source", "links"},
					},
				},
			},
		},
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

	projectRoot := filepath.Dir(path)
	if err := cfg.loadCollectionFiles(projectRoot); err != nil {
		return Config{}, err
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
	if c.Plan.DefaultDurationSec <= 0 {
		c.Plan.DefaultDurationSec = defaults.Plan.DefaultDurationSec
	}
	if strings.TrimSpace(c.Downloads.FilenameTemplate) == "" {
		c.Downloads.FilenameTemplate = defaults.Downloads.FilenameTemplate
	}
	c.Cache.applyDefaults(defaults.Cache)
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

// ToolSourceAddress returns the source address override for a given tool name when defined.
func (c Config) ToolSourceAddress(tool string) string {
	if c.Tools == nil {
		return ""
	}
	if pin, ok := c.Tools[tool]; ok {
		return strings.TrimSpace(pin.SourceAddress)
	}
	return ""
}

// LibraryShared returns true when the shared media library should be used.
// Defaults to true when mode is empty or "shared".
func (c Config) LibraryShared() bool {
	mode := strings.TrimSpace(strings.ToLower(c.Library.Mode))
	return mode == "" || mode == "shared"
}

// LibraryPath returns the configured library path override, if any.
func (c Config) LibraryPath() string {
	return strings.TrimSpace(c.Library.Path)
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

		collection.CacheSearchProfile = strings.TrimSpace(collection.CacheSearchProfile)

		// Apply default output directory
		if strings.TrimSpace(collection.OutputDir) == "" {
			collection.OutputDir = name
		}

		c.Collections[name] = collection
	}
}

func (c *CacheConfig) applyDefaults(defaults CacheConfig) {
	if c == nil {
		return
	}
	c.View.PrimaryFields = normalizeCacheFieldList(c.View.PrimaryFields)
	c.View.SecondaryFields = normalizeCacheFieldList(c.View.SecondaryFields)
	if len(c.View.PrimaryFields) == 0 {
		c.View.PrimaryFields = append([]string(nil), defaults.View.PrimaryFields...)
	}
	if len(c.View.SecondaryFields) == 0 {
		c.View.SecondaryFields = append([]string(nil), defaults.View.SecondaryFields...)
	}
	if c.SearchProfiles == nil {
		c.SearchProfiles = map[string]CacheSearchProfile{}
	}
	for name, profile := range defaults.SearchProfiles {
		existing, ok := c.SearchProfiles[name]
		if !ok {
			c.SearchProfiles[name] = profile
			continue
		}
		existing.applyDefaults(profile)
		c.SearchProfiles[name] = existing
	}
}

func (p *CacheSearchProfile) applyDefaults(defaults CacheSearchProfile) {
	if p == nil {
		return
	}
	p.SearchFields = normalizeCacheFieldList(p.SearchFields)
	p.Fill.TitleFields = normalizeCacheFieldList(p.Fill.TitleFields)
	p.Fill.ArtistFields = normalizeCacheFieldList(p.Fill.ArtistFields)
	p.Fill.LinkFields = normalizeCacheFieldList(p.Fill.LinkFields)
	if len(p.SearchFields) == 0 {
		p.SearchFields = append([]string(nil), defaults.SearchFields...)
	}
	if len(p.Fill.TitleFields) == 0 {
		p.Fill.TitleFields = append([]string(nil), defaults.Fill.TitleFields...)
	}
	if len(p.Fill.ArtistFields) == 0 {
		p.Fill.ArtistFields = append([]string(nil), defaults.Fill.ArtistFields...)
	}
	if len(p.Fill.LinkFields) == 0 {
		p.Fill.LinkFields = append([]string(nil), defaults.Fill.LinkFields...)
	}
}

func (c Config) CacheSearchProfile(name string) (CacheSearchProfile, bool) {
	if len(c.Cache.SearchProfiles) == 0 {
		return CacheSearchProfile{}, false
	}
	profile, ok := c.Cache.SearchProfiles[strings.TrimSpace(name)]
	return profile, ok
}

func normalizeCacheFieldList(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(strings.ToLower(field))
		if field == "" {
			continue
		}
		out = append(out, field)
	}
	return out
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
		hasFile := strings.TrimSpace(collection.File) != ""
		hasPlan := strings.TrimSpace(collection.Plan) != ""

		if hasFile && hasPlan {
			return fmt.Errorf("collection %q: cannot specify both file and plan", name)
		}
		if !hasFile && !hasPlan {
			return fmt.Errorf("collection %q: either file or plan is required", name)
		}

		// Header validation only applies to plan-based collections
		if hasPlan {
			if protectedHeaders[normalizeHeaderName(collection.LinkHeader)] {
				return fmt.Errorf("collection %q: link_header cannot be %q (protected name)", name, collection.LinkHeader)
			}
			if protectedHeaders[normalizeHeaderName(collection.StartHeader)] {
				return fmt.Errorf("collection %q: start_header cannot be %q (protected name)", name, collection.StartHeader)
			}
			if collection.DurationHeader != "" && protectedHeaders[normalizeHeaderName(collection.DurationHeader)] {
				return fmt.Errorf("collection %q: duration_header cannot be %q (protected name)", name, collection.DurationHeader)
			}
		}
	}

	return nil
}
