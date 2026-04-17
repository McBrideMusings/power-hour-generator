package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "powerhour.yaml")

	cfg := Config{
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
		},
		Collections: map[string]CollectionConfig{
			"songs": {
				Plan:      "songs.yaml",
				OutputDir: "songs",
			},
		},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{File: "intro.mp4", Fade: 1.0},
				{Collection: "songs", Slice: "start:10"},
				{File: "outro.mp4"},
			},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Timeline.Sequence) != 3 {
		t.Fatalf("sequence length = %d, want 3", len(loaded.Timeline.Sequence))
	}

	if loaded.Timeline.Sequence[0].File != "intro.mp4" {
		t.Errorf("seq[0].File = %q, want %q", loaded.Timeline.Sequence[0].File, "intro.mp4")
	}
	if loaded.Timeline.Sequence[1].Collection != "songs" {
		t.Errorf("seq[1].Collection = %q, want %q", loaded.Timeline.Sequence[1].Collection, "songs")
	}
	if loaded.Timeline.Sequence[1].Slice != "start:10" {
		t.Errorf("seq[1].Slice = %q, want start:10", loaded.Timeline.Sequence[1].Slice)
	}
	if loaded.Video.Width != 1920 {
		t.Errorf("video.Width = %d, want 1920", loaded.Video.Width)
	}
}

func TestSave_ModifyTimeline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "powerhour.yaml")

	cfg := Config{
		Version: 1,
		Collections: map[string]CollectionConfig{
			"songs": {Plan: "songs.yaml", OutputDir: "songs"},
		},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{Collection: "songs", Slice: "start:5"},
				{Collection: "songs", Slice: "start:5"},
			},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Modify: swap and add.
	cfg.Timeline.Sequence[0], cfg.Timeline.Sequence[1] = cfg.Timeline.Sequence[1], cfg.Timeline.Sequence[0]
	cfg.Timeline.Sequence = append(cfg.Timeline.Sequence, SequenceEntry{File: "outro.mp4"})

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save after modify: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Timeline.Sequence) != 3 {
		t.Fatalf("sequence length = %d, want 3", len(loaded.Timeline.Sequence))
	}
	if loaded.Timeline.Sequence[2].File != "outro.mp4" {
		t.Errorf("seq[2].File = %q, want %q", loaded.Timeline.Sequence[2].File, "outro.mp4")
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "powerhour.yaml")

	cfg := Config{Version: 1}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File should exist.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// No temp files should remain.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "powerhour.yaml" {
			t.Errorf("unexpected file: %s", e.Name())
		}
	}
}
