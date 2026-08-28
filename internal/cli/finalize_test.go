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

// TestFinalizeDryRunSegmentStatus pins issue #40: `finalize --dry-run` must
// show per-entry status (✓ / stale / missing) for inline file entries,
// mirroring the hash-based drift detection `status` already shows for the
// timeline section.
func TestFinalizeDryRunSegmentStatus(t *testing.T) {
	root := t.TempDir()
	pp := paths.ProjectPaths{
		Root:            root,
		SegmentsDir:     filepath.Join(root, "segments"),
		RenderStateFile: filepath.Join(root, ".powerhour", "render-state.json"),
	}
	if err := os.MkdirAll(filepath.Join(pp.SegmentsDir, "__inline__"), 0o755); err != nil {
		t.Fatalf("mkdir segments dir: %v", err)
	}

	// Three inline file entries: one rendered with a matching hash, one
	// rendered but with a stale (mismatched) stored hash, and one whose
	// segment file was never rendered.
	rendered := filepath.Join(root, "rendered.mp4")
	stale := filepath.Join(root, "stale.mp4")
	missing := filepath.Join(root, "missing.mp4")
	for _, f := range []string{rendered, stale, missing} {
		if err := os.WriteFile(f, []byte("fake source"), 0o644); err != nil {
			t.Fatalf("write source file %s: %v", f, err)
		}
	}

	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{File: rendered},
				{File: stale},
				{File: missing},
			},
		},
	}

	// Compute the real hashes so a matching stored hash can be constructed.
	computedHashes := buildInlineHashes(pp, cfg, nil)

	renderedSegPath := findSegPathBySource(t, pp, cfg, 0)
	staleSegPath := findSegPathBySource(t, pp, cfg, 1)
	missingSegPath := findSegPathBySource(t, pp, cfg, 2)

	// Write output files for the rendered and stale segments, but not for
	// the missing one.
	if err := os.WriteFile(renderedSegPath, []byte("fake output"), 0o644); err != nil {
		t.Fatalf("write rendered segment: %v", err)
	}
	if err := os.WriteFile(staleSegPath, []byte("fake output"), 0o644); err != nil {
		t.Fatalf("write stale segment: %v", err)
	}

	rs := &state.RenderState{
		Segments: map[string]state.SegmentState{
			renderedSegPath: {InputHash: computedHashes[renderedSegPath].computed},
			staleSegPath:    {InputHash: "sha256:deadbeef-not-a-real-hash"},
		},
	}

	inlineHashes := buildInlineHashes(pp, cfg, rs)

	if got := segmentDryRunStatus(renderedSegPath, inlineHashes); got != segStatusOK {
		t.Errorf("rendered segment status = %q, want %q", got, segStatusOK)
	}
	if got := segmentDryRunStatus(staleSegPath, inlineHashes); got != segStatusStale {
		t.Errorf("stale segment status = %q, want %q", got, segStatusStale)
	}
	if got := segmentDryRunStatus(missingSegPath, inlineHashes); got != segStatusMissing {
		t.Errorf("missing segment status = %q, want %q", got, segStatusMissing)
	}
}

// findSegPathBySource resolves the inline segment output path for the
// sequence entry at seqIdx, exactly as buildInlineHashes does.
func findSegPathBySource(t *testing.T, pp paths.ProjectPaths, cfg config.Config, seqIdx int) string {
	t.Helper()
	entry := cfg.Timeline.Sequence[seqIdx]
	sourcePath := entry.File
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(pp.Root, sourcePath)
	}
	return render.InlineSegmentPath(pp.SegmentsDir, seqIdx, sourcePath)
}

// TestResolveTimelineSegmentsHonorsPlaybackOrderSwap is the issue's own
// regression test: before the seam swap in render.ResolveTimelineSegments,
// a playback.Swap had no effect on the resolved segment order because
// ResolveTimelineSegments resolved from config order (BuildTimelinePlacements)
// instead of the playback order.
func TestResolveTimelineSegmentsHonorsPlaybackOrderSwap(t *testing.T) {
	root := t.TempDir()
	pp := paths.ProjectPaths{
		Root:        root,
		SegmentsDir: filepath.Join(root, "segments"),
	}

	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{Collection: "songs"},
			},
		},
	}
	rows := []csvplan.CollectionRow{
		{Index: 1, RowID: "s1"},
		{Index: 2, RowID: "s2"},
		{Index: 3, RowID: "s3"},
	}
	collections := map[string]project.Collection{
		"songs": {
			Name:   "songs",
			Config: config.CollectionConfig{Selection: "once"},
			Rows:   rows,
		},
	}

	before, err := render.ResolveTimelineSegments(pp, cfg, collections)
	if err != nil {
		t.Fatalf("ResolveTimelineSegments (before): %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("segments = %d, want 3", len(before))
	}

	order, _, err := playback.ResolveOrder(root, cfg, collections)
	if err != nil {
		t.Fatalf("ResolveOrder: %v", err)
	}
	if err := playback.Swap(&order, 0, 2); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	if err := playback.Save(root, order); err != nil {
		t.Fatalf("Save: %v", err)
	}

	after, err := render.ResolveTimelineSegments(pp, cfg, collections)
	if err != nil {
		t.Fatalf("ResolveTimelineSegments (after): %v", err)
	}

	if before[0].Path == after[0].Path && before[2].Path == after[2].Path {
		t.Fatalf("swap did not change resolved segment order: before=%+v after=%+v", before, after)
	}
	if after[0].Path != before[2].Path || after[2].Path != before[0].Path {
		t.Fatalf("resolved order does not reflect the swap: before=%+v after=%+v", before, after)
	}
}
