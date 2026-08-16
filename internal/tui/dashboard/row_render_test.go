package dashboard

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestRenderEditCellEmojiPadsToVisualWidth exercises a wide emoji preceding
// the cursor. Budgeting by rune count (the pre-fix behaviour) pads by
// len(runes) rather than visual width, so a cell with a 2-column emoji
// overflows its fixed width. It also checks that the reverse-video
// highlight lands on the byte-offset cursor's actual character.
func TestRenderEditCellEmojiPadsToVisualWidth(t *testing.T) {
	value := "🎬AB"
	width := 6
	cursor := len("🎬A") // byte offset just before 'B'

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	want := editStyle.Render("🎬A") + cursorCharStyle.Render("B") + editStyle.Render("  ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellCJKPadsToVisualWidth mirrors the emoji case with
// full-width CJK characters (2 terminal columns, 3 UTF-8 bytes each).
func TestRenderEditCellCJKPadsToVisualWidth(t *testing.T) {
	value := "日本AB"
	width := 8
	cursor := len("日本") // byte offset just before 'A'

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	want := editStyle.Render("日本") + cursorCharStyle.Render("A") + editStyle.Render("B  ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellCJKTruncationCursorOnEllipsis covers the case where a
// wide rune straddles the truncation cut, leaving a 1-column gap that
// padding must absorb, and a byte cursor past the cut must land on the
// ellipsis cell rather than the trailing pad.
func TestRenderEditCellCJKTruncationCursorOnEllipsis(t *testing.T) {
	value := "日本語ABCDEF" // 3 CJK (6 cols) + 6 ASCII (6 cols) = 12 cols total
	width := 6
	cursor := len("日本語") // byte offset just before 'A', past the truncation cut

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// budget = width-1 = 5 columns kept before the ellipsis: 日(2)+本(2) = 4
	// columns fit, 語(2) would push to 6 > 5 so it's dropped, leaving a
	// 1-column gap that the trailing pad space fills.
	want := editStyle.Render("日本") + cursorCharStyle.Render("…") + editStyle.Render(" ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellEmojiExactFitNoTruncation checks the untruncated,
// unpadded boundary case: content visual width equals the cell width
// exactly, so no pad and no truncation are needed.
func TestRenderEditCellEmojiExactFitNoTruncation(t *testing.T) {
	value := "🎬🎬🎬" // 3 runes, 6 columns
	width := 6
	cursor := len("🎬🎬") // byte offset just before the third emoji

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	want := editStyle.Render("🎬🎬") + cursorCharStyle.Render("🎬")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
