package dashboard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"powerhour/internal/tui"
	"powerhour/pkg/csvplan"
)

func TestGutterStatusPaddingWithVisualWidth(t *testing.T) {
	tests := []struct {
		name          string
		status        string
		statusWidth   int
		expectedWidth int
	}{
		{"ASCII status", "cached", 5, 5},
		{"ASCII status short", "---", 5, 5},
		{"ASCII status render", "render", 5, 5},
		{"emoji status", "🎵 ok", 5, 5},
		{"wide char status", "日本語", 5, 5},
		{"mixed emoji and text", "✓ done", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the padding logic from the fix
			truncatedStatus := tui.TruncateWithEllipsis(tt.status, tt.statusWidth)
			paddedStatus := lipgloss.NewStyle().Width(tt.statusWidth).Render(truncatedStatus)

			// Verify the padded status has the correct visual width
			visualWidth := lipgloss.Width(paddedStatus)
			if visualWidth != tt.expectedWidth {
				t.Errorf("paddedStatus visual width = %d, want %d (status=%q, truncated=%q, rendered=%q)",
					visualWidth, tt.expectedWidth, tt.status, truncatedStatus, paddedStatus)
			}
		})
	}
}

func TestGutterGutterFormatConsistency(t *testing.T) {
	// Test that the gutter format consistently produces stable visual width
	// across different status strings (ASCII, emoji, wide chars)
	statusTests := []string{
		"-----",
		"cached",
		"render",
		"🎵",      // single emoji
		"✓ ok",    // emoji with text
		"日本語",  // wide characters
		"📺 test", // emoji and ASCII
	}

	statusWidth := 5

	for _, status := range statusTests {
		t.Run(status, func(t *testing.T) {
			// Simulate the gutter construction:
			// cursor (2 visual width) + idx (2 visual width) + space (1) + paddedStatus (5 visual width)
			truncatedStatus := tui.TruncateWithEllipsis(status, statusWidth)
			paddedStatus := lipgloss.NewStyle().Width(statusWidth).Render(truncatedStatus)

			// The gutter format is: "%s%s %s" where cursor is 2 chars, idx is 2 chars, paddedStatus is statusWidth
			cursor := "  " // either "  " or "▸ " both 2 chars
			idx := "01"    // 2 chars

			// Construct gutter the same way as in the actual code
			gutter := cursor + idx + " " + paddedStatus

			// Verify the gutter has stable visual width
			expectedGutterWidth := 2 + 2 + 1 + statusWidth // cursor + idx + space + paddedStatus
			actualGutterWidth := lipgloss.Width(gutter)

			if actualGutterWidth != expectedGutterWidth {
				t.Errorf("gutter visual width = %d, want %d (status=%q, paddedStatus=%q, gutter=%q)",
					actualGutterWidth, expectedGutterWidth, status, paddedStatus, gutter)
			}
		})
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
