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

// TestSegmentScannerRefusesPositionOnlyNames is the guard behind the bug where
// two interstitials played the same video: with no title column the template
// renders down to pure position ("001"), leaving an empty prefix and suffix
// that match every file in the directory.
func TestSegmentScannerRefusesPositionOnlyNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"001.mp4", "002.mp4"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// A row with no id and no title: nothing can make its name specific.
	seg := render.Segment{Clip: project.Clip{PlaybackPosition: 9}}
	seg.OutputPath = filepath.Join(dir, render.SegmentBaseName("$INDEX_PAD3_$SAFE_TITLE", seg)+".mp4")

	path, numbered := newSegmentScanner().find("$INDEX_PAD3_$SAFE_TITLE", seg)
	if path != "" || numbered {
		t.Fatalf("find = (%q, %v), want (\"\", false) — a position-only name must never match another row", path, numbered)
	}
}

// TestSegmentBaseNameAppendsRowIDWhenNameIsPositionOnly verifies the naming
// fix: a titleless row's segment carries its row id, so two of them can never
// resolve to the same file.
func TestSegmentBaseNameAppendsRowIDWhenNameIsPositionOnly(t *testing.T) {
	a := render.Segment{Clip: project.Clip{PlaybackPosition: 14, Row: csvplan.Row{RowID: "285919"}}}
	b := render.Segment{Clip: project.Clip{PlaybackPosition: 15, Row: csvplan.Row{RowID: "0335f2"}}}

	nameA := render.SegmentBaseName("$INDEX_PAD3_$SAFE_TITLE", a)
	nameB := render.SegmentBaseName("$INDEX_PAD3_$SAFE_TITLE", b)
	if nameA != "014_285919" {
		t.Fatalf("name = %q, want 014_285919", nameA)
	}
	if nameA == nameB {
		t.Fatalf("two titleless rows share the name %q", nameA)
	}

	// The same row at a different position keeps its id, so the scanner's
	// prefix/suffix can still find it after a reorder.
	moved := a
	moved.Clip.PlaybackPosition = 90
	if got := render.SegmentBaseName("$INDEX_PAD3_$SAFE_TITLE", moved); got != "090_285919" {
		t.Fatalf("moved name = %q, want 090_285919", got)
	}
}

// TestSegmentBaseNameLeavesSpecificNamesAlone verifies a row that does
// contribute to its own name is not given a redundant id suffix — the 120
// already-rendered song segments must keep resolving.
func TestSegmentBaseNameLeavesSpecificNamesAlone(t *testing.T) {
	seg := render.Segment{Clip: project.Clip{
		PlaybackPosition: 2,
		Row:              csvplan.Row{RowID: "ac4f5b", Title: "Miami"},
	}}
	if got := render.SegmentBaseName("$INDEX_PAD3_$SAFE_TITLE", seg); got != "002_miami" {
		t.Fatalf("name = %q, want 002_miami unchanged", got)
	}
}

// TestSourceCacheResolvesEachLinkOnce verifies the memo: a link is resolved
// once per loaded state, however many times the readiness map asks for it.
// Reordering the playback order cannot change whether a source file exists,
// so a gesture must never pay to find out again — on a plan whose links sit
// on a network mount, each cold stat costs hundreds of milliseconds.
func TestSourceCacheResolvesEachLinkOnce(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	c := newSourceCache()
	row := csvplan.CollectionRow{Link: present}
	first := c.resolve(nil, dir, row)
	if first != present {
		t.Fatalf("resolve = %q, want %q", first, present)
	}

	// Delete the file: a second resolve that hit the disk would now return "".
	if err := os.Remove(present); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := c.resolve(nil, dir, row); got != present {
		t.Fatalf("resolve = %q after the file vanished, want the memoized %q — the second call went to disk", got, present)
	}

	// A fresh cache is what a reload gets, and it sees the current truth.
	if got := newSourceCache().resolve(nil, dir, row); got != "" {
		t.Fatalf("a fresh cache returned %q, want \"\" — a reload must re-resolve", got)
	}
}

// TestSourceCacheNilResolvesDirectly verifies the nil receiver still works,
// so a caller with no cache is not a nil-pointer panic.
func TestSourceCacheNilResolvesDirectly(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var c *sourceCache
	if got := c.resolve(nil, dir, csvplan.CollectionRow{Link: present}); got != present {
		t.Fatalf("nil-receiver resolve = %q, want %q", got, present)
	}
}
