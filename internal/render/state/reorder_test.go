package state

import (
	"os"
	"path/filepath"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/pkg/csvplan"
)

const reorderTemplate = "$INDEX_PAD3_$SAFE_TITLE"

// reorderSegment builds a segment the way the render path does: its output
// path comes from the filename template, so moving the clip in the playback
// order moves its file.
func reorderSegment(t *testing.T, dir, collection, rowID, title string, position int, overlays []config.OverlayEntry) render.Segment {
	t.Helper()

	seg := render.Segment{
		Clip: project.Clip{
			Sequence:         position,
			PlaybackPosition: position,
			ClipType:         project.ClipType(collection),
			TypeIndex:        position,
			SourceKind:       project.SourceKindPlan,
			DurationSeconds:  60,
			Row: csvplan.Row{
				Index:           position,
				RowID:           rowID,
				Title:           title,
				StartRaw:        "0:30",
				DurationSeconds: 60,
				Link:            "https://example.com/" + rowID,
			},
		},
		Overlays: overlays,
	}
	seg.OutputPath = filepath.Join(dir, render.SegmentBaseName(reorderTemplate, seg)+".mp4")
	return seg
}

func songOverlays() []config.OverlayEntry {
	return []config.OverlayEntry{{Type: "song-info"}}
}

func interstitialOverlays() []config.OverlayEntry {
	return []config.OverlayEntry{{Type: "drink"}}
}

// renderState seeds a state file describing segments that were rendered at
// the positions they currently hold.
func renderState(t *testing.T, cfg config.Config, segs ...render.Segment) *RenderState {
	t.Helper()
	rs := &RenderState{
		GlobalConfigHash: GlobalConfigHash(cfg),
		Segments:         map[string]SegmentState{},
	}
	for _, seg := range segs {
		rs.Segments[SegmentKey(seg)] = SegmentState{
			InputHash:  SegmentInputHash(seg, reorderTemplate),
			OutputPath: seg.OutputPath,
		}
		if err := os.WriteFile(seg.OutputPath, []byte("rendered"), 0o644); err != nil {
			t.Fatalf("write %s: %v", seg.OutputPath, err)
		}
	}
	return rs
}

func countActions(actions []SegmentAction, action string) int {
	n := 0
	for _, a := range actions {
		if a.Action == action {
			n++
		}
	}
	return n
}

// TestSwapSongsRerenders is the positive half of the pair ADR 0001 asks for:
// two clips whose overlay burns in the index swap places, so both have to be
// re-encoded.
func TestSwapSongsRerenders(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	before := []render.Segment{
		reorderSegment(t, dir, "songs", "aaa111", "Song A", 1, songOverlays()),
		reorderSegment(t, dir, "songs", "bbb222", "Song B", 2, songOverlays()),
	}
	rs := renderState(t, cfg, before...)

	after := []render.Segment{
		reorderSegment(t, dir, "songs", "aaa111", "Song A", 2, songOverlays()),
		reorderSegment(t, dir, "songs", "bbb222", "Song B", 1, songOverlays()),
	}

	actions := DetectChanges(rs, after, cfg, reorderTemplate, false)

	if got := countActions(actions, ActionRender); got != 2 {
		t.Fatalf("renders after swapping two songs = %d, want 2 (the burned-in badge changed for both)", got)
	}
	for _, a := range actions {
		if a.Reason != ReasonInputChanged {
			t.Errorf("reason = %q, want %q", a.Reason, ReasonInputChanged)
		}
	}
}

// TestSwapInterstitialsRenamesWithoutRendering is the negative half: two
// clips whose overlay renders no number swap places, so nothing changed about
// their pixels and neither may be re-encoded.
func TestSwapInterstitialsRenamesWithoutRendering(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	before := []render.Segment{
		reorderSegment(t, dir, "interstitials", "ccc333", "Drink A", 1, interstitialOverlays()),
		reorderSegment(t, dir, "interstitials", "ddd444", "Drink B", 2, interstitialOverlays()),
	}
	rs := renderState(t, cfg, before...)

	after := []render.Segment{
		reorderSegment(t, dir, "interstitials", "ccc333", "Drink A", 2, interstitialOverlays()),
		reorderSegment(t, dir, "interstitials", "ddd444", "Drink B", 1, interstitialOverlays()),
	}
	if after[0].OutputPath == before[0].OutputPath {
		t.Fatalf("test is not exercising a move: output path unchanged (%s)", after[0].OutputPath)
	}

	actions := DetectChanges(rs, after, cfg, reorderTemplate, false)

	if got := countActions(actions, ActionRender); got != 0 {
		t.Fatalf("renders after swapping two interstitials = %d, want 0 (drink renders no number)", got)
	}
	if got := countActions(actions, ActionRename); got != 2 {
		t.Fatalf("renames = %d, want 2", got)
	}

	ApplyRenames(actions)

	// The swap is a cycle: each clip wants the filename the other held. Both
	// files must survive it with the right contents in the right place.
	for i, seg := range after {
		if _, err := os.Stat(seg.OutputPath); err != nil {
			t.Errorf("segment %d: expected file at %s after rename: %v", i, seg.OutputPath, err)
		}
		if actions[i].Action != ActionRename {
			t.Errorf("segment %d: action became %q after ApplyRenames, want the rename to succeed", i, actions[i].Action)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("files in segment dir after rename = %v, want exactly the 2 segments", names)
	}
}

// A rename with nothing at the prior path is not a rename — it is a render.
func TestRenameFallsBackToRenderWhenPriorFileGone(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	before := reorderSegment(t, dir, "interstitials", "ccc333", "Drink A", 1, interstitialOverlays())
	rs := renderState(t, cfg, before)
	if err := os.Remove(before.OutputPath); err != nil {
		t.Fatal(err)
	}

	after := reorderSegment(t, dir, "interstitials", "ccc333", "Drink A", 2, interstitialOverlays())
	actions := DetectChanges(rs, []render.Segment{after}, cfg, reorderTemplate, false)

	if actions[0].Action != ActionRender {
		t.Fatalf("action = %q, want %q", actions[0].Action, ActionRender)
	}
	if actions[0].Reason != ReasonOutputMissing {
		t.Errorf("reason = %q, want %q", actions[0].Reason, ReasonOutputMissing)
	}
}

// Prune keys on row identity, so an entry whose file was renamed is still
// current and must survive.
func TestPruneKeepsRenamedEntry(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	before := reorderSegment(t, dir, "interstitials", "ccc333", "Drink A", 1, interstitialOverlays())
	rs := renderState(t, cfg, before)

	after := reorderSegment(t, dir, "interstitials", "ccc333", "Drink A", 2, interstitialOverlays())
	Prune(rs, map[string]bool{SegmentKey(after): true})

	if _, ok := rs.Segments[SegmentKey(after)]; !ok {
		t.Fatal("renamed segment's state entry was pruned; its key is a row identity and did not change")
	}
}

// An inline file: slot has no row id, so it keys on its output path — and
// that path never moves, because a file slot never participates in a swap.
func TestSegmentKeyFallsBackToPathWithoutRowID(t *testing.T) {
	seg := render.Segment{OutputPath: "/segments/__inline__/0-intro.mp4"}
	if got, want := SegmentKey(seg), "path:/segments/__inline__/0-intro.mp4"; got != want {
		t.Errorf("SegmentKey = %q, want %q", got, want)
	}
}

// A state file written before the rekey converts in place rather than
// reporting every segment as new.
func TestMigrateKeysConvertsPathKeyedEntries(t *testing.T) {
	dir := t.TempDir()
	seg := reorderSegment(t, dir, "songs", "aaa111", "Song A", 1, songOverlays())

	rs := &RenderState{Segments: map[string]SegmentState{
		seg.OutputPath: {InputHash: "sha256:carried-across", SourcePath: "/cache/a.mp4"},
	}}
	MigrateKeys(rs, []render.Segment{seg})

	if _, ok := rs.Segments[seg.OutputPath]; ok {
		t.Error("old path-shaped key survived migration")
	}
	got, ok := rs.Segments[SegmentKey(seg)]
	if !ok {
		t.Fatal("entry was not migrated to the row key")
	}
	if got.InputHash != "sha256:carried-across" {
		t.Errorf("InputHash = %q, want the hash to be carried across", got.InputHash)
	}
	if got.OutputPath != seg.OutputPath {
		t.Errorf("OutputPath = %q, want %q so the next move has a rename source", got.OutputPath, seg.OutputPath)
	}
}

// The upgrade case that actually matters: the rekey itself renames every
// segment whose filename embeds its position, so the old entry's key no
// longer matches any current output path. It has to be found by source path
// and must record where the file really is, or the very first run after the
// upgrade re-encodes the whole project.
func TestMigrateKeysJoinsOnSourcePathWhenTheFilenameChanged(t *testing.T) {
	dir := t.TempDir()

	oldSeg := reorderSegment(t, dir, "songs", "aaa111", "Song A", 1, songOverlays())
	newSeg := reorderSegment(t, dir, "songs", "aaa111", "Song A", 9, songOverlays())
	newSeg.CachedPath = "/cache/a.mp4"
	if oldSeg.OutputPath == newSeg.OutputPath {
		t.Fatal("test is not exercising a filename change")
	}

	rs := &RenderState{Segments: map[string]SegmentState{
		oldSeg.OutputPath: {InputHash: "sha256:carried-across", SourcePath: "/cache/a.mp4"},
	}}
	MigrateKeys(rs, []render.Segment{newSeg})

	got, ok := rs.Segments[SegmentKey(newSeg)]
	if !ok {
		t.Fatal("entry was not migrated")
	}
	if got.OutputPath != oldSeg.OutputPath {
		t.Errorf("OutputPath = %q, want the old path %q where the file actually is", got.OutputPath, oldSeg.OutputPath)
	}
}

// Two rows cut from one source file cannot be told apart by source path, so
// neither is migrated rather than one being given the other's hash.
func TestMigrateKeysSkipsAmbiguousSourceMatches(t *testing.T) {
	dir := t.TempDir()

	a := reorderSegment(t, dir, "songs", "aaa111", "Song A", 9, songOverlays())
	a.CachedPath = "/cache/shared.mp4"
	b := reorderSegment(t, dir, "songs", "bbb222", "Song B", 10, songOverlays())
	b.CachedPath = "/cache/shared.mp4"

	rs := &RenderState{Segments: map[string]SegmentState{
		filepath.Join(dir, "old-a.mp4"): {InputHash: "sha256:a", SourcePath: "/cache/shared.mp4"},
		filepath.Join(dir, "old-b.mp4"): {InputHash: "sha256:b", SourcePath: "/cache/shared.mp4"},
	}}
	MigrateKeys(rs, []render.Segment{a, b})

	if _, ok := rs.Segments[SegmentKey(a)]; ok {
		t.Error("an ambiguous source match was migrated; it could have carried the wrong hash")
	}
	if len(rs.Segments) != 2 {
		t.Errorf("state size = %d, want the 2 legacy entries left untouched", len(rs.Segments))
	}
}
