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
	ACodec      string `yaml:"acodec"`
	BitrateKbps int    `yaml:"bitrate_kbps"`
}

// OverlaysConfig groups overlay settings and templates.
type OverlaysConfig struct {
	FontFile      string            `yaml:"font_file"`
	FontSizeMain  int               `yaml:"font_size_main"`
	FontSizeIndex int               `yaml:"font_size_index"`
	Color         string            `yaml:"color"`
	OutlineColor  string            `yaml:"outline_color"`
	BeginText     TimedTextOverlay  `yaml:"begin_text"`
	EndText       TimedTextOverlay  `yaml:"end_text"`
	IndexBadge    IndexBadgeOverlay `yaml:"index_badge"`
}

// TimedTextOverlay represents a timed text overlay configuration.
type TimedTextOverlay struct {
	Template         string  `yaml:"template"`
	DurationSec      float64 `yaml:"duration_s"`
	FadeInSec        float64 `yaml:"fade_in_s"`
	FadeOutSec       float64 `yaml:"fade_out_s"`
	OffsetFromEndSec float64 `yaml:"offset_from_end_s"`
}

// IndexBadgeOverlay controls the persistent index badge overlay.
type IndexBadgeOverlay struct {
	Template   string `yaml:"template"`
	Persistent *bool  `yaml:"persistent,omitempty"`
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

// PersistentValue returns the effective persistent flag applying defaults.
func (o IndexBadgeOverlay) PersistentValue() bool {
	if o.Persistent == nil {
		return true
	}
	return *o.Persistent
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
		},
		Overlays: OverlaysConfig{
			FontFile:      "",
			FontSizeMain:  42,
			FontSizeIndex: 36,
			Color:         "white",
			OutlineColor:  "black",
			BeginText: TimedTextOverlay{
				Template:    "{title} — {artist}",
				DurationSec: 4.0,
				FadeInSec:   0.5,
				FadeOutSec:  0.5,
			},
			EndText: TimedTextOverlay{
				Template:         "{name}",
				OffsetFromEndSec: 4.0,
				DurationSec:      4.0,
			},
			IndexBadge: IndexBadgeOverlay{
				Template:   "{index}",
				Persistent: boolPtr(true),
			},
		},
		Files: FileOverrides{},
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
	if c.Overlays.FontSizeMain == 0 {
		c.Overlays.FontSizeMain = defaults.Overlays.FontSizeMain
	}
	if c.Overlays.FontSizeIndex == 0 {
		c.Overlays.FontSizeIndex = defaults.Overlays.FontSizeIndex
	}
	if c.Overlays.Color == "" {
		c.Overlays.Color = defaults.Overlays.Color
	}
	if c.Overlays.OutlineColor == "" {
		c.Overlays.OutlineColor = defaults.Overlays.OutlineColor
	}
	if c.Overlays.FontFile == "" {
		c.Overlays.FontFile = defaults.Overlays.FontFile
	}
	if c.Overlays.BeginText.Template == "" {
		c.Overlays.BeginText.Template = defaults.Overlays.BeginText.Template
	}
	if c.Overlays.BeginText.DurationSec == 0 {
		c.Overlays.BeginText.DurationSec = defaults.Overlays.BeginText.DurationSec
	}
	if c.Overlays.BeginText.FadeInSec == 0 {
		c.Overlays.BeginText.FadeInSec = defaults.Overlays.BeginText.FadeInSec
	}
	if c.Overlays.BeginText.FadeOutSec == 0 {
		c.Overlays.BeginText.FadeOutSec = defaults.Overlays.BeginText.FadeOutSec
	}
	if c.Overlays.EndText.Template == "" {
		c.Overlays.EndText.Template = defaults.Overlays.EndText.Template
	}
	if c.Overlays.EndText.DurationSec == 0 {
		c.Overlays.EndText.DurationSec = defaults.Overlays.EndText.DurationSec
	}
	if c.Overlays.EndText.OffsetFromEndSec == 0 {
		c.Overlays.EndText.OffsetFromEndSec = defaults.Overlays.EndText.OffsetFromEndSec
	}
	if c.Overlays.IndexBadge.Template == "" {
		c.Overlays.IndexBadge.Template = defaults.Overlays.IndexBadge.Template
	}
	if c.Overlays.IndexBadge.Persistent == nil {
		c.Overlays.IndexBadge.Persistent = boolPtr(true)
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
