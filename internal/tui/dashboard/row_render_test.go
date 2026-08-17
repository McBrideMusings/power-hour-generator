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
