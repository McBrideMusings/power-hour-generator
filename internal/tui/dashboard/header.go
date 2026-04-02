package dashboard

import (
	"fmt"
	"strings"
)

// renderHeader produces the 1-line header bar:
//   POWER HOUR │ 1:Timeline  2:Songs  3:Intros │ songs: 18/20 cached │ ⚠ yt-dlp
func renderHeader(m Model) string {
	var b strings.Builder

	b.WriteString(headerProject.Render("POWER HOUR"))
	b.WriteString(headerSep.Render(" │ "))

	// View tabs with number keys.
	for i, name := range m.viewNames {
		if i > 0 {
			b.WriteString("  ")
		}
		label := fmt.Sprintf("%d:%s", i+1, name)
		if i == m.activeView {
			b.WriteString(headerTabActive.Render(label))
		} else {
			b.WriteString(headerTab.Render(label))
		}
	}

	// Collection status counts.
	b.WriteString(headerSep.Render(" │ "))
	first := true
	for _, name := range m.collectionNames {
		s := m.summaries[name]
		if !first {
			b.WriteString("  ")
		}
		first = false

		cachedStr := countGreen.Render(fmt.Sprintf("%d", s.Cached))
		totalStr := fmt.Sprintf("%d", s.Total)
		b.WriteString(fmt.Sprintf("%s: %s/%s", name, cachedStr, totalStr))
	}

	// Tool health badge.
	if m.toolWarning != "" {
		b.WriteString(headerSep.Render(" │ "))
		b.WriteString(countYellow.Render("⚠ " + m.toolWarning))
	}

	return b.String()
}
