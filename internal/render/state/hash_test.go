package state

import (
	"strings"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/pkg/csvplan"
)

func testConfig() config.Config {
	return config.Default()
}

func testSegment() render.Segment {
	return render.Segment{
		Clip: project.Clip{
			Sequence:        1,
			ClipType:        project.ClipTypeSong,
			TypeIndex:       1,
			SourceKind:      project.SourceKindPlan,
			DurationSeconds: 60,
			FadeInSeconds:   0.5,
			FadeOutSeconds:  0.5,
			Row: csvplan.Row{
				Index:           1,
				Title:           "Test Song",
				Artist:          "Test Artist",
				StartRaw:        "1:30",
				DurationSeconds: 60,
				Name:            "test",
				Link:            "https://example.com/video",
				CustomFields:    map[string]string{"genre": "rock"},
			},
		},
		Profile: project.ResolvedProfile{
			Name:         "song-main",
			DefaultStyle: config.TextStyle{FontColor: "white"},
		},
		Segments: []config.OverlaySegment{
			{
				Name:     "title",
				Template: "{title}",
			},
		},
		OutputPath: "/output/seg001.mp4",
	}
}

func TestGlobalConfigHashStable(t *testing.T) {
	cfg := testConfig()
	h1 := GlobalConfigHash(cfg)
	h2 := GlobalConfigHash(cfg)

	if h1 != h2 {
		t.Errorf("same config produced different hashes: %q vs %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", h1)
	}
}

func TestGlobalConfigHashChangesOnVideoChange(t *testing.T) {
	cfg1 := testConfig()
	cfg2 := testConfig()
	cfg2.Video.Width = 1280

	h1 := GlobalConfigHash(cfg1)
	h2 := GlobalConfigHash(cfg2)

	if h1 == h2 {
		t.Error("changing video width should produce different hash")
	}
}

func TestGlobalConfigHashChangesOnAudioChange(t *testing.T) {
	cfg1 := testConfig()
	cfg2 := testConfig()
	cfg2.Audio.SampleRate = 44100

	h1 := GlobalConfigHash(cfg1)
	h2 := GlobalConfigHash(cfg2)

	if h1 == h2 {
		t.Error("changing sample rate should produce different hash")
	}
}

func TestGlobalConfigHashChangesOnEncodingChange(t *testing.T) {
	cfg1 := testConfig()
	cfg2 := testConfig()
	cfg2.Encoding.VideoCodec = "libx265"

	h1 := GlobalConfigHash(cfg1)
	h2 := GlobalConfigHash(cfg2)

	if h1 == h2 {
		t.Error("changing video codec should produce different hash")
	}
}

func TestSegmentInputHashStable(t *testing.T) {
	seg := testSegment()
	h1 := SegmentInputHash(seg, "$INDEX_$TITLE")
	h2 := SegmentInputHash(seg, "$INDEX_$TITLE")

	if h1 != h2 {
		t.Errorf("same segment produced different hashes: %q vs %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", h1)
	}
}

func TestSegmentInputHashChangesOnStartTime(t *testing.T) {
	seg1 := testSegment()
	seg2 := testSegment()
	seg2.Clip.Row.StartRaw = "2:00"

	h1 := SegmentInputHash(seg1, "$INDEX")
	h2 := SegmentInputHash(seg2, "$INDEX")

	if h1 == h2 {
		t.Error("changing start time should produce different hash")
	}
}

func TestSegmentInputHashChangesOnOverlay(t *testing.T) {
	seg1 := testSegment()
	seg2 := testSegment()
	seg2.Segments = append(seg2.Segments, config.OverlaySegment{
		Name:     "artist",
		Template: "{artist}",
	})

	h1 := SegmentInputHash(seg1, "$INDEX")
	h2 := SegmentInputHash(seg2, "$INDEX")

	if h1 == h2 {
		t.Error("adding overlay segment should produce different hash")
	}
}

func TestSegmentInputHashChangesOnCustomField(t *testing.T) {
	seg1 := testSegment()
	seg2 := testSegment()
	seg2.Clip.Row.CustomFields = map[string]string{"genre": "jazz"}

	h1 := SegmentInputHash(seg1, "$INDEX")
	h2 := SegmentInputHash(seg2, "$INDEX")

	if h1 == h2 {
		t.Error("changing custom field value should produce different hash")
	}
}

func TestSegmentInputHashChangesOnTemplate(t *testing.T) {
	seg := testSegment()
	h1 := SegmentInputHash(seg, "$INDEX_$TITLE")
	h2 := SegmentInputHash(seg, "$INDEX_$ARTIST")

	if h1 == h2 {
		t.Error("different template should produce different hash")
	}
}
