package dashboard

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var cursorCharStyle = lipgloss.NewStyle().Reverse(true)

// renderCell truncates plain text to width, pads it to width, then applies the
// style. Uses lipgloss Width() for padding so Unicode values don't misalign columns.
func renderCell(value string, width int, style lipgloss.Style) string {
	v := truncateCollectionValue(value, width)
	return style.Width(width).Render(v)
}

func renderRow(cells ...string) string {
	return strings.Join(cells, "  ")
}

// renderEditField renders text with reverse-video cursor highlighting. cursor
// is a byte offset; past end-of-string the cursor renders as a trailing space.
// No truncation or padding — use renderEditCell for fixed-width cells.
func renderEditField(value string, cursor int) string {
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(value) {
		return editStyle.Render(value) + cursorCharStyle.Render(" ")
	}
	_, size := utf8.DecodeRuneInString(value[cursor:])
	return editStyle.Render(value[:cursor]) +
		cursorCharStyle.Render(value[cursor:cursor+size]) +
		editStyle.Render(value[cursor+size:])
}

// renderEditCell renders value in a fixed-width edit cell with reverse-video
// cursor highlighting. cursor is a byte offset into value. width is a visual
// column count (via go-runewidth), so wide runes (CJK, most emoji) are
// budgeted at their real terminal width instead of one rune each.
//
// When value is wider than width, the visible window slides to keep the
// cursor on screen: it centers on the cursor's visual column (clamped so it
// never scrolls before the start or past the end of value), rather than
// always showing value's first width columns with the cursor pinned to
// whichever edge cell it overflowed into. That fixed-at-column-0 window was
// #133/#139 — a cursor anywhere past width rendered identically, with no
// signal of where in the value it actually sat or what surrounded it.
func renderEditCell(value string, cursor int, width int) string {
	if width <= 0 {
		return ""
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}

	// cursorCol is the visual column the byte cursor sits at in value, so a
	// wide rune before the cursor pushes it right by its real width rather
	// than by one rune.
	cursorCol := runewidth.StringWidth(value[:cursor])
	totalWidth := runewidth.StringWidth(value)

	startCol := 0
	if totalWidth > width {
		startCol = cursorCol - width/2
		if maxStart := totalWidth - width; startCol > maxStart {
			startCol = maxStart
		}
		if startCol < 0 {
			startCol = 0
		}
	}

	content, actualStart := sliceRunesByColumn(value, startCol, width)
	contentWidth := runewidth.StringWidth(string(content))
	if pad := width - contentWidth; pad > 0 {
		content = append(content, []rune(strings.Repeat(" ", pad))...)
	}

	// windowCursorCol is the cursor's column relative to the window's actual
	// start (which can land after the requested startCol when a wide rune
	// straddling that boundary gets dropped rather than split), clamped to
	// the last visible cell so a cursor at end-of-string or on a dropped
	// wide rune still renders on screen instead of past the padded content.
	windowCursorCol := cursorCol - actualStart
	if windowCursorCol < 0 {
		windowCursorCol = 0
	}
	if windowCursorCol > width-1 {
		windowCursorCol = width - 1
	}

	idx := len(content) - 1
	col := 0
	for i, r := range content {
		rw := runewidth.RuneWidth(r)
		if windowCursorCol >= col && windowCursorCol < col+rw {
			idx = i
			break
		}
		col += rw
	}
	if idx < 0 {
		idx = 0
	}

	return editStyle.Render(string(content[:idx])) +
		cursorCharStyle.Render(string(content[idx:idx+1])) +
		editStyle.Render(string(content[idx+1:]))
}

// sliceRunesByColumn returns the runes of value whose visual columns fall
// within [startCol, startCol+width) — the sliding viewport used by
// renderEditCell — plus actualStart, the visual column the returned runes
// actually begin at. actualStart matches startCol unless a wide rune
// straddles that boundary; such a rune is dropped rather than split
// (mirroring the drop-not-split rule at the trailing edge), which pushes the
// real start one column later than requested. Callers must measure cursor
// position against actualStart, not startCol, or the cursor highlight lands
// one cell off whenever that drop happens.
func sliceRunesByColumn(value string, startCol, width int) (runes []rune, actualStart int) {
	endCol := startCol + width
	actualStart = -1
	col := 0
	for _, r := range value {
		rw := runewidth.RuneWidth(r)
		if col >= startCol {
			if col+rw > endCol {
				break
			}
			if actualStart < 0 {
				actualStart = col
			}
			runes = append(runes, r)
		}
		col += rw
	}
	if actualStart < 0 {
		actualStart = startCol
	}
	return runes, actualStart
}
