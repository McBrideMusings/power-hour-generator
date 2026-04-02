package dashboard

import "github.com/charmbracelet/lipgloss"

var vlcDisabled = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Faint(true)

// renderFooter returns the context-sensitive hotkey reference line.
func renderFooter(m Model) string {
	vlc := footerStyle.Render("v VLC  Shift+v VLC all")
	if !m.vlcFound {
		vlc = vlcDisabled.Render("v VLC  Shift+v VLC all")
	}

	kind := m.viewKind(m.activeView)
	switch kind {
	case "timeline":
		return footerStyle.Render("←/→ views  ↑/↓ move  Shift+↑/↓ reorder  a add  d del  ") + vlc + footerStyle.Render("  r render  c concat  ? help  q quit")
	case "collection":
		return footerStyle.Render("←/→ views  ↑/↓ move  Shift+↑/↓ reorder  a add  d del  ") + vlc + footerStyle.Render("  e edit  Shift+e external  r render  ? help  q quit")
	case "cache":
		return footerStyle.Render("←/→ views  ↑/↓ move  f filter  ") + vlc + footerStyle.Render("  ? help  q quit")
	case "tools":
		return footerStyle.Render("←/→ views  ? help  q quit")
	}
	return footerStyle.Render("←/→ views  ? help  q quit")
}
