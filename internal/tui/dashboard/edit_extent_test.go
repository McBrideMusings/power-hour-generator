package dashboard

import (
	"strings"
	"testing"

	"powerhour/pkg/csvplan"
)

// editOverflowExtent is shared by the collection and cache views, so its
// behaviour is pinned in one place.

const (
	extentGutterWidth    = 10
	extentGutterGapWidth = 4
	extentColumnGapWidth = 2
)

func extentWidths() []int { return []int{15, 20, 25} }

func wantXOffset(widths []int, editIdx int) int {
	x := extentGutterWidth + extentGutterGapWidth
	for k := range editIdx {
		x += widths[k] + extentColumnGapWidth
	}
	return x
}

func callExtent(widths []int, editIdx, termWidth, valueWidth int) (int, int, int) {
	return editOverflowExtent(widths, editIdx, extentGutterWidth, extentGutterGapWidth, extentColumnGapWidth, termWidth, valueWidth)
}

// A value that fits its own column must not touch the columns to its right —
// this is what lets you read a row's link while typing its label.
func TestEditExtentShortValueStaysInsideItsColumn(t *testing.T) {
	widths := extentWidths()

	for editIdx := range widths {
		x, cellWidth, covered := callExtent(widths, editIdx, 200, 5)

		if want := wantXOffset(widths, editIdx); x != want {
			t.Errorf("column %d: xOffset = %d, want %d", editIdx, x, want)
		}
		if cellWidth != widths[editIdx] {
			t.Errorf("column %d: cellWidth = %d, want its own width %d", editIdx, cellWidth, widths[editIdx])
		}
		if covered != 0 {
			t.Errorf("column %d: covered = %d, want 0 — a short edit hides nothing", editIdx, covered)
		}
	}
}

// A value too long for its column grows rightwards, and lands on a column
// boundary so what is still on screen stays aligned with its header.
func TestEditExtentLongValueGrowsToAColumnBoundary(t *testing.T) {
	widths := extentWidths()

	// Needs 40+headroom; column 0 is 15 wide, so it must swallow column 1
	// (20 wide + a 2-wide gap) to reach 37 and then column 2 as well.
	_, cellWidth, covered := callExtent(widths, 0, 200, 40)

	if covered != 2 {
		t.Fatalf("covered = %d, want 2", covered)
	}
	want := widths[0] + extentColumnGapWidth + widths[1] + extentColumnGapWidth + widths[2]
	if cellWidth != want {
		t.Errorf("cellWidth = %d, want %d (a whole number of columns)", cellWidth, want)
	}
}

// Growth stops one column short rather than half-covering the next one.
func TestEditExtentGrowsOnlyAsFarAsItNeeds(t *testing.T) {
	widths := extentWidths()

	// 20 + headroom = 24, which column 0 (15) cannot hold but 15+2+20 = 37 can.
	_, cellWidth, covered := callExtent(widths, 0, 200, 20)

	if covered != 1 {
		t.Fatalf("covered = %d, want 1 — column 2 was not needed", covered)
	}
	if want := widths[0] + extentColumnGapWidth + widths[1]; cellWidth != want {
		t.Errorf("cellWidth = %d, want %d", cellWidth, want)
	}
}

// The last column has nothing to snap to, so it takes the leftover margin.
func TestEditExtentLastColumnTakesTheMargin(t *testing.T) {
	widths := extentWidths()
	const termWidth = 140

	x, cellWidth, covered := callExtent(widths, 2, termWidth, 200)

	if covered != 0 {
		t.Errorf("covered = %d, want 0 — nothing sits to the right of the last column", covered)
	}
	if want := termWidth - x - 2; cellWidth != want {
		t.Errorf("cellWidth = %d, want %d (the right margin less its pad)", cellWidth, want)
	}
}

// A terminal too narrow to hold the column floors at the column's own width
// rather than going negative.
func TestEditExtentNarrowTerminalFloorsAtColumnWidth(t *testing.T) {
	widths := extentWidths()

	_, cellWidth, covered := callExtent(widths, 2, 30, 200)

	if cellWidth != widths[2] {
		t.Errorf("cellWidth = %d, want the column floor %d", cellWidth, widths[2])
	}
	if covered != 0 {
		t.Errorf("covered = %d, want 0", covered)
	}
}

func extentEditView(editValue string) collectionView {
	return collectionView{
		name: "interstitials",
		rows: []csvplan.CollectionRow{{
			Index: 1,
			CustomFields: map[string]string{
				"label":      "driver dance",
				"start_time": "42:43",
				"link":       "MEMORABLE_LINK_TEXT",
			},
		}},
		states: []rowState{rowRendered},
		columns: []collectionColumn{
			{field: "label", header: "LABEL"},
			{field: "start_time", header: "START_TIME"},
			{field: "link", header: "LINK"},
		},
		rowStatus:    make(map[int]string),
		termWidth:    120,
		termHeight:   30,
		editing:      true,
		cursor:       0,
		editFieldIdx: 1,
		editValue:    editValue,
		editCursor:   0,
	}
}

// The reported bug: editing a short field used to blank out the link, which
// is the only thing identifying the row you are labelling.
func TestInlineEditKeepsTrailingColumnsVisibleForAShortValue(t *testing.T) {
	rendered := extentEditView("42:43").view()

	if !strings.Contains(rendered, "MEMORABLE_LINK_TEXT") {
		t.Errorf("the link column vanished while editing a short field:\n%s", rendered)
	}
	if strings.Contains(rendered, "fields") {
		t.Errorf("an overflow hint appeared though nothing was hidden:\n%s", rendered)
	}
}

// A genuinely long value still takes the room it needs and says what it hid.
func TestInlineEditHidesTrailingColumnsForALongValue(t *testing.T) {
	rendered := extentEditView(strings.Repeat("x", 80)).view()

	if strings.Contains(rendered, "MEMORABLE_LINK_TEXT") {
		t.Errorf("the link column survived an 80-character edit that should cover it:\n%s", rendered)
	}
	if !strings.Contains(rendered, "+1 fields") {
		t.Errorf("expected a +1 fields hint for the covered link column:\n%s", rendered)
	}
}
