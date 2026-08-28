package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/pkg/csvplan"
)

// segForPosition builds a segment whose OutputPath embeds pos through the
// default $INDEX_PAD3_$SAFE_TITLE template.
func segForPosition(t *testing.T, dir string, pos int) render.Segment {
	t.Helper()
	seg := render.Segment{Clip: project.Clip{
		PlaybackPosition: pos,
		TypeIndex:        1,
		Row:              csvplan.Row{Title: "Song A", Index: 1},
	}}
	seg.OutputPath = filepath.Join(dir, render.SegmentBaseName("$INDEX_PAD3_$SAFE_TITLE", seg)+".mp4")
	return seg
}

// TestSegmentScannerExactMatch verifies a segment rendered at the row's
// current position reports as correctly numbered.
func TestSegmentScannerExactMatch(t *testing.T) {
	dir := t.TempDir()
	seg := segForPosition(t, dir, 7)
	if err := os.WriteFile(seg.OutputPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	path, numbered := newSegmentScanner().find("$INDEX_PAD3_$SAFE_TITLE", seg)
	if path != seg.OutputPath || !numbered {
		t.Fatalf("find = (%q, %v), want (%q, true)", path, numbered, seg.OutputPath)
	}
}

// TestSegmentScannerFindsOlderPosition is the middle state: the row moved in
// the playback order, so the segment on disk carries the old number. It must
// still be found, and reported as mis-numbered rather than correct.
func TestSegmentScannerFindsOlderPosition(t *testing.T) {
	dir := t.TempDir()
	old := segForPosition(t, dir, 3)
	if err := os.WriteFile(old.OutputPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	moved := segForPosition(t, dir, 42)
	if moved.OutputPath == old.OutputPath {
		t.Fatal("setup: the two positions produced the same filename")
	}

	path, numbered := newSegmentScanner().find("$INDEX_PAD3_$SAFE_TITLE", moved)
	if path != old.OutputPath {
		t.Fatalf("find path = %q, want the segment rendered at the old position %q", path, old.OutputPath)
	}
	if numbered {
		t.Fatal("find numbered = true, want false — the burned-in number is stale")
	}
}

// TestSegmentScannerNoRenderAtAll verifies an empty output dir yields no path,
// so the caller falls back to the raw source.
func TestSegmentScannerNoRenderAtAll(t *testing.T) {
	dir := t.TempDir()
	path, numbered := newSegmentScanner().find("$INDEX_PAD3_$SAFE_TITLE", segForPosition(t, dir, 1))
	if path != "" || numbered {
		t.Fatalf("find = (%q, %v), want (\"\", false)", path, numbered)
	}
}

// TestSegmentScannerIgnoresOtherRows verifies the prefix/suffix match is row-
// specific: another song's segment must not be mistaken for this row's.
func TestSegmentScannerIgnoresOtherRows(t *testing.T) {
	dir := t.TempDir()
	other := render.Segment{Clip: project.Clip{
		PlaybackPosition: 3,
		TypeIndex:        2,
		Row:              csvplan.Row{Title: "Song B", Index: 2},
	}}
	other.OutputPath = filepath.Join(dir, render.SegmentBaseName("$INDEX_PAD3_$SAFE_TITLE", other)+".mp4")
	if err := os.WriteFile(other.OutputPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	path, _ := newSegmentScanner().find("$INDEX_PAD3_$SAFE_TITLE", segForPosition(t, dir, 9))
	if path != "" {
		t.Fatalf("find = %q, want \"\" — another row's segment is not this row's", path)
	}
}

// TestPlaybackReadinessRanksMisnumberedAboveRaw verifies the four states and
// that a mis-numbered render outranks the uncut source.
func TestPlaybackReadinessRanksMisnumberedAboveRaw(t *testing.T) {
	cases := []struct {
		name     string
		rendered string
		numbered bool
		raw      string
		want     string
	}{
		{"exact render wins", "/seg.mp4", true, "/raw.mkv", "rendered"},
		{"stale number still beats raw", "/seg.mp4", false, "/raw.mkv", "misnumbered"},
		{"no render falls back to raw", "", false, "/raw.mkv", "cached"},
		{"nothing playable", "", false, "", "missing"},
	}
	for _, tc := range cases {
		if got := playbackReadiness(tc.rendered, tc.numbered, tc.raw); got != tc.want {
			t.Errorf("%s: playbackReadiness = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestSegmentScannerFindsDoubleDigitPosition guards the padding trap: probing
// only positions 1 and 2 yields a shared "00" prefix under $INDEX_PAD3, which
// would exclude every segment numbered 010 and up.
func TestSegmentScannerFindsDoubleDigitPosition(t *testing.T) {
	dir := t.TempDir()
	old := segForPosition(t, dir, 47)
	if err := os.WriteFile(old.OutputPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if filepath.Base(old.OutputPath) != "047_song-a.mp4" {
		t.Fatalf("setup: basename = %q, want 047_song-a.mp4", filepath.Base(old.OutputPath))
	}

	path, numbered := newSegmentScanner().find("$INDEX_PAD3_$SAFE_TITLE", segForPosition(t, dir, 2))
	if path != old.OutputPath || numbered {
		t.Fatalf("find = (%q, %v), want (%q, false)", path, numbered, old.OutputPath)
	}
}

// TestSegmentScannerPrefersNewest verifies that when a row has been reordered
// twice and left two segments on disk, the newer one is chosen.
func TestSegmentScannerPrefersNewest(t *testing.T) {
	dir := t.TempDir()
	older := segForPosition(t, dir, 3)
	newer := segForPosition(t, dir, 8)
	for _, seg := range []render.Segment{older, newer} {
		if err := os.WriteFile(seg.OutputPath, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older.OutputPath, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	path, _ := newSegmentScanner().find("$INDEX_PAD3_$SAFE_TITLE", segForPosition(t, dir, 50))
	if path != newer.OutputPath {
		t.Fatalf("find = %q, want the newer segment %q", path, newer.OutputPath)
	}
}
