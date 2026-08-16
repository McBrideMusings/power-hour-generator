package dashboard

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"powerhour/internal/tui"
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

	truncated := runewidth.StringWidth(value) > width
	content := truncateRunesToWidth(value, width)
	contentWidth := runewidth.StringWidth(string(content))
	if pad := width - contentWidth; pad > 0 {
		content = append(content, []rune(strings.Repeat(" ", pad))...)
	}

	// cursorCol is the visual column the byte cursor sits at in the
	// (untruncated) value, so a wide rune before the cursor pushes it right
	// by its real width rather than by one rune.
	cursorCol := runewidth.StringWidth(value[:cursor])
	if truncated {
		// Past the truncation cut: land on the ellipsis cell, not the
		// trailing pad that fills a dropped-wide-rune gap.
		if cursorCol > contentWidth-1 {
			cursorCol = contentWidth - 1
		}
	} else if cursorCol > width-1 {
		// End-of-string on an untruncated value still falls through to a
		// trailing pad cell (typing position after the last character).
		cursorCol = width - 1
	}

	idx := len(content) - 1
	col := 0
	for i, r := range content {
		rw := runewidth.RuneWidth(r)
		if cursorCol >= col && cursorCol < col+rw {
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

// truncateRunesToWidth returns value's runes, truncated with a trailing '…'
// so the result's visual width does not exceed width. A wide rune that would
// straddle the cut is dropped rather than split, which can leave the result
// one column narrower than width — the caller pads to make up the gap.
func truncateRunesToWidth(value string, width int) []rune {
	return []rune(tui.TruncateToWidth(value, width, tui.TruncateOptions{
		Ellipsis:          "…",
		EllipsisWhenTight: true,
	}))
}
