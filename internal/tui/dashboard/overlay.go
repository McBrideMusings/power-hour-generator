package dashboard

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHelp
	overlayDoctor
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
	b.WriteString("  o            Open project in file manager\n")
	b.WriteString("  u / Ctrl+R   Refresh from disk\n")
	b.WriteString("  ?            This help\n")
	b.WriteString("  q / Esc / Ctrl+C Quit\n")
	b.WriteByte('\n')

	b.WriteString(bold.Render("Navigation"))
	b.WriteByte('\n')
	b.WriteString("  ↑/↓ or j/k       Move cursor\n")
	b.WriteString("  J/K              Reorder item\n")
	b.WriteString("  a                Focus Add Clip slot (paste link/path/CSV)\n")
	if activeView == 0 {
		b.WriteString("  x                Delete selected timeline entry\n")
	}
	b.WriteString("  v                Play in VLC\n")
	b.WriteString("  V                Play all in VLC\n")

	if activeView != 0 && activeView <= 10 { // collection views
		b.WriteByte('\n')
		b.WriteString(bold.Render("Collection"))
		b.WriteByte('\n')
		b.WriteString("  d            Duplicate row to bottom\n")
		b.WriteString("  x            Delete selected row\n")
		b.WriteString("  e/E          Edit/ext\n")
		b.WriteString("  f/F          Fetch selected/all\n")
		b.WriteString("  r/R          Render selected/all\n")
	}

	b.WriteByte('\n')
	b.WriteString(bold.Render("Cache View"))
	b.WriteByte('\n')
	b.WriteString("  x            Remove selected cache entry\n")
	b.WriteString("  d            Doctor selected entry (interactive)\n")
	b.WriteString("  D            Doctor all visible entries (interactive)\n")

	b.WriteByte('\n')
	b.WriteString(faint.Render("[?] Close  [q/Esc] Quit"))

	content := overlayBorder.Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func nonEmpty(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}
