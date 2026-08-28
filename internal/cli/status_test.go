package cli

import (
	"os"
	"path/filepath"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/playback"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/state"
	"powerhour/pkg/csvplan"
)

// TestBuildRowStatusesHonorsSequenceEntryFadeOverride pins issue #166: status
// must hash rows with the same fade-in/fade-out seconds the dashboard uses —
// including a timeline sequence-entry fade override — so both surfaces agree
// on whether a row is stale or rendered.
func TestBuildRowStatusesHonorsSequenceEntryFadeOverride(t *testing.T) {
	root := t.TempDir()
	pp := paths.ProjectPaths{
		Root:            root,
		SegmentsDir:     filepath.Join(root, "segments"),
		RenderStateFile: filepath.Join(root, ".powerhour", "render-state.json"),
	}
	if err := os.MkdirAll(pp.SegmentsDir, 0o755); err != nil {
		t.Fatalf("mkdir segments dir: %v", err)
	}

	// A local source file the row links to, so cache status resolves to "cached".
	sourceFile := filepath.Join(root, "source.mp4")
	if err := os.WriteFile(sourceFile, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	cfg := config.Config{
		Collections: map[string]config.CollectionConfig{
			"songs": {Fade: 2.0}, // base: fade_in=1.0, fade_out=1.0
		},
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{Collection: "songs", Fade: 4.0}, // override: fade_in=2.0, fade_out=2.0
			},
		},
	}

	row := csvplan.CollectionRow{
		Index:        1,
		Link:         sourceFile,
		CustomFields: map[string]string{"title": "Test Song"},
	}
	coll := project.Collection{Name: "songs", Rows: []csvplan.CollectionRow{row}}
	collections := map[string]project.Collection{"songs": coll}

	tmpl := cfg.SegmentFilenameTemplate()

	// Build the segment's output path the same way buildRowStatuses does, so
	// the test can pre-populate render state and touch an existing output.
	clip := project.Clip{
		Sequence:        row.Index,
		ClipType:        project.ClipType("songs"),
		TypeIndex:       row.Index,
		Row:             row.ToRow(),
		SourceKind:      project.SourceKindPlan,
		DurationSeconds: row.DurationSeconds,
	}
	if clip.Row.Index <= 0 {
		clip.Row.Index = clip.TypeIndex
	}
	baseSeg := render.Segment{Clip: clip}
	outputPath := filepath.Join(pp.SegmentsDir, render.SegmentBaseName(tmpl, baseSeg)+".mp4")
	if err := os.WriteFile(outputPath, []byte("fake output"), 0o644); err != nil {
		t.Fatalf("write output file: %v", err)
	}

	// Hash including the sequence-entry fade override — this is what the
	// dashboard computes and what the real render job actually applied.
	overrideClip := clip
	overrideClip.FadeInSeconds = 2.0
	overrideClip.FadeOutSeconds = 2.0
	overrideSeg := render.Segment{Clip: overrideClip, OutputPath: outputPath}
	overrideHash := state.SegmentInputHash(overrideSeg, tmpl)

	// Hash computed from collection-level fades only, ignoring the override —
	// this is the pre-fix status.go behavior.
	baseOnlyClip := clip
	baseOnlyClip.FadeInSeconds = 1.0
	baseOnlyClip.FadeOutSeconds = 1.0
	baseOnlySeg := render.Segment{Clip: baseOnlyClip, OutputPath: outputPath}
	baseOnlyHash := state.SegmentInputHash(baseOnlySeg, tmpl)

	if overrideHash == baseOnlyHash {
		t.Fatalf("expected override hash and base-only hash to differ, both = %q", overrideHash)
	}

	t.Run("stored hash matches override hash -> rendered", func(t *testing.T) {
		rs := &state.RenderState{
			GlobalConfigHash: state.GlobalConfigHash(cfg),
			Segments: map[string]state.SegmentState{
				state.SegmentKey(overrideSeg): {InputHash: overrideHash},
			},
		}

		rows, _ := buildRowStatuses(pp, cfg, nil, rs, collections, playback.PositionIndex{}, tmpl)
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		if rows[0].RenderStatus != "rendered" {
			t.Errorf("RenderStatus = %q, want %q (status must hash with the sequence-entry fade override applied, matching the dashboard)", rows[0].RenderStatus, "rendered")
		}
	})

	t.Run("stored hash matches base-only hash (no override) -> stale", func(t *testing.T) {
		rs := &state.RenderState{
			GlobalConfigHash: state.GlobalConfigHash(cfg),
			Segments: map[string]state.SegmentState{
				state.SegmentKey(baseOnlySeg): {InputHash: baseOnlyHash},
			},
		}

		rows, _ := buildRowStatuses(pp, cfg, nil, rs, collections, playback.PositionIndex{}, tmpl)
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		if rows[0].RenderStatus != "stale" {
			t.Errorf("RenderStatus = %q, want %q (fade override changed the effective fade, so the stored base-only hash is stale)", rows[0].RenderStatus, "stale")
		}
		if rows[0].RenderReason != "input changed" {
			t.Errorf("RenderReason = %q, want %q", rows[0].RenderReason, "input changed")
		}
	})
}
