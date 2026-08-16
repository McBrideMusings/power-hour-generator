package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"powerhour/internal/tui"
)

// headerSegment is a piece of the header line: plain text plus the style
// used to render it. Truncation operates on the plain text so ANSI escape
// bytes from an already-rendered style are never sliced through.
type headerSegment struct {
	text  string
	style lipgloss.Style
}

// renderHeader produces a 2-line header:
//
//	POWER HOUR │ 1:Timeline  2:Songs  3:Intros │ songs: 18/20 cached │ ⚠ yt-dlp
//	/full/project/path
func renderHeader(m Model) string {
	var segments []headerSegment

	segments = append(segments, headerSegment{"POWER HOUR", headerProject})
	segments = append(segments, headerSegment{" │ ", headerSep})

	// View tabs with number keys.
	for i, name := range m.viewNames {
		if i > 0 {
			segments = append(segments, headerSegment{"  ", lipgloss.NewStyle()})
		}
		label := fmt.Sprintf("%d:%s", i+1, name)
		if i == m.activeView {
			segments = append(segments, headerSegment{label, headerTabActive})
		} else {
			segments = append(segments, headerSegment{label, headerTab})
		}
	}

	// Collection status counts.
	segments = append(segments, headerSegment{" │ ", headerSep})
	first := true
	for _, name := range m.collectionNames {
		s := m.summaries[name]
		if !first {
			segments = append(segments, headerSegment{"  ", lipgloss.NewStyle()})
		}
		first = false

		segments = append(segments, headerSegment{fmt.Sprintf("%s: ", name), lipgloss.NewStyle()})
		segments = append(segments, headerSegment{fmt.Sprintf("%d", s.Cached), countGreen})
		segments = append(segments, headerSegment{fmt.Sprintf("/%d", s.Total), lipgloss.NewStyle()})
	}

	// Tool health badge.
	if m.toolWarning != "" {
		segments = append(segments, headerSegment{" │ ", headerSep})
		segments = append(segments, headerSegment{"⚠ " + m.toolWarning, countYellow})
	}

	line1 := renderSegmentsWithBudget(segments, m.termWidth)

	projectPath := m.pp.Root
	if projectPath == "" {
		projectPath = filepath.Clean(".")
	}
	if m.termWidth > 0 {
		projectPath = truncateCollectionValue(projectPath, m.termWidth)
	}

	var b strings.Builder
	b.WriteString(line1)
	b.WriteByte('\n')
	b.WriteString(faint.Render(projectPath))

	return b.String()
}

// renderSegmentsWithBudget renders each segment's styled text in order,
// stopping once the running visual-width budget (termWidth) is exhausted.
// The segment that would overflow the remaining budget is truncated with
// the shared ellipsis helper instead of being dropped outright; segments
// after it are omitted entirely. A non-positive width renders every segment
// untruncated (used directly by tests and any width==0 call site).
func renderSegmentsWithBudget(segments []headerSegment, width int) string {
	var b strings.Builder

	if width <= 0 {
		for _, seg := range segments {
			b.WriteString(seg.style.Render(seg.text))
		}
		return b.String()
	}

	remaining := width
	for _, seg := range segments {
		length := runewidth.StringWidth(seg.text)
		if length <= remaining {
			b.WriteString(seg.style.Render(seg.text))
			remaining -= length
			continue
		}
		if remaining > 0 {
			truncated := tui.TruncateWithEllipsis(seg.text, remaining)
			if truncated != "" {
				b.WriteString(seg.style.Render(truncated))
			}
		}
		break
	}

	return b.String()
}
