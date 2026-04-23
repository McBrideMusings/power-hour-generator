package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderCell truncates plain text to width, pads it to width, then applies the
// style. Style-last is the critical part: fmt's %-*s counts bytes, not visual
// width, so padding ANSI-wrapped strings produces the wrong column width.
func renderCell(value string, width int, style lipgloss.Style) string {
	v := truncateCollectionValue(value, width)
	return style.Render(fmt.Sprintf("%-*s", width, v))
}

// renderRow joins cells with a two-space separator.
func renderRow(cells ...string) string {
	return strings.Join(cells, "  ")
}
