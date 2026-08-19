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
	width := max(termWidth-len(helpRowPrefix), 12)
	truncated := tui.TruncateWithEllipsis(strings.TrimSpace(text), width)
	if truncated == "" {
		return ""
	}
	return helpRowPrefix + style.Render(truncated)
}

// helpRowSource is one candidate line in a view's help-row priority ladder,
// as consumed by resolveHelpRow. An empty (or whitespace-only) text means
// "not applicable right now" — resolveHelpRow skips it and falls through to
// the next source.
type helpRowSource struct {
	text  string
	style lipgloss.Style
}

// resolveHelpRow is the shared priority-ladder selector behind every view's
// renderHelpRow: it walks sources in order and renders (via helpRowText)
// the first one whose text is non-empty after trimming. Every view's ladder
// — confirm-delete, transient row note, inline-edit context, default hint —
// reduces to an ordered []helpRowSource; passing the higher-priority source
// first and the default hint last reproduces the original if/else chain.
//
// Not every branch fits the (text, style) shape: a focused add-slot can
// render multiple lines (input + suggestions + a dynamic hint), which
// helpRowText's single-line truncation can't represent. That branch stays
// view-local — pass it as fallback, called only when no source matched, so
// its result is returned as-is with no further processing. Pass nil when a
// view's ladder always bottoms out in a non-empty default source.
func resolveHelpRow(termWidth int, fallback func() string, sources ...helpRowSource) string {
	for _, s := range sources {
		if strings.TrimSpace(s.text) != "" {
			return helpRowText(s.text, s.style, termWidth)
		}
	}
	if fallback != nil {
		return fallback()
	}
	return ""
}

// noteStyleFor picks the style for a transient row-status note: error notes
// render in errorNoteStyle, everything else in editStyle. Shared by every
// view that surfaces inlineRowNote output in its help row.
func noteStyleFor(rawStatus string) lipgloss.Style {
	if isErrorRowNote(rawStatus) {
		return errorNoteStyle
	}
	return editStyle
}
