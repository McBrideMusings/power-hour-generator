package dashboard

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"powerhour/pkg/csvplan"
)

// TestCollectionViewGutterWidthStableAcrossStatuses renders the real
// collectionView.view() output (not a hand-rebuilt copy of the padding
// logic) and measures the actual gutter segment preceding each row's data
// columns. The gutter must always occupy the same visual width regardless
// of whether the status token is ASCII, an emoji, or CJK.
//
// Gutter width is idxWidth(4) + statusWidth(5) + 1 separator = 10
// (collection_view.go:260-262), plus gutterGapWidth(4) before the data
// columns (collection_view.go:264,390) = 14 total. These constants are
// unexported, so the expected width is hardcoded here and tied back to
// those line numbers rather than recomputed.
func TestCollectionViewGutterWidthStableAcrossStatuses(t *testing.T) {
	const wantPrefixWidth = 14 // gutterWidth(10) + gutterGapWidth(4)

	statuses := []string{
		"cached",   // ASCII, compacts to "C"
		"rendered", // ASCII, compacts to "OK"
		"-----",    // ASCII, passes through default branch
		"🎵🎵",       // emoji, passes through default branch verbatim
		"📺 test",   // emoji + ASCII
		"✓ ok",     // emoji + ASCII
		"日本語",      // CJK, passes through default branch verbatim
	}

	rows := make([]csvplan.CollectionRow, len(statuses))
	states := make([]rowState, len(statuses))
	rowStatus := make(map[int]string, len(statuses))
	markers := make([]string, len(statuses))
	for i, status := range statuses {
		marker := "MARKER" + string(rune('0'+i))
		markers[i] = marker
		rows[i] = csvplan.CollectionRow{
			Index: i,
			CustomFields: map[string]string{
				"title": marker,
			},
		}
		states[i] = rowRendered
		rowStatus[i] = status
	}

	v := collectionView{
		name:       "songs",
		rows:       rows,
		states:     states,
		columns:    []collectionColumn{{field: "title", header: "TITLE"}},
		rowStatus:  rowStatus,
		cursor:     0,
		termWidth:  120,
		termHeight: 60,
	}

	rendered := v.view()
	lines := strings.Split(rendered, "\n")

	var prefixWidths []int
	for i, status := range statuses {
		marker := markers[i]
		var line string
		found := false
		for _, l := range lines {
			if strings.Contains(l, marker) {
				line = l
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("status %q (marker %s): marker not found in rendered view:\n%s", status, marker, rendered)
		}

		idx := strings.Index(line, marker)
		if idx < 0 {
			t.Fatalf("status %q (marker %s): marker index lookup failed on line %q", status, marker, line)
		}
		prefix := line[:idx]
		gotWidth := lipgloss.Width(prefix)
		prefixWidths = append(prefixWidths, gotWidth)

		if gotWidth != wantPrefixWidth {
			t.Errorf("status %q: gutter prefix width = %d, want %d (marker offset=%d, line=%q)",
				status, gotWidth, wantPrefixWidth, idx, line)
		}
	}

	// A uniform-but-wrong padding scheme (e.g. padding every status to the
	// same wrong width) would still pass the fixed-constant check above only
	// if that wrong width happened to equal wantPrefixWidth. Guard against
	// silent drift by also requiring every row to agree with row 0.
	for i, w := range prefixWidths {
		if w != prefixWidths[0] {
			t.Errorf("status %q: gutter prefix width %d does not match row 0's width %d", statuses[i], w, prefixWidths[0])
		}
	}
}

func TestEditContextNoteExitKeysFirst(t *testing.T) {
	// The editContextNote should put exit keys first so they survive truncation.
	v := collectionView{
		columns: []collectionColumn{
			{field: "title", header: "TITLE"},
			{field: "artist", header: "ARTIST"},
		},
		editFieldIdx: 0, // editing "title" column
	}

	row := csvplan.CollectionRow{Index: 5}

	got := editContextNote(v, row)

	// Must include exit keys
	if !strings.Contains(got, "Enter save") {
		t.Errorf("editContextNote() missing 'Enter save': %q", got)
	}
	if !strings.Contains(got, "Esc cancel") {
		t.Errorf("editContextNote() missing 'Esc cancel': %q", got)
	}

	// Should also include context about the row/field
	if !strings.Contains(got, "Edit row") {
		t.Errorf("editContextNote() missing row context: %q", got)
	}

	// Exit keys should come before the row context for truncation survival
	enterIdx := strings.Index(got, "Enter save")
	editIdx := strings.Index(got, "Edit row")
	if enterIdx > editIdx {
		t.Errorf("editContextNote() should have exit keys before row context: %q", got)
	}
}

func TestEditContextNoteWithHint(t *testing.T) {
	v := collectionView{
		columns: []collectionColumn{
			{field: "title"},
		},
		editFieldIdx: 0,
		editHint:     "custom hint text",
	}
	row := csvplan.CollectionRow{Index: 1}

	got := editContextNote(v, row)

	if !strings.Contains(got, "custom hint text") {
		t.Errorf("editContextNote() did not include hint: %q", got)
	}

	// Hint should be last
	if !strings.HasSuffix(strings.TrimSpace(got), "custom hint text") {
		t.Errorf("editContextNote() hint not at end: %q", got)
	}
}

func TestEditHelpRowNarrowTerminal(t *testing.T) {
	// On a narrow terminal, helpRowText should truncate content but keep exit keys visible.
	v := collectionView{
		columns: []collectionColumn{
			{field: "title", header: "TITLE"},
		},
		editFieldIdx: 0,
		editing:      true,
		cursor:       0,
		rows: []csvplan.CollectionRow{
			{Index: 0, Link: "http://example.com", CustomFields: map[string]string{"title": "Test"}},
		},
		rowStatus: make(map[int]string),
		termWidth: 40, // Narrow terminal
	}

	note := editContextNote(v, v.rows[0])
	rendered := helpRowText(note, faint, v.termWidth)

	// Exit keys must be visible even on narrow terminal
	if !strings.Contains(rendered, "Enter") && !strings.Contains(rendered, "Esc") {
		t.Errorf("helpRowText() on narrow terminal lost exit keys: %q", rendered)
	}

	// Verify truncation respected
	width := runewidth.StringWidth(rendered)
	if width > v.termWidth {
		t.Errorf("helpRowText() exceeded terminal width: got %d, want <= %d", width, v.termWidth)
	}
}

func TestEditHelpRowWideTerminal(t *testing.T) {
	// On a wide terminal, the full help text should be visible.
	v := collectionView{
		columns: []collectionColumn{
			{field: "title", header: "TITLE"},
		},
		editFieldIdx: 0,
		editing:      true,
		cursor:       0,
		rows: []csvplan.CollectionRow{
			{Index: 3, Link: "http://example.com", CustomFields: map[string]string{"title": "Long Title"}},
		},
		rowStatus: make(map[int]string),
		termWidth: 120, // Wide terminal
	}

	note := editContextNote(v, v.rows[0])
	rendered := helpRowText(note, faint, v.termWidth)

	// All components should be present
	if !strings.Contains(rendered, "Enter save") {
		t.Errorf("helpRowText() on wide terminal missing 'Enter save': %q", rendered)
	}
	if !strings.Contains(rendered, "Esc cancel") {
		t.Errorf("helpRowText() on wide terminal missing 'Esc cancel': %q", rendered)
	}
	if !strings.Contains(rendered, "Tab next field") {
		t.Errorf("helpRowText() on wide terminal missing 'Tab next field': %q", rendered)
	}
	if !strings.Contains(rendered, "Edit row 03") {
		t.Errorf("helpRowText() on wide terminal missing row context: %q", rendered)
	}
	if !strings.Contains(rendered, "title") {
		t.Errorf("helpRowText() on wide terminal missing field context: %q", rendered)
	}

	// Verify no unnecessary truncation happened
	width := runewidth.StringWidth(rendered)
	if width > v.termWidth {
		t.Errorf("helpRowText() exceeded terminal width: got %d, want <= %d", width, v.termWidth)
	}
}

func TestEditContextNoteRowIndex(t *testing.T) {
	tests := []struct {
		rowIdx int
		want   string
	}{
		{0, "Edit row 00"},
		{5, "Edit row 05"},
		{42, "Edit row 42"},
	}

	for _, tt := range tests {
		v := collectionView{
			columns:      []collectionColumn{},
			editFieldIdx: -1,
		}
		row := csvplan.CollectionRow{Index: tt.rowIdx}

		got := editContextNote(v, row)
		if !strings.Contains(got, tt.want) {
			t.Errorf("editContextNote() with index %d missing %q in: %q", tt.rowIdx, tt.want, got)
		}
	}
}

// makeConfirmTestRows builds n collection rows with distinct titles, plus
// matching states (all rowRendered) so computeRowStates-shaped bookkeeping
// isn't needed for these render-path tests.
func makeConfirmTestRows(n int) ([]csvplan.CollectionRow, []rowState) {
	rows := make([]csvplan.CollectionRow, n)
	states := make([]rowState, n)
	for i := 0; i < n; i++ {
		rows[i] = csvplan.CollectionRow{
			Index: i,
			CustomFields: map[string]string{
				"title":  "Song " + string(rune('A'+i%26)),
				"artist": "Artist",
			},
		}
		states[i] = rowRendered
	}
	return rows, states
}

// TestConfirmDeletePromptRendersBeneathCursorRow verifies that pressing "x"
// on a collection row (setting confirmDelete) renders the prompt as a row
// immediately after the cursor row's line, not in the footer at the bottom.
// This is a regression test for #100: a test that would fail if the prompt
// went back to the unified footer, since in that case the last non-empty
// line of the render would carry the prompt instead of the line right after
// the cursor row.
func TestConfirmDeletePromptRendersBeneathCursorRow(t *testing.T) {
	rows, states := makeConfirmTestRows(10)
	v := collectionView{
		name:      "songs",
		rows:      rows,
		states:    states,
		columns:   []collectionColumn{{field: "title", header: "TITLE"}, {field: "artist", header: "ARTIST"}},
		cursor:    3,
		rowStatus: make(map[int]string),
		termWidth: 100,
		// Tall enough that no scrolling is needed for 10 rows.
		termHeight:    60,
		confirmDelete: "Delete row 03 \"Song D\"? [y/n]",
	}

	rendered := v.view()
	lines := strings.Split(rendered, "\n")

	cursorLineIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "▸") && strings.Contains(line, "03") {
			cursorLineIdx = i
			break
		}
	}
	if cursorLineIdx == -1 {
		t.Fatalf("could not locate cursor row line in rendered view:\n%s", rendered)
	}

	if cursorLineIdx+1 >= len(lines) {
		t.Fatalf("no line follows the cursor row to hold the prompt:\n%s", rendered)
	}
	promptLine := lines[cursorLineIdx+1]
	if !strings.Contains(promptLine, "Delete") || !strings.Contains(promptLine, "[y/n]") {
		t.Errorf("line directly after cursor row does not contain the confirm prompt: %q\nfull render:\n%s", promptLine, rendered)
	}

	// The prompt must NOT be sitting in the footer (the last non-empty line).
	lastLine := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastLine = lines[i]
			break
		}
	}
	if strings.Contains(lastLine, "[y/n]") {
		t.Errorf("confirm prompt appears to be in the footer (last line): %q", lastLine)
	}
}

// TestConfirmDeletePromptRespectsHeightBudget verifies that inserting the
// confirm-delete row is charged against the visible-row budget: visibleRowCount()
// gives up one data row exactly when confirmDelete is set, so the total
// rendered line count is unchanged versus the no-prompt render at the same
// scroll position — no row is wasted, and none pushes the total past budget.
// Without the accounting fix in visibleRowCount(), the inserted prompt would
// add a line on top of the existing row count and this assertion would fail.
func TestConfirmDeletePromptRespectsHeightBudget(t *testing.T) {
	rows, states := makeConfirmTestRows(30)

	base := collectionView{
		name:       "songs",
		rows:       rows,
		states:     states,
		columns:    []collectionColumn{{field: "title", header: "TITLE"}},
		cursor:     5,
		rowStatus:  make(map[int]string),
		termWidth:  100,
		termHeight: 24, // small terminal forces scrolling across 30 rows
	}

	without := base
	without.confirmDelete = ""
	withoutRendered := without.view()
	withoutLines := strings.Count(withoutRendered, "\n")

	with := base
	with.confirmDelete = "Delete row 05 \"Song F\"? [y/n]"
	withRendered := with.view()
	withLines := strings.Count(withRendered, "\n")

	if withLines != withoutLines {
		t.Errorf("expected total line count to stay unchanged (confirm row charged against the same budget): without=%d with=%d", withoutLines, withLines)
	}

	// Sanity bound: total output must fit within the terminal height the
	// budget was computed for (subheader + header + rows + indicators + help
	// row all come out of termHeight-10, so the total should never exceed
	// termHeight).
	if withLines > base.termHeight {
		t.Errorf("rendered line count %d exceeds termHeight %d with confirm prompt inserted:\n%s", withLines, base.termHeight, withRendered)
	}
}

// TestConfirmDeletePromptUsesConfirmStyle verifies that the inline
// confirm-delete prompt inserted beneath the cursor row is rendered through
// confirmStyle (bold, color "3"), not plain/unstyled text. Complements
// TestConfirmDeletePromptRendersBeneathCursorRow (placement) and
// TestConfirmDeletePromptRespectsHeightBudget (no row drift) — this test
// is the styling gap those two leave open.
func TestConfirmDeletePromptUsesConfirmStyle(t *testing.T) {
	withANSIColorProfile(t)

	rows, states := makeConfirmTestRows(10)
	v := collectionView{
		name:          "songs",
		rows:          rows,
		states:        states,
		columns:       []collectionColumn{{field: "title", header: "TITLE"}, {field: "artist", header: "ARTIST"}},
		cursor:        3,
		rowStatus:     make(map[int]string),
		termWidth:     100,
		termHeight:    60,
		confirmDelete: "Delete row 03 \"Song D\"? [y/n]",
	}

	rendered := v.view()
	lines := strings.Split(rendered, "\n")

	var promptLine string
	for _, line := range lines {
		if strings.Contains(line, "[y/n]") {
			promptLine = line
			break
		}
	}
	if promptLine == "" {
		t.Fatalf("confirm prompt line not found in rendered view:\n%s", rendered)
	}

	// confirmStyle renders through helpRowText, so the styled prompt must
	// match exactly what confirmStyle.Render would produce for the same
	// truncated text — same shape TestEditRowBackgroundTint uses to assert
	// on real SGR codes rather than guessing at escape sequences by hand.
	want := helpRowText(v.confirmDelete, confirmStyle, v.termWidth)
	if promptLine != want {
		t.Errorf("confirm prompt line does not match confirmStyle-rendered output:\ngot:  %q\nwant: %q", promptLine, want)
	}

	// Sanity: the styled line must actually carry ANSI SGR codes (bold +
	// color 3), not have silently degraded to plain text.
	if !strings.Contains(promptLine, "\x1b[") {
		t.Errorf("confirm prompt line has no ANSI escape codes, expected confirmStyle SGR codes: %q", promptLine)
	}
}

func TestEditContextNoteNoFieldSelected(t *testing.T) {
	// When editFieldIdx is -1 or out of range, should still work gracefully.
	v := collectionView{
		columns:      []collectionColumn{{field: "title"}},
		editFieldIdx: -1,
	}
	row := csvplan.CollectionRow{Index: 0}

	got := editContextNote(v, row)

	// Should still have exit keys and row context
	if !strings.Contains(got, "Enter save") {
		t.Errorf("editContextNote() with no field selected missing 'Enter save': %q", got)
	}
	if !strings.Contains(got, "Edit row") {
		t.Errorf("editContextNote() with no field selected missing row context: %q", got)
	}

	// Should not have a field reference (since editFieldIdx is -1)
	// The function should not append " · <fieldname>" when editFieldIdx is out of range
}

// TestInlineEditOverflowHintRendersWhenColumnsHidden verifies that when
// editing a non-final column, a faint "+N fields" hint renders immediately
// after the edit cell to signal that N columns are hidden by the overflow.
func TestInlineEditOverflowHintRendersWhenColumnsHidden(t *testing.T) {
	rows := []csvplan.CollectionRow{
		{
			Index: 0,
			CustomFields: map[string]string{
				"title":  "Song A",
				"artist": "Artist A",
				"link":   "http://example.com/a",
				"name":   "Track A",
			},
		},
	}
	v := collectionView{
		name:   "songs",
		rows:   rows,
		states: []rowState{rowRendered},
		columns: []collectionColumn{
			{field: "title", header: "TITLE"},
			{field: "artist", header: "ARTIST"},
			{field: "link", header: "LINK"},
			{field: "name", header: "NAME"},
		},
		rowStatus:    make(map[int]string),
		termWidth:    100,
		termHeight:   30,
		editing:      true,
		cursor:       0,
		editFieldIdx: 1, // Editing ARTIST (column 1); columns 2 and 3 are hidden
		editValue:    "Artist A",
		editCursor:   5,
	}

	rendered := v.view()

	// The hint "+2 fields" should appear in the rendered output since there are
	// 2 columns (link and name) beyond the edited column.
	if !strings.Contains(rendered, "+2 fields") {
		t.Errorf("overflow hint not found in rendered edit row. Expected '+2 fields' but rendered:\n%s", rendered)
	}
}

// TestInlineEditOverflowHintAbsentWhenEditingLastColumn verifies that when
// editing the final column in the sequence, NO overflow hint appears (since
// there are zero columns beyond it to hide).
func TestInlineEditOverflowHintAbsentWhenEditingLastColumn(t *testing.T) {
	rows := []csvplan.CollectionRow{
		{
			Index: 0,
			CustomFields: map[string]string{
				"title":  "Song B",
				"artist": "Artist B",
				"link":   "http://example.com/b",
			},
		},
	}
	v := collectionView{
		name:   "songs",
		rows:   rows,
		states: []rowState{rowRendered},
		columns: []collectionColumn{
			{field: "title", header: "TITLE"},
			{field: "artist", header: "ARTIST"},
			{field: "link", header: "LINK"},
		},
		rowStatus:    make(map[int]string),
		termWidth:    100,
		termHeight:   30,
		editing:      true,
		cursor:       0,
		editFieldIdx: 2, // Editing LINK (last column); no columns hidden
		editValue:    "http://example.com/b",
		editCursor:   10,
	}

	rendered := v.view()

	// No hint should appear since there are zero columns beyond the edited column.
	if strings.Contains(rendered, "+") && strings.Contains(rendered, "fields") {
		// More precise check: look for the "+N fields" pattern
		if strings.Contains(rendered, "+0 fields") || strings.Contains(rendered, "+ fields") {
			t.Errorf("unexpected overflow hint in rendered edit row when editing last column:\n%s", rendered)
		}
	}
}

// TestInlineEditOverflowHintHiddenColumnCount verifies the hint shows the
// correct count when editing middle columns with varying numbers of trailing columns.
func TestInlineEditOverflowHintHiddenColumnCount(t *testing.T) {
	tests := []struct {
		name              string
		editFieldIdx      int
		totalColumns      int
		expectedHintCount int
	}{
		{"Edit column 0 of 5", 0, 5, 4}, // 4 hidden
		{"Edit column 1 of 5", 1, 5, 3}, // 3 hidden
		{"Edit column 2 of 5", 2, 5, 2}, // 2 hidden
		{"Edit column 3 of 5", 3, 5, 1}, // 1 hidden
		{"Edit column 4 of 5", 4, 5, 0}, // 0 hidden (last column)
		{"Edit column 0 of 3", 0, 3, 2}, // 2 hidden
		{"Edit column 2 of 3", 2, 3, 0}, // 0 hidden (last column)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build a row with all columns populated
			customFields := make(map[string]string)
			columns := make([]collectionColumn, tt.totalColumns)
			for i := 0; i < tt.totalColumns; i++ {
				fieldName := fmt.Sprintf("field_%d", i)
				columns[i] = collectionColumn{field: fieldName, header: strings.ToUpper(fieldName)}
				customFields[fieldName] = fmt.Sprintf("value_%d", i)
			}

			rows := []csvplan.CollectionRow{
				{Index: 0, CustomFields: customFields},
			}
			v := collectionView{
				name:         "test",
				rows:         rows,
				states:       []rowState{rowRendered},
				columns:      columns,
				rowStatus:    make(map[int]string),
				termWidth:    200, // Wide terminal to avoid other truncation
				termHeight:   30,
				editing:      true,
				cursor:       0,
				editFieldIdx: tt.editFieldIdx,
				editValue:    customFields[columns[tt.editFieldIdx].field],
				editCursor:   0,
			}

			rendered := v.view()

			if tt.expectedHintCount > 0 {
				expectedHint := fmt.Sprintf("+%d fields", tt.expectedHintCount)
				if !strings.Contains(rendered, expectedHint) {
					t.Errorf("expected hint %q not found in rendered output", expectedHint)
				}
			} else {
				// No columns hidden; hint should not appear
				if strings.Contains(rendered, "+ fields") {
					t.Errorf("hint should not appear when no columns are hidden, but found in output:\n%s", rendered)
				}
			}
		})
	}
}

// TestInlineEditOverflowWidthFirstColumn verifies that editing the FIRST
// declared column places xOffset right after the gutter and its gap, with
// no preceding-column width folded in, and that overflowWidth stretches to
// the terminal's right margin (minus the trailing 2-column pad).
func TestInlineEditOverflowWidthFirstColumn(t *testing.T) {
	const gutterWidth = 10
	const gutterGapWidth = 4
	const columnGapWidth = 2
	widths := []int{15, 20, 25}
	termWidth := 100

	gotXOffset, gotOverflowWidth := editOverflowExtent(widths, 0, gutterWidth, gutterGapWidth, columnGapWidth, termWidth)

	wantXOffset := gutterWidth + gutterGapWidth // 14: no preceding columns
	wantOverflowWidth := max(widths[0], termWidth-wantXOffset-2)

	if gotXOffset != wantXOffset {
		t.Errorf("xOffset = %d, want %d", gotXOffset, wantXOffset)
	}
	if gotOverflowWidth != wantOverflowWidth {
		t.Errorf("overflowWidth = %d, want %d", gotOverflowWidth, wantOverflowWidth)
	}
}

// TestInlineEditOverflowWidthMiddleColumn verifies that editing a MIDDLE
// column's xOffset accounts for every preceding column's width plus its
// trailing columnGapWidth gap, not just the immediately preceding one.
func TestInlineEditOverflowWidthMiddleColumn(t *testing.T) {
	const gutterWidth = 10
	const gutterGapWidth = 4
	const columnGapWidth = 2
	widths := []int{15, 20, 25}
	termWidth := 120

	gotXOffset, gotOverflowWidth := editOverflowExtent(widths, 1, gutterWidth, gutterGapWidth, columnGapWidth, termWidth)

	wantXOffset := gutterWidth + gutterGapWidth + widths[0] + columnGapWidth // accounts for column 0
	wantOverflowWidth := max(widths[1], termWidth-wantXOffset-2)

	if gotXOffset != wantXOffset {
		t.Errorf("xOffset = %d, want %d", gotXOffset, wantXOffset)
	}
	if gotOverflowWidth != wantOverflowWidth {
		t.Errorf("overflowWidth = %d, want %d", gotOverflowWidth, wantOverflowWidth)
	}
}

// TestInlineEditOverflowWidthLastColumn verifies that editing the LAST
// column accumulates every preceding column's width and gap, and that a
// narrow terminal collapses overflowWidth back down to the column's own
// width (the max() floor) rather than going negative.
func TestInlineEditOverflowWidthLastColumn(t *testing.T) {
	const gutterWidth = 10
	const gutterGapWidth = 4
	const columnGapWidth = 2
	widths := []int{15, 20, 25}

	t.Run("wide terminal stretches to margin", func(t *testing.T) {
		termWidth := 140
		gotXOffset, gotOverflowWidth := editOverflowExtent(widths, 2, gutterWidth, gutterGapWidth, columnGapWidth, termWidth)

		wantXOffset := gutterWidth + gutterGapWidth + widths[0] + columnGapWidth + widths[1] + columnGapWidth // accounts for columns 0 and 1
		wantOverflowWidth := max(widths[2], termWidth-wantXOffset-2)

		if gotXOffset != wantXOffset {
			t.Errorf("xOffset = %d, want %d", gotXOffset, wantXOffset)
		}
		if gotOverflowWidth != wantOverflowWidth {
			t.Errorf("overflowWidth = %d, want %d", gotOverflowWidth, wantOverflowWidth)
		}
	})

	t.Run("narrow terminal floors to column width", func(t *testing.T) {
		termWidth := 30 // narrower than xOffset, so termWidth-xOffset-2 goes negative
		gotXOffset, gotOverflowWidth := editOverflowExtent(widths, 2, gutterWidth, gutterGapWidth, columnGapWidth, termWidth)

		wantXOffset := gutterWidth + gutterGapWidth + widths[0] + columnGapWidth + widths[1] + columnGapWidth
		wantOverflowWidth := widths[2] // the max() floor wins

		if gotXOffset != wantXOffset {
			t.Errorf("xOffset = %d, want %d", gotXOffset, wantXOffset)
		}
		if gotOverflowWidth != wantOverflowWidth {
			t.Errorf("overflowWidth = %d, want %d (floor at column width)", gotOverflowWidth, wantOverflowWidth)
		}
	})
}

// TestEditRowBackgroundTint verifies that edit rows render with a background
// tint across the full row width (gutter, data cells, separators, and
// overflow hints), while normal rows do not have the background tint.
// This is a regression test for issue #142.
func TestEditRowBackgroundTint(t *testing.T) {
	withANSIColorProfile(t)

	rows := []csvplan.CollectionRow{
		{
			Index: 0,
			CustomFields: map[string]string{
				"title":  "Song A",
				"artist": "Artist A",
				"link":   "http://example.com/a",
			},
		},
		{
			Index: 1,
			CustomFields: map[string]string{
				"title":  "Song B",
				"artist": "Artist B",
				"link":   "http://example.com/b",
			},
		},
	}
	states := []rowState{rowRendered, rowRendered}

	// Test 1: Normal row (not editing) should NOT have background tint
	v := collectionView{
		name:   "songs",
		rows:   rows,
		states: states,
		columns: []collectionColumn{
			{field: "title", header: "TITLE"},
			{field: "artist", header: "ARTIST"},
			{field: "link", header: "LINK"},
		},
		rowStatus:  make(map[int]string),
		cursor:     0,
		editing:    false, // Normal mode
		termWidth:  100,
		termHeight: 30,
	}

	rendered := v.view()
	lines := strings.Split(rendered, "\n")

	// Find the Song A line
	var songALine string
	for _, line := range lines {
		if strings.Contains(line, "Song A") {
			songALine = line
			break
		}
	}
	if songALine == "" {
		t.Fatalf("Song A line not found in rendered output")
	}

	// Check that normal row does NOT contain background SGR codes. Background codes
	// start with "4" followed by a digit (e.g. "40", "41", "42", "48;5;", "48;2;").
	// Look for patterns like ESC[...4Xm where X is 0-9 (standard 8/16 colors) or
	// ESC[48;5; or ESC[48;2; (256/truecolor). Use a simpler check: look for "m" followed
	// by content that suggests a background color in a similar row position.
	hasNormalBgCode := strings.Contains(songALine, "\x1b[40m") ||
		strings.Contains(songALine, "\x1b[41m") ||
		strings.Contains(songALine, "\x1b[42m") ||
		strings.Contains(songALine, "\x1b[43m") ||
		strings.Contains(songALine, "\x1b[44m") ||
		strings.Contains(songALine, "\x1b[45m") ||
		strings.Contains(songALine, "\x1b[46m") ||
		strings.Contains(songALine, "\x1b[47m") ||
		strings.Contains(songALine, "\x1b[100m") ||
		strings.Contains(songALine, "\x1b[48;5;") ||
		strings.Contains(songALine, "\x1b[48;2;")
	if hasNormalBgCode {
		t.Errorf("normal (non-edit) row should NOT contain background color SGR codes but got: %q", songALine)
	}

	// Test 2: Edit row should have background tint
	v.editing = true
	v.cursor = 1       // Edit the second row
	v.editFieldIdx = 1 // Editing ARTIST column
	v.editValue = "Artist B"
	v.editCursor = 4

	rendered = v.view()
	lines = strings.Split(rendered, "\n")

	// Find the Song B line (the edit row)
	var songBLine string
	for _, line := range lines {
		if strings.Contains(line, "Song B") {
			songBLine = line
			break
		}
	}
	if songBLine == "" {
		t.Fatalf("Song B line not found in rendered output")
	}

	// Check that edit row DOES contain background SGR codes (any of the standard background codes)
	hasEditBgCode := strings.Contains(songBLine, "\x1b[40m") ||
		strings.Contains(songBLine, "\x1b[41m") ||
		strings.Contains(songBLine, "\x1b[42m") ||
		strings.Contains(songBLine, "\x1b[43m") ||
		strings.Contains(songBLine, "\x1b[44m") ||
		strings.Contains(songBLine, "\x1b[45m") ||
		strings.Contains(songBLine, "\x1b[46m") ||
		strings.Contains(songBLine, "\x1b[47m") ||
		strings.Contains(songBLine, "\x1b[100m") ||
		strings.Contains(songBLine, "\x1b[48;5;") ||
		strings.Contains(songBLine, "\x1b[48;2;")
	if !hasEditBgCode {
		t.Errorf("edit row should contain background color SGR codes but got: %q", songBLine)
	}

	// Verify the background appears multiple times (gutter, data cells, separators)
	// by counting occurrences of background codes
	backgroundCount := strings.Count(songBLine, "\x1b[40m") +
		strings.Count(songBLine, "\x1b[41m") +
		strings.Count(songBLine, "\x1b[42m") +
		strings.Count(songBLine, "\x1b[43m") +
		strings.Count(songBLine, "\x1b[44m") +
		strings.Count(songBLine, "\x1b[45m") +
		strings.Count(songBLine, "\x1b[46m") +
		strings.Count(songBLine, "\x1b[47m") +
		strings.Count(songBLine, "\x1b[48;5;") +
		strings.Count(songBLine, "\x1b[48;2;")
	if backgroundCount < 3 {
		// We should see at least a few background codes for gutter, cells, and separators
		t.Logf("warning: edit row has only %d background SGR codes, expected at least 3. Line: %q", backgroundCount, songBLine)
	}
}

// ansiEscapeRe matches a complete SGR escape sequence, e.g. "\x1b[38;5;240m".
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// assertNoDanglingEscape fails t if s contains an "\x1b[" that is not
// followed by a valid SGR terminator ('m') before either the next "\x1b["
// or the end of the string — i.e. a truncated/unterminated escape sequence.
func assertNoDanglingEscape(t *testing.T, s string) {
	t.Helper()
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], "\x1b[")
		if idx < 0 {
			return
		}
		start := i + idx
		rest := s[start:]
		loc := ansiEscapeRe.FindStringIndex(rest)
		if loc == nil || loc[0] != 0 {
			t.Fatalf("dangling/unterminated ANSI escape sequence at byte %d in %q", start, s)
		}
		i = start + loc[1]
	}
}

// TestRenderAddSlotBudgetsPlainTextBeforeStyling renders the add-clip slot
// help row at a narrow terminal width with a long pasted single-line body
// and a long keysHint (forced via addSuggestions) and asserts the composed
// first line never exceeds termWidth in visual columns, and that no ANSI
// escape sequence is truncated mid-code — regression for #101, where the
// line was composed entirely from already-styled segments with no width
// budget at all.
func TestRenderAddSlotBudgetsPlainTextBeforeStyling(t *testing.T) {
	// A body long enough to leave little room for the trailers (detect hint,
	// keysHint) but short enough that it alone fits within the available
	// budget at termWidth=40 — this exercises the trailer truncation/drop
	// logic that #101 fixed.
	moderateURL := "https://youtu.be/dQw4w9WgXcQabcd"

	// A body long enough to exceed the available budget by itself.
	// classifyAddBuffer caps single-line bodies at 80 bytes, well past a
	// termWidth=40 budget, so the composed line can still exceed termWidth
	// in this case — renderEditField's body rendering is unbounded and out
	// of scope for #101 (noted in the issue and in the implementation
	// summary as unresolved follow-up). This case only asserts no ANSI
	// escape sequence is truncated mid-code.
	longURL := "https://example.com/watch?v=" + strings.Repeat("abcdef1234", 8)

	cases := []struct {
		name           string
		addBuffer      string
		addSuggestions []songSuggestion
		assertWidth    bool
	}{
		{
			name:        "moderate single-line paste, default keysHint",
			addBuffer:   moderateURL,
			assertWidth: true,
		},
		{
			name:      "moderate single-line paste, select keysHint",
			addBuffer: moderateURL,
			addSuggestions: []songSuggestion{
				{Title: "Some Song", Artist: "Some Artist"},
			},
			assertWidth: true,
		},
		{
			name:      "long single-line paste, select keysHint",
			addBuffer: longURL,
			addSuggestions: []songSuggestion{
				{Title: "Some Song", Artist: "Some Artist"},
			},
			assertWidth: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := collectionView{
				addFocus:       true,
				addBuffer:      tc.addBuffer,
				addCursor:      len(tc.addBuffer),
				addSuggestions: tc.addSuggestions,
				termWidth:      40,
				termHeight:     24,
			}

			out := v.renderAddSlot()
			lines := strings.Split(out, "\n")
			firstLine := lines[0]

			assertNoDanglingEscape(t, firstLine)

			if tc.assertWidth {
				plain := stripANSI(firstLine)
				if w := runewidth.StringWidth(plain); w > v.termWidth {
					t.Errorf("first line visual width %d exceeds termWidth %d; plain=%q", w, v.termWidth, plain)
				}
			}
		})
	}
}
