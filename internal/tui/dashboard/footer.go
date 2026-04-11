package dashboard

import "github.com/charmbracelet/lipgloss"

var vlcDisabled = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Faint(true)

// renderFooter returns the context-sensitive hotkey reference line.
func renderFooter(m Model) string {
	if m.job.active {
		return footerStyle.Render("Busy: " + busySpinner(m.tick) + " " + m.job.label + "  Esc quit")
	}

	vlc := footerStyle.Render("v VLC  V all")
	if !m.vlcFound {
		vlc = vlcDisabled.Render("v VLC  V all")
	}

	kind := m.viewKind(m.activeView)
	switch kind {
	case "timeline":
		return footerStyle.Render("←/→ views  ↑/↓ move  J/K reorder  a add  d del  ") + vlc + footerStyle.Render("  r render  c concat  ? help  Esc quit")
	case "collection":
		return footerStyle.Render("←/→ views  ↑/↓ move  J/K reorder  a add  d del  f/F fetch/all  ") + vlc + footerStyle.Render("  e/E edit/ext  r/R render/all  ? help  Esc quit")
	case "cache":
		return footerStyle.Render("←/→ views  ↑/↓ move  f filter  d doctor  D all  ") + vlc + footerStyle.Render("  ? help  Esc quit")
	case "tools":
		return footerStyle.Render("←/→ views  ? help  Esc quit")
	}
	return footerStyle.Render("←/→ views  ? help  Esc quit")
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
