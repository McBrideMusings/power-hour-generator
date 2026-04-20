package dashboard

import (
	"strings"

	"powerhour/internal/tui"

	"github.com/charmbracelet/lipgloss"
)

// helpRowPrefix is the shared column/prefix every inline help row uses:
// two spaces of gutter, a plus marker, then one space before the payload.
// All views (collection, cache, timeline) render their footer through this
// constant so the help row reads the same regardless of view or mode.
const helpRowPrefix = "  + "

// helpRowText renders a single-line inline help row at the standard column
// with the standard "+ " marker, truncated to the terminal width and
// optionally styled with the given lipgloss style. Pass `lipgloss.Style{}`
// (zero value) for plain text.
//
// This is the single source of truth for how the "what can I do right now"
// footer is rendered. Callers decide what text and style to supply; the
// helper owns positioning, prefix, and truncation.
func helpRowText(text string, style lipgloss.Style, termWidth int) string {
	width := termWidth - len(helpRowPrefix)
	if width < 12 {
		width = 12
	}
	truncated := tui.TruncateWithEllipsis(strings.TrimSpace(text), width)
	if truncated == "" {
		return ""
	}
	return helpRowPrefix + style.Render(truncated)
}
