package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	encodingProfileFile = "encoding_profile.json"
	encodingDefaultsFile = "encoding.yaml"
	encodingProfileTTL  = 24 * time.Hour
)

// CodecFamily groups related encoders by technology.
type CodecFamily struct {
	Name   string
	Codecs []string
}

// CodecFamilies lists all supported encoder families with candidates in priority order.
var CodecFamilies = []CodecFamily{
	{"H.264", []string{"h264_videotoolbox", "h264_nvenc", "h264_amf", "libx264"}},
	{"H.265 (HEVC)", []string{"hevc_videotoolbox", "hevc_nvenc", "hevc_amf", "libx265"}},
	{"VP9", []string{"libvpx-vp9"}},
	{"AV1", []string{"av1_nvenc", "av1_amf", "libsvtav1", "librav1e", "libaom-av1"}},
}

// EncodingDefaults is the comprehensive global encoding configuration. It covers
// every setting needed for both segment rendering and final output — the single
// source of truth stored at ~/.powerhour/encoding.yaml.
type EncodingDefaults struct {
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

// EncodingProfile is the cached result of encoder probing.
type EncodingProfile struct {
	SelectedCodec     string              `json:"selected_codec"`
	AvailableCodecs   []string            `json:"available_codecs"`
	AvailableByFamily map[string][]string `json:"available_by_family"`
	Hostname          string              `json:"hostname"`
	GOOS              string              `json:"goos"`
	ProbedAt          time.Time           `json:"probed_at"`
}

// AvailableAll returns all available codecs ordered by family priority.
func (p EncodingProfile) AvailableAll() []string {
	var all []string
	seen := map[string]bool{}
	for _, family := range CodecFamilies {
		for _, codec := range p.AvailableByFamily[family.Name] {
			if !seen[codec] {
				all = append(all, codec)
				seen[codec] = true
			}
		}
	}
	return all
}

func encodingProfilePath() (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, encodingProfileFile), nil
}

func encodingDefaultsPath() (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, encodingDefaultsFile), nil
}

func machineFingerprint() string {
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s/%s", runtime.GOOS, hostname)
}

// LoadEncodingProfile loads the cached encoding profile if valid.
// Returns nil if missing, expired, wrong machine, or uses old schema.
func LoadEncodingProfile() *EncodingProfile {
	path, err := encodingProfilePath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var profile EncodingProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil
	}
	if time.Since(profile.ProbedAt) > encodingProfileTTL {
		return nil
	}
	hostname, _ := os.Hostname()
	if profile.GOOS != runtime.GOOS || profile.Hostname != hostname {
		return nil
	}
	// Profile predates the multi-family schema — needs a fresh probe.
	if len(profile.AvailableByFamily) == 0 {
		return nil
	}
	return &profile
}

// SaveEncodingProfile persists the encoding profile to disk.
func SaveEncodingProfile(profile EncodingProfile) error {
	path, err := encodingProfilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare encoding profile dir: %w", err)
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal encoding profile: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// DeleteEncodingProfile removes the cached encoding profile.
func DeleteEncodingProfile() error {
	path, err := encodingProfilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ProbeEncoders discovers available encoders across all codec families.
func ProbeEncoders(ctx context.Context, ffmpegPath string) (EncodingProfile, error) {
	hostname, _ := os.Hostname()
	profile := EncodingProfile{
		Hostname:          hostname,
		GOOS:              runtime.GOOS,
		ProbedAt:          time.Now(),
		AvailableByFamily: make(map[string][]string),
	}

	for _, family := range CodecFamilies {
		for _, codec := range family.Codecs {
			if testEncoder(ctx, ffmpegPath, codec) {
				profile.AvailableCodecs = append(profile.AvailableCodecs, codec)
				profile.AvailableByFamily[family.Name] = append(profile.AvailableByFamily[family.Name], codec)
				if profile.SelectedCodec == "" && family.Name == "H.264" {
					profile.SelectedCodec = codec
				}
			}
		}
	}

	if profile.SelectedCodec == "" {
		if len(profile.AvailableCodecs) > 0 {
			profile.SelectedCodec = profile.AvailableCodecs[0]
		} else {
			profile.SelectedCodec = "libx264"
		}
	}

	return profile, nil
}

func testEncoder(ctx context.Context, ffmpegPath, codec string) bool {
	args := []string{
		"-f", "lavfi",
		"-i", "color=black:s=64x64:d=1:r=1",
		"-c:v", codec,
		"-frames:v", "1",
		"-f", "null",
		"-",
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	return cmd.Run() == nil
}

// LoadEncodingDefaults loads the global encoding.yaml. Returns zero-value if missing.
func LoadEncodingDefaults() EncodingDefaults {
	path, err := encodingDefaultsPath()
	if err != nil {
		return EncodingDefaults{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return EncodingDefaults{}
	}
	var defaults EncodingDefaults
	if err := yaml.Unmarshal(data, &defaults); err != nil {
		return EncodingDefaults{}
	}
	return defaults
}

// SaveEncodingDefaults writes the global encoding.yaml to disk.
func SaveEncodingDefaults(defaults EncodingDefaults) error {
	path, err := encodingDefaultsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare encoding defaults dir: %w", err)
	}
	data, err := yaml.Marshal(&defaults)
	if err != nil {
		return fmt.Errorf("marshal encoding defaults: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// ResolvedEncoding is the fully merged encoding config used by render and concat.
type ResolvedEncoding struct {
	// Video
	VideoCodec   string
	Width        int
	Height       int
	FPS          int
	CRF          int
	Preset       string
	VideoBitrate string
	Container    string

	// Audio
	AudioCodec   string
	AudioBitrate string
	SampleRate   int
	Channels     int

	// Loudnorm
	LoudnormEnabled  bool
	LoudnormLUFS     float64
	LoudnormTruePeak float64
	LoudnormLRA      float64
}

// ResolveEncoding merges project overrides > global defaults > built-in fallback.
// profile fills VideoCodec when nothing else sets it.
func ResolveEncoding(profile *EncodingProfile, global, project EncodingDefaults) ResolvedEncoding {
	boolTrue := true
	lufs := -14.0
	peak := -1.5
	lra := 11.0

	r := ResolvedEncoding{
		VideoCodec:       "libx264",
		Width:            1920,
		Height:           1080,
		FPS:              30,
		CRF:              20,
		Preset:           "fast",
		VideoBitrate:     "8M",
		Container:        "mp4",
		AudioCodec:       "aac",
		AudioBitrate:     "192k",
		SampleRate:       48000,
		Channels:         2,
		LoudnormEnabled:  true,
		LoudnormLUFS:     lufs,
		LoudnormTruePeak: peak,
		LoudnormLRA:      lra,
	}
	_ = boolTrue

	apply := func(d EncodingDefaults, fromProfile bool) {
		if d.VideoCodec != "" {
			r.VideoCodec = d.VideoCodec
		} else if fromProfile && profile != nil && profile.SelectedCodec != "" {
			r.VideoCodec = profile.SelectedCodec
		}
		if d.Width > 0 {
			r.Width = d.Width
		}
		if d.Height > 0 {
			r.Height = d.Height
		}
		if d.FPS > 0 {
			r.FPS = d.FPS
		}
		if d.CRF > 0 {
			r.CRF = d.CRF
		}
		if d.Preset != "" {
			r.Preset = d.Preset
		}
		if d.VideoBitrate != "" {
			r.VideoBitrate = d.VideoBitrate
		}
		if d.Container != "" {
			r.Container = d.Container
		}
		if d.AudioCodec != "" {
			r.AudioCodec = d.AudioCodec
		}
		if d.AudioBitrate != "" {
			r.AudioBitrate = d.AudioBitrate
		}
		if d.SampleRate > 0 {
			r.SampleRate = d.SampleRate
		}
		if d.Channels > 0 {
			r.Channels = d.Channels
		}
		if d.LoudnormEnabled != nil {
			r.LoudnormEnabled = *d.LoudnormEnabled
		}
		if d.LoudnormLUFS != nil {
			r.LoudnormLUFS = *d.LoudnormLUFS
		}
		if d.LoudnormTruePeak != nil {
			r.LoudnormTruePeak = *d.LoudnormTruePeak
		}
		if d.LoudnormLRA != nil {
			r.LoudnormLRA = *d.LoudnormLRA
		}
	}

	apply(global, true)
	apply(project, false)
	return r
}
