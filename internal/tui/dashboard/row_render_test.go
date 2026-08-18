package dashboard

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// withANSIColorProfile forces the shared default lipgloss renderer's color
// profile to ANSI for the duration of one test, then restores whatever
// profile was active before. editStyle and cursorCharStyle (row_render.go)
// are built via lipgloss.NewStyle(), which binds them to that package-level
// default renderer. Under `go test`, stdout isn't a TTY, so the renderer
// auto-detects no-color and Style.Render degrades to the identity function
// — every escape code disappears. That collapses got/want comparisons in
// the cursor-position tests below to plain string concatenation regardless
// of where the byte-cursor-to-visual-column split actually landed, letting
// a byte-offset-vs-visual-column regression in renderEditCell/renderEditField
// pass undetected. Forcing ANSI here makes editStyle/cursorCharStyle emit
// real SGR codes for the test, so the comparison becomes genuinely
// sensitive to where the split happens. Scoped per-test (rather than via
// TestMain) so it doesn't leak into other test files in this package that
// assert on plain, unstyled output.
func withANSIColorProfile(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(prev)
	})
}

// TestRenderEditCellEmojiPadsToVisualWidth exercises a wide emoji preceding
// the cursor. Budgeting by rune count (the pre-fix behaviour) pads by
// len(runes) rather than visual width, so a cell with a 2-column emoji
// overflows its fixed width. It also checks that the reverse-video
// highlight lands on the byte-offset cursor's actual character.
func TestRenderEditCellEmojiPadsToVisualWidth(t *testing.T) {
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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

// TestRenderEditCellCJKTruncationCursorOnEllipsis covers a wide rune
// straddling the sliding window's left edge: the window is requested to
// start mid-rune, that rune is dropped rather than split, and the cursor
// highlight must track the window's actual (post-drop) start column, not
// the requested one, or it lands one cell off.
func TestRenderEditCellCJKTruncationCursorOnEllipsis(t *testing.T) {
	withANSIColorProfile(t)
	value := "日本語ABCDEF" // 3 CJK (6 cols) + 6 ASCII (6 cols) = 12 cols total
	width := 6
	cursor := len("日本語") // byte offset just before 'A'

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// cursorCol = width("日本語") = 6. Requested startCol = 6 - width/2 = 3,
	// which falls inside 本's occupied columns [2,4) — 本 is dropped rather
	// than split, so the window actually starts at column 4 (語). The
	// cursor (col 6, i.e. 語's end / A's start) lands on 'A'.
	want := editStyle.Render("語") + cursorCharStyle.Render("A") + editStyle.Render("BC ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellEmojiExactFitNoTruncation checks the untruncated,
// unpadded boundary case: content visual width equals the cell width
// exactly, so no pad and no truncation are needed.
func TestRenderEditCellEmojiExactFitNoTruncation(t *testing.T) {
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
	value := "hello"
	cursor := 0
	width := 1

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// width 1 with cursor at column 0: the window is just "h", cursor on it.
	want := cursorCharStyle.Render("h")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellWidth2 tests edge case: width = 2
func TestRenderEditCellWidth2(t *testing.T) {
	withANSIColorProfile(t)
	value := "hello"
	cursor := 0
	width := 2

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// width 2 with cursor at column 0: window is "he", cursor on 'h'.
	want := cursorCharStyle.Render("h") +
		editStyle.Render("e")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellCursorAtStart0Width tests cursor at 0 in a value shorter than width
func TestRenderEditCellCursorAtStart0Width(t *testing.T) {
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
	value := "this is a long string"
	cursor := 0
	width := 8

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// cursor at column 0 keeps the window pinned to the start: "this is ",
	// cursor on 't'.
	want := cursorCharStyle.Render("t") +
		editStyle.Render("his is ")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellCursorPastTruncation tests a cursor positioned past what
// a fixed [0:width] window would show: the pre-fix behaviour rendered the
// same clamped-to-edge cursor for any such position, with no signal of
// where in the value it actually was. The sliding window instead scrolls to
// center on the cursor, so the visible substring changes and shows the
// characters actually around the cursor.
func TestRenderEditCellCursorPastTruncation(t *testing.T) {
	withANSIColorProfile(t)
	value := "this is a long string"
	cursor := len("this is a ") // byte offset just before 'l' of "long"
	width := 8

	got := renderEditCell(value, cursor, width)

	if w := lipgloss.Width(got); w != width {
		t.Fatalf("rendered width = %d, want %d (out: %q)", w, width, got)
	}

	// cursorCol = 10. startCol = 10 - width/2 = 6, window covers columns
	// [6,14): "s a long". Cursor at col 10 relative to the window (col 4)
	// lands on 'l', the start of "long" — not the value's head.
	want := editStyle.Render("s a ") +
		cursorCharStyle.Render("l") +
		editStyle.Render("ong")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestRenderEditCellMultibyteUnicodeInMiddleOfValue tests multi-byte character with cursor positioned within it (or adjacent)
func TestRenderEditCellMultibyteUnicodeInMiddleOfValue(t *testing.T) {
	withANSIColorProfile(t)
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
	withANSIColorProfile(t)
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

// TestRenderEditCellSlidingWindowTracksCursor is the #133/#139 regression
// test. Pre-fix, renderEditCell always rendered value's fixed [0:width]
// window: once the cursor moved past width, every further cursor position
// rendered the identical clamped-to-edge output, giving no signal of where
// in the value the cursor actually was. Walking the cursor from 0 to the
// end of a value much longer than width must produce genuinely different
// visible substrings, each containing the character at the cursor.
func TestRenderEditCellSlidingWindowTracksCursor(t *testing.T) {
	withANSIColorProfile(t)
	value := "abcdefghijklmnopqrstuvwxyz" // 26 cols, well past width
	width := 10

	type sample struct {
		cursor      int
		wantVisible string // substring the window must contain
	}
	samples := []sample{
		{cursor: 0, wantVisible: "abcdefghij"},  // window pinned to the start
		{cursor: 13, wantVisible: "ijklmnopqr"}, // window centered on the cursor
		{cursor: 26, wantVisible: "qrstuvwxyz"}, // window pinned to the end (the tail)
	}

	var visibles []string
	for _, s := range samples {
		got := renderEditCell(value, s.cursor, width)
		visible := stripANSI(got)
		visibles = append(visibles, visible)
		if visible != s.wantVisible {
			t.Errorf("cursor=%d: visible window = %q, want %q", s.cursor, visible, s.wantVisible)
		}
	}

	// The core sliding-window property: distinct cursor positions past
	// width must show distinct content, not the same clamped-in-place
	// window every time.
	if visibles[0] == visibles[1] || visibles[1] == visibles[2] || visibles[0] == visibles[2] {
		t.Fatalf("expected all three windows to differ, got %q, %q, %q", visibles[0], visibles[1], visibles[2])
	}
}

// TestRenderEditCellSlidingWindowShowsTailAtEnd asserts that with the cursor
// at end-of-value (the common case while typing at the end of a long field),
// the visible window shows the value's tail — the characters immediately
// before the cursor — not its head. A fixed [0:width] window would show the
// head regardless of where the cursor is.
func TestRenderEditCellSlidingWindowShowsTailAtEnd(t *testing.T) {
	withANSIColorProfile(t)
	value := "the quick brown fox jumps over the lazy dog" // 44 cols
	width := 12
	cursor := len(value)

	got := renderEditCell(value, cursor, width)
	visible := stripANSI(got)

	wantTail := value[len(value)-width:] // last `width` columns of value (ASCII: 1 byte = 1 col)
	if visible != wantTail {
		t.Fatalf("visible window = %q, want tail %q", visible, wantTail)
	}
	if strings.HasPrefix(visible, "the quick") {
		t.Fatalf("visible window still shows the head of value: %q", visible)
	}
}
