package dashboard

import "github.com/charmbracelet/lipgloss"

var vlcDisabled = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Faint(true)

// renderFooter returns the context-sensitive hotkey reference line.
func renderFooter(m Model) string {
	if m.job.active {
		return footerStyle.Render("Busy: " + busySpinner(m.tick) + " " + m.job.label + "  q/Esc quit")
	}

	vlc := footerStyle.Render("v VLC  V all")
	if !m.vlcFound {
		vlc = vlcDisabled.Render("v VLC  V all")
	}

	kind := m.viewKind(m.activeView)
	switch kind {
	case "timeline":
		return footerStyle.Render("←/→ views  ↑/↓ move  J/K reorder  a add  x del  u refresh  ") + vlc + footerStyle.Render("  r render  c concat  ? help  q/Esc quit")
	case "collection":
		return footerStyle.Render("←/→ views  ↑/↓ move  J/K reorder  a add  d dup  x del  u refresh  f/F fetch/all  ") + vlc + footerStyle.Render("  e/E edit/ext  r/R render/all  ? help  q/Esc quit")
	case "cache":
		return footerStyle.Render("←/→ views  ↑/↓ move  f filter  x del  d doctor  D all  u refresh  ") + vlc + footerStyle.Render("  ? help  q/Esc quit")
	case "tools":
		return footerStyle.Render("←/→ views  u refresh  ? help  q/Esc quit")
	}
	return footerStyle.Render("←/→ views  u refresh  ? help  q/Esc quit")
}

func busySpinner(tick int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if len(frames) == 0 {
		return ""
	}
	if tick < 0 {
		tick = 0
	}
	return frames[tick%len(frames)]
}
