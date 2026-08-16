package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// TestSaveGlobalConfigPermissions asserts that SaveGlobalConfig writes
// ~/.powerhour/config.yaml owner-only (0o600) under an owner-only dir
// (0o700), even when the dir and file already exist with looser
// permissions from a prior install.
func TestSaveGlobalConfigPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file permissions do not apply on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".powerhour")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("pre-create dir: %v", err)
	}
	path := filepath.Join(dir, globalConfigFile)
	if err := os.WriteFile(path, []byte("downloads:\n  proxy: \"\"\n"), 0o644); err != nil {
		t.Fatalf("pre-create file: %v", err)
	}

	if err := SaveGlobalConfig(GlobalConfig{}); err != nil {
		t.Fatalf("SaveGlobalConfig: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("expected dir perm 0700, got %#o", perm)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected file perm 0600, got %#o", perm)
	}
}

// TestResolveEncodingPrecedence asserts, field by field, that the merge
// order is: project overrides > global defaults > built-in fallback, and
// that zero/empty values in project or global do NOT override a set value
// beneath them.
func TestResolveEncodingPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		global  EncodingDefaults
		project EncodingDefaults
		get     func(ResolvedEncoding) any
		want    any
	}{
		// VideoCodec
		{"video codec: global only wins over built-in", EncodingDefaults{VideoCodec: "libx265"}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.VideoCodec }, "libx265"},
		{"video codec: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{VideoCodec: "libx265"}, func(r ResolvedEncoding) any { return r.VideoCodec }, "libx265"},
		{"video codec: project wins over global", EncodingDefaults{VideoCodec: "libx265"}, EncodingDefaults{VideoCodec: "libvpx-vp9"}, func(r ResolvedEncoding) any { return r.VideoCodec }, "libvpx-vp9"},
		{"video codec: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.VideoCodec }, "libx264"},

		// Width
		{"width: global only wins over built-in", EncodingDefaults{Width: 1280}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Width }, 1280},
		{"width: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{Width: 1280}, func(r ResolvedEncoding) any { return r.Width }, 1280},
		{"width: project wins over global", EncodingDefaults{Width: 1280}, EncodingDefaults{Width: 3840}, func(r ResolvedEncoding) any { return r.Width }, 3840},
		{"width: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Width }, 1920},
		{"width: project 0 does not override global", EncodingDefaults{Width: 1280}, EncodingDefaults{Width: 0}, func(r ResolvedEncoding) any { return r.Width }, 1280},

		// Height
		{"height: global only wins over built-in", EncodingDefaults{Height: 720}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Height }, 720},
		{"height: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{Height: 720}, func(r ResolvedEncoding) any { return r.Height }, 720},
		{"height: project wins over global", EncodingDefaults{Height: 720}, EncodingDefaults{Height: 2160}, func(r ResolvedEncoding) any { return r.Height }, 2160},
		{"height: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Height }, 1080},
		{"height: project 0 does not override global", EncodingDefaults{Height: 720}, EncodingDefaults{Height: 0}, func(r ResolvedEncoding) any { return r.Height }, 720},

		// FPS
		{"fps: global only wins over built-in", EncodingDefaults{FPS: 24}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.FPS }, 24},
		{"fps: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{FPS: 24}, func(r ResolvedEncoding) any { return r.FPS }, 24},
		{"fps: project wins over global", EncodingDefaults{FPS: 24}, EncodingDefaults{FPS: 60}, func(r ResolvedEncoding) any { return r.FPS }, 60},
		{"fps: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.FPS }, 30},
		{"fps: project 0 does not override global", EncodingDefaults{FPS: 24}, EncodingDefaults{FPS: 0}, func(r ResolvedEncoding) any { return r.FPS }, 24},

		// CRF
		{"crf: global only wins over built-in", EncodingDefaults{CRF: 18}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.CRF }, 18},
		{"crf: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{CRF: 18}, func(r ResolvedEncoding) any { return r.CRF }, 18},
		{"crf: project wins over global", EncodingDefaults{CRF: 18}, EncodingDefaults{CRF: 28}, func(r ResolvedEncoding) any { return r.CRF }, 28},
		{"crf: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.CRF }, 20},
		{"crf: project 0 does not override global", EncodingDefaults{CRF: 18}, EncodingDefaults{CRF: 0}, func(r ResolvedEncoding) any { return r.CRF }, 18},

		// Preset
		{"preset: global only wins over built-in", EncodingDefaults{Preset: "slow"}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Preset }, "slow"},
		{"preset: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{Preset: "slow"}, func(r ResolvedEncoding) any { return r.Preset }, "slow"},
		{"preset: project wins over global", EncodingDefaults{Preset: "slow"}, EncodingDefaults{Preset: "veryfast"}, func(r ResolvedEncoding) any { return r.Preset }, "veryfast"},
		{"preset: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Preset }, "fast"},

		// VideoBitrate
		{"video bitrate: global only wins over built-in", EncodingDefaults{VideoBitrate: "4M"}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.VideoBitrate }, "4M"},
		{"video bitrate: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{VideoBitrate: "4M"}, func(r ResolvedEncoding) any { return r.VideoBitrate }, "4M"},
		{"video bitrate: project wins over global", EncodingDefaults{VideoBitrate: "4M"}, EncodingDefaults{VideoBitrate: "12M"}, func(r ResolvedEncoding) any { return r.VideoBitrate }, "12M"},
		{"video bitrate: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.VideoBitrate }, "8M"},

		// Container
		{"container: global only wins over built-in", EncodingDefaults{Container: "mkv"}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Container }, "mkv"},
		{"container: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{Container: "mkv"}, func(r ResolvedEncoding) any { return r.Container }, "mkv"},
		{"container: project wins over global", EncodingDefaults{Container: "mkv"}, EncodingDefaults{Container: "mov"}, func(r ResolvedEncoding) any { return r.Container }, "mov"},
		{"container: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Container }, "mp4"},

		// AudioCodec
		{"audio codec: global only wins over built-in", EncodingDefaults{AudioCodec: "opus"}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.AudioCodec }, "opus"},
		{"audio codec: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{AudioCodec: "opus"}, func(r ResolvedEncoding) any { return r.AudioCodec }, "opus"},
		{"audio codec: project wins over global", EncodingDefaults{AudioCodec: "opus"}, EncodingDefaults{AudioCodec: "flac"}, func(r ResolvedEncoding) any { return r.AudioCodec }, "flac"},
		{"audio codec: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.AudioCodec }, "aac"},

		// AudioBitrate
		{"audio bitrate: global only wins over built-in", EncodingDefaults{AudioBitrate: "128k"}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.AudioBitrate }, "128k"},
		{"audio bitrate: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{AudioBitrate: "128k"}, func(r ResolvedEncoding) any { return r.AudioBitrate }, "128k"},
		{"audio bitrate: project wins over global", EncodingDefaults{AudioBitrate: "128k"}, EncodingDefaults{AudioBitrate: "320k"}, func(r ResolvedEncoding) any { return r.AudioBitrate }, "320k"},
		{"audio bitrate: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.AudioBitrate }, "192k"},

		// SampleRate
		{"sample rate: global only wins over built-in", EncodingDefaults{SampleRate: 44100}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.SampleRate }, 44100},
		{"sample rate: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{SampleRate: 44100}, func(r ResolvedEncoding) any { return r.SampleRate }, 44100},
		{"sample rate: project wins over global", EncodingDefaults{SampleRate: 44100}, EncodingDefaults{SampleRate: 96000}, func(r ResolvedEncoding) any { return r.SampleRate }, 96000},
		{"sample rate: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.SampleRate }, 48000},
		{"sample rate: project 0 does not override global", EncodingDefaults{SampleRate: 44100}, EncodingDefaults{SampleRate: 0}, func(r ResolvedEncoding) any { return r.SampleRate }, 44100},

		// Channels
		{"channels: global only wins over built-in", EncodingDefaults{Channels: 1}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Channels }, 1},
		{"channels: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{Channels: 1}, func(r ResolvedEncoding) any { return r.Channels }, 1},
		{"channels: project wins over global", EncodingDefaults{Channels: 1}, EncodingDefaults{Channels: 6}, func(r ResolvedEncoding) any { return r.Channels }, 6},
		{"channels: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.Channels }, 2},
		{"channels: project 0 does not override global", EncodingDefaults{Channels: 1}, EncodingDefaults{Channels: 0}, func(r ResolvedEncoding) any { return r.Channels }, 1},

		// LoudnormEnabled (pointer, must distinguish "explicitly false" from "unset")
		{"loudnorm enabled: global only wins over built-in", EncodingDefaults{LoudnormEnabled: ptr(false)}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.LoudnormEnabled }, false},
		{"loudnorm enabled: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{LoudnormEnabled: ptr(false)}, func(r ResolvedEncoding) any { return r.LoudnormEnabled }, false},
		{"loudnorm enabled: project explicit false wins over global true", EncodingDefaults{LoudnormEnabled: ptr(true)}, EncodingDefaults{LoudnormEnabled: ptr(false)}, func(r ResolvedEncoding) any { return r.LoudnormEnabled }, false},
		{"loudnorm enabled: neither set falls back to built-in true", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.LoudnormEnabled }, true},

		// LoudnormLUFS
		{"loudnorm lufs: global only wins over built-in", EncodingDefaults{LoudnormLUFS: ptr(-16.0)}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.LoudnormLUFS }, -16.0},
		{"loudnorm lufs: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{LoudnormLUFS: ptr(-16.0)}, func(r ResolvedEncoding) any { return r.LoudnormLUFS }, -16.0},
		{"loudnorm lufs: project explicit 0 wins over global", EncodingDefaults{LoudnormLUFS: ptr(-16.0)}, EncodingDefaults{LoudnormLUFS: ptr(0.0)}, func(r ResolvedEncoding) any { return r.LoudnormLUFS }, 0.0},
		{"loudnorm lufs: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.LoudnormLUFS }, -14.0},

		// LoudnormTruePeak
		{"loudnorm true peak: global only wins over built-in", EncodingDefaults{LoudnormTruePeak: ptr(-2.0)}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.LoudnormTruePeak }, -2.0},
		{"loudnorm true peak: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{LoudnormTruePeak: ptr(-2.0)}, func(r ResolvedEncoding) any { return r.LoudnormTruePeak }, -2.0},
		{"loudnorm true peak: project wins over global", EncodingDefaults{LoudnormTruePeak: ptr(-2.0)}, EncodingDefaults{LoudnormTruePeak: ptr(-1.0)}, func(r ResolvedEncoding) any { return r.LoudnormTruePeak }, -1.0},
		{"loudnorm true peak: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.LoudnormTruePeak }, -1.5},

		// LoudnormLRA
		{"loudnorm lra: global only wins over built-in", EncodingDefaults{LoudnormLRA: ptr(7.0)}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.LoudnormLRA }, 7.0},
		{"loudnorm lra: project only wins over built-in", EncodingDefaults{}, EncodingDefaults{LoudnormLRA: ptr(7.0)}, func(r ResolvedEncoding) any { return r.LoudnormLRA }, 7.0},
		{"loudnorm lra: project wins over global", EncodingDefaults{LoudnormLRA: ptr(7.0)}, EncodingDefaults{LoudnormLRA: ptr(15.0)}, func(r ResolvedEncoding) any { return r.LoudnormLRA }, 15.0},
		{"loudnorm lra: neither set falls back to built-in", EncodingDefaults{}, EncodingDefaults{}, func(r ResolvedEncoding) any { return r.LoudnormLRA }, 11.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.get(ResolveEncoding(nil, tt.global, tt.project))
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolveEncodingDefaults asserts the whole built-in fallback struct
// when neither global nor project set anything, covering every field at once.
func TestResolveEncodingDefaults(t *testing.T) {
	got := ResolveEncoding(nil, EncodingDefaults{}, EncodingDefaults{})
	want := ResolvedEncoding{
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
		LoudnormLUFS:     -14.0,
		LoudnormTruePeak: -1.5,
		LoudnormLRA:      11.0,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestResolveEncodingProfileCodec covers the profile-fills-VideoCodec branch,
// which is only reachable via apply(global, true) — the profile is never
// consulted on the project pass.
func TestResolveEncodingProfileCodec(t *testing.T) {
	profile := &EncodingProfile{SelectedCodec: "hevc_videotoolbox"}

	tests := []struct {
		name    string
		profile *EncodingProfile
		global  EncodingDefaults
		project EncodingDefaults
		want    string
	}{
		{"profile fills codec when neither config sets it", profile, EncodingDefaults{}, EncodingDefaults{}, "hevc_videotoolbox"},
		{"global video_codec beats profile", profile, EncodingDefaults{VideoCodec: "libvpx-vp9"}, EncodingDefaults{}, "libvpx-vp9"},
		{"project video_codec beats profile", profile, EncodingDefaults{}, EncodingDefaults{VideoCodec: "libaom-av1"}, "libaom-av1"},
		{"project video_codec beats global and profile", profile, EncodingDefaults{VideoCodec: "libvpx-vp9"}, EncodingDefaults{VideoCodec: "libaom-av1"}, "libaom-av1"},
		{"nil profile falls back to built-in", nil, EncodingDefaults{}, EncodingDefaults{}, "libx264"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveEncoding(tt.profile, tt.global, tt.project)
			if got.VideoCodec != tt.want {
				t.Errorf("got VideoCodec %q, want %q", got.VideoCodec, tt.want)
			}
		})
	}
}
