package dashboard

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
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

// TestRenderCellStyledMatchesPlainWidth locks in renderCell's padding
// contract: style.Width(width).Render(v) after truncateCollectionValue. The
// pre-fix bug was fmt.Sprintf("%-*s", width, style.Render(v)), which counts
// the ANSI escape bytes injected by style.Render as part of the %-*s width
// budget — so a styled cell renders narrower (visibly) than a plain one for
// the same input, breaking column alignment. This asserts styled and plain
// output are identical once escapes are stripped, for both short (pad) and
// long (truncate) values, including multi-byte/wide runes.
func TestRenderCellStyledMatchesPlainWidth(t *testing.T) {
	values := []string{
		"",
		"short",                             // ASCII shorter than every width: pure padding
		"this is a long ascii string value", // ASCII longer than every width: truncation
		"日本語のとても長い動画タイトルです",        // CJK: truncation can drop a wide rune, leaving a 1-col gap for padding to absorb
		"🎵 Hello there wide chars", // emoji: wide runes mixed with ASCII
		"é́ combining marks",      // combining marks: rune count != visual width
	}
	widths := []int{5, 10, 20}

	// A renderer bound explicitly to the ANSI profile guarantees styled
	// output actually carries escape codes, independent of whether this
	// test binary's stdout is a tty (lipgloss's default renderer lazily
	// detects color support from os.Stdout and would otherwise silently
	// downgrade to plain text under `go test`).
	ansiRenderer := lipgloss.NewRenderer(io.Discard)
	ansiRenderer.SetColorProfile(termenv.ANSI)
	plain := lipgloss.Style{}
	styled := ansiRenderer.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	for _, v := range values {
		for _, w := range widths {
			gotPlain := renderCell(v, w, plain)
			gotStyled := renderCell(v, w, styled)

			if !strings.Contains(gotStyled, "\x1b") {
				t.Fatalf("renderCell(%q, %d, styled) produced no ANSI escapes; test is not exercising the styled path (out: %q)", v, w, gotStyled)
			}

			strippedPlain := stripANSI(gotPlain)
			strippedStyled := stripANSI(gotStyled)

			if strippedPlain != strippedStyled {
				t.Fatalf("renderCell(%q, %d) visible text mismatch:\nplain:  %q\nstyled: %q", v, w, strippedPlain, strippedStyled)
			}

			plainWidth := runewidth.StringWidth(strippedPlain)
			styledWidth := runewidth.StringWidth(strippedStyled)
			if plainWidth != w {
				t.Errorf("renderCell(%q, %d, plain) visible width = %d, want %d", v, w, plainWidth, w)
			}
			if styledWidth != w {
				t.Errorf("renderCell(%q, %d, styled) visible width = %d, want %d", v, w, styledWidth, w)
			}
		}
	}
}

// TestRenderEditFieldEmpty tests renderEditField on an empty string
func TestRenderEditFieldEmpty(t *testing.T) {
	value := ""
	cursor := 0

	got := renderEditField(value, cursor)

	want := editStyle.Render("") + cursorCharStyle.Render(" ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditFieldCursorAtStart tests cursor at position 0 in a non-empty string
func TestRenderEditFieldCursorAtStart(t *testing.T) {
	value := "hello"
	cursor := 0

	got := renderEditField(value, cursor)

	want := editStyle.Render("") +
		cursorCharStyle.Render("h") +
		editStyle.Render("ello")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditFieldCursorAtMiddle tests cursor in the middle of a string
func TestRenderEditFieldCursorAtMiddle(t *testing.T) {
	value := "hello"
	cursor := 2 // position in "he[l]lo"

	got := renderEditField(value, cursor)

	want := editStyle.Render("he") +
		cursorCharStyle.Render("l") +
		editStyle.Render("lo")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditFieldCursorAtEnd tests cursor at position equal to len(value)
func TestRenderEditFieldCursorAtEnd(t *testing.T) {
	value := "hello"
	cursor := len(value)

	got := renderEditField(value, cursor)

	want := editStyle.Render("hello") + cursorCharStyle.Render(" ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditFieldCursorPastEnd tests cursor beyond the string length
func TestRenderEditFieldCursorPastEnd(t *testing.T) {
	value := "hello"
	cursor := len(value) + 10 // way past the end

	got := renderEditField(value, cursor)

	want := editStyle.Render("hello") + cursorCharStyle.Render(" ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditFieldCursorNegative tests negative cursor (should clamp to 0)
func TestRenderEditFieldCursorNegative(t *testing.T) {
	value := "hello"
	cursor := -5

	got := renderEditField(value, cursor)

	want := editStyle.Render("") +
		cursorCharStyle.Render("h") +
		editStyle.Render("ello")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditFieldUnicodeEmoji tests multi-byte emoji as a single rune
func TestRenderEditFieldUnicodeEmoji(t *testing.T) {
	value := "🎬 hello"
	cursor := len("🎬") // byte offset just before the space

	got := renderEditField(value, cursor)

	want := editStyle.Render("🎬") +
		cursorCharStyle.Render(" ") +
		editStyle.Render("hello")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditFieldCJK tests CJK characters (3 bytes each)
func TestRenderEditFieldCJK(t *testing.T) {
	value := "日本語"
	cursor := len("日本") // byte offset just before 語

	got := renderEditField(value, cursor)

	want := editStyle.Render("日本") +
		cursorCharStyle.Render("語") +
		editStyle.Render("")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellWidth0 tests edge case: width = 0
func TestRenderEditCellWidth0(t *testing.T) {
	value := "hello"
	cursor := 0
	width := 0

	got := renderEditCell(value, cursor, width)

	if got != "" {
		t.Fatalf("got %q, want empty string", got)
	}
}

// TestRenderEditCellWidth1 tests edge case: width = 1
func TestRenderEditCellWidth1(t *testing.T) {
	value := "hello"
	cursor := 0
	width := 1

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// When "hello" is truncated to width 1, it becomes "…" and cursor at 0 lands on the ellipsis
	want := cursorCharStyle.Render("…")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellWidth2 tests edge case: width = 2
func TestRenderEditCellWidth2(t *testing.T) {
	value := "hello"
	cursor := 0
	width := 2

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// "hello" truncated to width 2 becomes "h…", cursor at 0 lands on 'h'
	want := cursorCharStyle.Render("h") +
		editStyle.Render("…")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellCursorAtStart0Width tests cursor at 0 in a value shorter than width
func TestRenderEditCellCursorAtStart0Width(t *testing.T) {
	value := "hi"
	cursor := 0
	width := 6

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// "hi" + 4 spaces, cursor on 'h'
	want := cursorCharStyle.Render("h") +
		editStyle.Render("i    ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellCursorAtMiddle tests cursor in middle of value shorter than width
func TestRenderEditCellCursorAtMiddle(t *testing.T) {
	value := "hi"
	cursor := 1
	width := 6

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// cursor on 'i'
	want := editStyle.Render("h") +
		cursorCharStyle.Render("i") +
		editStyle.Render("    ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellCursorAtPadding tests cursor on trailing padding (end-of-string position)
func TestRenderEditCellCursorAtPadding(t *testing.T) {
	value := "hi"
	cursor := len(value)
	width := 6

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// cursor = 2 (len("hi"))
	// cursorCol = runewidth.StringWidth("hi") = 2
	// !truncated and cursorCol (2) not > width-1 (5), so stays at 2
	// content = ['h', 'i', ' ', ' ', ' ', ' ']
	// Loop finds cursor at col 2, which is the first padding space
	// Total width should be exactly 6 characters after stripping ANSI codes
	stripped := stripANSI(got)
	if stripped != "hi    " {
		t.Fatalf("visible text = %q, want %q (len=%d, want len=6)", stripped, "hi    ", len(stripped))
	}
}

// TestRenderEditCellValueLongerTruncation tests value longer than width (truncation)
func TestRenderEditCellValueLongerTruncation(t *testing.T) {
	value := "this is a long string"
	cursor := 0
	width := 8

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// "this is a long string" truncated to width 8 becomes "this is…"
	// cursor at 0 lands on 't'
	want := cursorCharStyle.Render("t") +
		editStyle.Render("his is…")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellCursorPastTruncation tests cursor beyond truncation cut
func TestRenderEditCellCursorPastTruncation(t *testing.T) {
	value := "this is a long string"
	cursor := len("this is a ") // byte offset past the truncation cut
	width := 8

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// Truncated value is "this is…", cursor at byte 10 (past the cut) lands on ellipsis
	// cursorCol = runewidth.StringWidth("this is a") = 9 (past the content)
	// Since truncated and cursorCol (9) > contentWidth-1 (7), cursorCol is clamped to 7 (the ellipsis)
	want := editStyle.Render("this is") +
		cursorCharStyle.Render("…")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellMultibyteUnicodeInMiddleOfValue tests multi-byte character with cursor positioned within it (or adjacent)
func TestRenderEditCellMultibyteUnicodeInMiddleOfValue(t *testing.T) {
	value := "abc🎬def"
	cursor := len("abc🎬") // byte offset just before 'd'
	width := 10

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// abc🎬 (visual width 5) + d (1) + ef (2) = 8 total, padded to 10
	want := editStyle.Render("abc🎬") +
		cursorCharStyle.Render("d") +
		editStyle.Render("ef  ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellByteCursorVsVisualColumn tests that byte cursor offset is
// correctly converted to visual column. This is critical for multi-byte Unicode
// where byte offset != visual column (e.g., emoji is 4 bytes but 2 visual columns).
// If cursorCol is incorrectly computed as byte offset instead of visual columns,
// the cursor lands on the wrong character.
func TestRenderEditCellByteCursorVsVisualColumn(t *testing.T) {
	// Emoji (4 bytes, 2 visual columns) + 'A' (1 byte, 1 visual column)
	// Cursor at byte offset 5 = after "🎬A"
	// Visual column should be 3 (emoji=2, A=1), so cursor should be on 'B'
	// But if byte offset is used as column, cursor would be on column 5
	value := "🎬AB"
	cursor := len("🎬A") // 4 + 1 = 5 bytes
	width := 6

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// When cursor is correctly on 'B' at visual column 3:
	// editStyle.Render("🎬A") + cursorCharStyle.Render("B") + editStyle.Render("  ")
	want := editStyle.Render("🎬A") + cursorCharStyle.Render("B") + editStyle.Render("  ")
	if got != want {
		t.Fatalf("cursor position incorrect.\ngot %q\nwant %q", got, want)
	}
}

