package dashboard

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"powerhour/pkg/csvplan"
)

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
			columns: []collectionColumn{},
			editFieldIdx: -1,
		}
		row := csvplan.CollectionRow{Index: tt.rowIdx}

		got := editContextNote(v, row)
		if !strings.Contains(got, tt.want) {
			t.Errorf("editContextNote() with index %d missing %q in: %q", tt.rowIdx, tt.want, got)
		}
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
