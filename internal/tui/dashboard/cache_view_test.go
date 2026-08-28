package dashboard

import (
	"strings"
	"testing"
)

// TestCacheInlineEditRenderSkipsTrailingColumns verifies that when a cache
// entry row is in edit mode on a specific field, trailing columns are NOT
// rendered as separate cells (they are replaced by the overflow stretch).
// This is verified by checking that the edit row contains the edit value but
// NOT the value from the next column that would normally follow.
//
// Without the fix (continue instead of break), the next column value would
// appear after the edit cell, which would be wrong. With the fix (break after
// the edit cell), only the edit cell and the FILE column (if not skipped by
// the edit cell overflow) should appear.
func TestCacheInlineEditRenderSkipsTrailingColumns(t *testing.T) {
	// Create entries where the second column has a distinct, easy-to-search value.
	// We'll edit the first column and verify the second column value does NOT
	// appear on that row (because it's skipped by the break statement).
	const uniqueArtistValue = "UNIQUEARTIST12345"

	v := cacheView{
		columns: []string{"title", "artist"},
		allEntries: []cacheEntry{
			{
				Identifier: "vid1",
				Source:     "http://example.com/vid1",
				CachedPath: "/cache/vid1.mp4",
				// First entry: editing this one
				Values: []string{"My Song", "Some Artist"},
			},
			{
				Identifier: "vid2",
				Source:     "http://example.com/vid2",
				CachedPath: "/cache/vid2.mp4",
				// Second entry: not editing, so artist column should show
				Values: []string{"Another Song", uniqueArtistValue},
			},
		},
		filteredEntries: []cacheEntry{
			{
				Identifier: "vid1",
				Source:     "http://example.com/vid1",
				CachedPath: "/cache/vid1.mp4",
				Values:     []string{"My Song", "Some Artist"},
			},
			{
				Identifier: "vid2",
				Source:     "http://example.com/vid2",
				CachedPath: "/cache/vid2.mp4",
				Values:     []string{"Another Song", uniqueArtistValue},
			},
		},
		showAll:      false,
		editing:      true,
		cursor:       0,
		editFieldIdx: 0, // editing the first column (title)
		editValue:    "EditedTitle",
		editCursor:   5,
		termWidth:    120,
		termHeight:   30,
		rowStatus:    make(map[string]string),
	}

	rendered := v.view()
	lines := strings.Split(rendered, "\n")

	// Find the line with the edited entry (the one with "EditedTitle")
	var editRowLine string
	for _, line := range lines {
		if strings.Contains(line, "EditedTitle") {
			editRowLine = line
			break
		}
	}

	if editRowLine == "" {
		t.Errorf("cache edit row did not render the edited value 'EditedTitle' in view:\n%s", rendered)
	}

	// Find the line with the non-edited entry (should have the unique artist)
	var nonEditRowLine string
	for _, line := range lines {
		if strings.Contains(line, uniqueArtistValue) {
			nonEditRowLine = line
			break
		}
	}

	if nonEditRowLine == "" {
		t.Errorf("non-edited cache row did not show the artist column value %q in view:\n%s", uniqueArtistValue, rendered)
	}

	// The non-edit row should have both the song title and the artist.
	// The edit row should NOT have "Some Artist" from the edited row's second column
	// (because the break statement skips rendering it).
	if strings.Contains(editRowLine, "Some Artist") {
		t.Errorf("edit row unexpectedly contains the second column value 'Some Artist'. This suggests the break statement is not working correctly (trailing columns are being rendered). Edit row: %q", editRowLine)
	}
}

// TestConfirmDeletePromptRoutesThroughFooterInConfirmStyle verifies that,
// like the timeline view (and unlike the collection view's per-row
// insertion), the cache view has no per-row insertion point for
// confirmDelete — it is routed through the single shared footer help row
// via renderHelpRow, styled with confirmStyle, and its presence does not
// change the total rendered line count (no row drift) versus the
// no-prompt render.
func TestCacheConfirmDeletePromptRoutesThroughFooterInConfirmStyle(t *testing.T) {
	withANSIColorProfile(t)

	entries := []cacheEntry{
		{
			Identifier: "vid1",
			Source:     "http://example.com/vid1",
			CachedPath: "/cache/vid1.mp4",
			Values:     []string{"My Song", "Some Artist"},
		},
		{
			Identifier: "vid2",
			Source:     "http://example.com/vid2",
			CachedPath: "/cache/vid2.mp4",
			Values:     []string{"Another Song", "Other Artist"},
		},
	}

	base := cacheView{
		columns:         []string{"title", "artist"},
		allEntries:      entries,
		filteredEntries: entries,
		showAll:         false,
		cursor:          0,
		termWidth:       100,
		termHeight:      40,
		rowStatus:       make(map[string]string),
	}

	without := base
	without.confirmDelete = ""
	withoutRendered := without.view()
	withoutLines := strings.Count(withoutRendered, "\n")

	with := base
	with.confirmDelete = "Delete vid1? [y/n]"
	withRendered := with.view()
	withLines := strings.Count(withRendered, "\n")

	if withLines != withoutLines {
		t.Errorf("expected total line count to stay unchanged (prompt replaces the footer, not adds a row): without=%d with=%d\nwith:\n%s", withoutLines, withLines, withRendered)
	}

	wantFooter := helpRowText(with.confirmDelete, confirmStyle, with.termWidth)
	if !strings.Contains(withRendered, wantFooter) {
		t.Errorf("footer does not carry the confirmStyle-rendered prompt.\nwant substring: %q\ngot view:\n%s", wantFooter, withRendered)
	}
}

// TestCacheAddSlotRendersInPlaceOfHelpRow verifies the focused add-slot
// replaces the default help row without adding a line — mirroring
// timelineView.renderAddSlot's contract (asserted the same way by
// TestCacheConfirmDeletePromptRoutesThroughFooterInConfirmStyle above).
func TestCacheAddSlotRendersInPlaceOfHelpRow(t *testing.T) {
	entries := []cacheEntry{
		{Identifier: "vid1", Source: "http://example.com/vid1", CachedPath: "/cache/vid1.mp4", Values: []string{"My Song", "Some Artist"}},
	}

	base := cacheView{
		columns:         []string{"title", "artist"},
		allEntries:      entries,
		filteredEntries: entries,
		cursor:          0,
		termWidth:       100,
		termHeight:      40,
		rowStatus:       make(map[string]string),
	}

	without := base
	withoutRendered := without.view()
	withoutLines := strings.Count(withoutRendered, "\n")

	with := base
	with.addFocus = true
	with.addBuffer = "HWl1Tu9oZmY"
	with.addCursor = len(with.addBuffer)
	withRendered := with.view()
	withLines := strings.Count(withRendered, "\n")

	if withLines != withoutLines {
		t.Errorf("expected total line count to stay unchanged (add-slot replaces the footer, not adds a row): without=%d with=%d\nwith:\n%s", withoutLines, withLines, withRendered)
	}
	if !strings.Contains(withRendered, "HWl1Tu9oZmY") {
		t.Errorf("rendered view does not contain the add-slot buffer text.\ngot view:\n%s", withRendered)
	}
}

// TestCacheAddSlotShowsClassificationHint verifies the add-slot's trailing
// hint (set by refreshAddCacheHint) is rendered when the buffer classifies
// as a recognized input, falling back to the default keys hint otherwise.
func TestCacheAddSlotShowsClassificationHint(t *testing.T) {
	base := cacheView{
		columns:    []string{"title", "artist"},
		termWidth:  120,
		termHeight: 40,
		rowStatus:  make(map[string]string),
	}

	withHint := base
	withHint.addFocus = true
	withHint.addBuffer = "https://example.com/watch?v=abc"
	withHint.addCursor = len(withHint.addBuffer)
	withHint.addHint = "Enter downloads and caches this URL."
	gotWithHint := withHint.renderAddSlot()
	if !strings.Contains(gotWithHint, "Enter downloads and caches this URL.") {
		t.Errorf("renderAddSlot did not include the classification hint.\ngot: %q", gotWithHint)
	}

	withoutHint := base
	withoutHint.addFocus = true
	gotDefault := withoutHint.renderAddSlot()
	if !strings.Contains(gotDefault, "Enter add") {
		t.Errorf("renderAddSlot did not fall back to the default keys hint when addHint is empty.\ngot: %q", gotDefault)
	}
}

// TestComputeCacheColumnWidthsDistributesLeftoverToCappedColumns mirrors
// TestComputeColumnWidthsDistributesLeftoverToCappedColumns for the cache
// view's column-width function: a short configured column stays floored at
// columnMinWidth while the FILE column (whose basename was truncated by
// columnMaxWidth) absorbs the leftover.
func TestComputeCacheColumnWidthsDistributesLeftoverToCappedColumns(t *testing.T) {
	headers := []string{"SHORT", "FILE"}
	entries := []cacheEntry{
		{Values: []string{"ab"}, CachedPath: strings.Repeat("x", 200)},
	}

	// natural: SHORT -> floored to columnMinWidth (6); FILE -> capped to
	// columnMaxWidth (45). natural total = 51. baseWidth = 0, so available =
	// tableWidth. Give it 10 extra columns of leftover.
	widths := computeCacheColumnWidths(headers, entries, 51+10, 0)

	if got, want := widths[0], columnMinWidth; got != want {
		t.Errorf("short column width = %d, want %d (floored, not grown)", got, want)
	}
	if got, want := widths[1], columnMaxWidth+10; got != want {
		t.Errorf("file column width = %d, want %d (capped column absorbs all leftover)", got, want)
	}
}

// TestComputeCacheColumnWidthsShrinksProportionallyUnderPressure mirrors
// TestComputeColumnWidthsShrinksProportionallyUnderPressure for the cache
// view: under a width deficit, both the configured column and the FILE
// column give up width proportional to their slack above columnMinWidth.
func TestComputeCacheColumnWidthsShrinksProportionallyUnderPressure(t *testing.T) {
	headers := []string{"A", "FILE"}
	entries := []cacheEntry{
		{Values: []string{strings.Repeat("a", 20)}, CachedPath: strings.Repeat("b", 30)},
	}

	// natural: A=20, FILE=30, total=50. available=30, deficit=20.
	// slack: A=20-6=14, FILE=30-6=24, totalSlack=38.
	// cut_A = 20*14/38 = 7, cut_FILE = 20*24/38 = 12.
	widths := computeCacheColumnWidths(headers, entries, 30, 0)

	if got, want := widths[0], 20-7; got != want {
		t.Errorf("column A width = %d, want %d", got, want)
	}
	if got, want := widths[1], 30-12; got != want {
		t.Errorf("FILE column width = %d, want %d", got, want)
	}
	for i, w := range widths {
		if w < columnMinWidth {
			t.Errorf("column %d width %d fell below columnMinWidth %d", i, w, columnMinWidth)
		}
	}
}

// TestComputeCacheColumnWidthsNoColumns verifies the empty-headers case
// returns nil rather than an empty-but-allocated slice or panicking.
func TestComputeCacheColumnWidthsNoColumns(t *testing.T) {
	if widths := computeCacheColumnWidths(nil, nil, 100, 0); widths != nil {
		t.Errorf("computeCacheColumnWidths with no headers = %v, want nil", widths)
	}
}
