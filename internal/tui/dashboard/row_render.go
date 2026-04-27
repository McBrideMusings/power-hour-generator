package dashboard

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

var cursorCharStyle = lipgloss.NewStyle().Reverse(true)

// renderCell truncates plain text to width, pads it to width, then applies the
// style. Style-last is the critical part: fmt's %-*s counts bytes, not visual
// width, so padding ANSI-wrapped strings produces the wrong column width.
func renderCell(value string, width int, style lipgloss.Style) string {
	v := truncateCollectionValue(value, width)
	return style.Render(fmt.Sprintf("%-*s", width, v))
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
// cursor highlighting. cursor is a byte offset into value.
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

	runes := []rune(value)
	runeOffset := utf8.RuneCountInString(value[:cursor])

	if len(runes) > width {
		runes = append(runes[:width-1], '…')
		if runeOffset > width-1 {
			runeOffset = width - 1
		}
	}
	if pad := width - len(runes); pad > 0 {
		runes = append(runes, []rune(strings.Repeat(" ", pad))...)
	}
	// Cursor at end-of-string for a full-width value falls on the last cell.
	if runeOffset >= len(runes) {
		runeOffset = len(runes) - 1
	}

	return editStyle.Render(string(runes[:runeOffset])) +
		cursorCharStyle.Render(string(runes[runeOffset:runeOffset+1])) +
		editStyle.Render(string(runes[runeOffset+1:]))
}
