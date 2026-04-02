package dashboard

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHelp
)

var (
	overlayBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(1, 2)
)

// ToolStatus holds info about an external tool for the overlay.
type ToolStatus struct {
	Name          string
	Version       string
	Path          string
	InstallMethod string
	UpdateAvail   string // empty if up to date
}

func renderHelpOverlay(activeView int, width, height int) string {
	var b strings.Builder

	b.WriteString(bold.Render("Keyboard Shortcuts"))
	b.WriteByte('\n')
	b.WriteByte('\n')

	b.WriteString(bold.Render("Global"))
	b.WriteByte('\n')
	b.WriteString("  ←/→ or h/l   Switch views\n")
	b.WriteString("  1-9           Jump to view\n")
	b.WriteString("  r            Render all segments\n")
	b.WriteString("  c            Concatenate final video\n")
	b.WriteString("  ?            This help\n")
	b.WriteString("  q / Ctrl+C   Quit\n")
	b.WriteByte('\n')

	b.WriteString(bold.Render("Navigation"))
	b.WriteByte('\n')
	b.WriteString("  ↑/↓ or j/k       Move cursor\n")
	b.WriteString("  Shift+↑/↓ or J/K Reorder item\n")
	b.WriteString("  a                Add entry/row\n")
	b.WriteString("  d                Delete entry/row\n")
	b.WriteString("  v                Play in VLC\n")
	b.WriteString("  V (Shift+v)      Play all in VLC\n")

	if activeView != 0 && activeView <= 10 { // collection views
		b.WriteByte('\n')
		b.WriteString(bold.Render("Collection"))
		b.WriteByte('\n')
		b.WriteString("  e            Inline edit row\n")
		b.WriteString("  Shift+e      Open in external editor\n")
	}

	b.WriteByte('\n')
	b.WriteString(faint.Render("[Esc] Close"))

	content := overlayBorder.Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func nonEmpty(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}
