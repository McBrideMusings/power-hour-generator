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
	Optional      bool
	Available     bool
	Version       string
	Path          string
	InstallMethod string
	UpdateAvail   string // empty if up to date
}

func renderHelpOverlay(viewKind string, width, height int) string {
	var b strings.Builder

	section := func(title string, lines ...string) {
		b.WriteString(bold.Render(title))
		b.WriteByte('\n')
		for _, line := range lines {
			b.WriteString("  " + line + "\n")
		}
		b.WriteByte('\n')
	}

	b.WriteString(bold.Render("Keyboard Shortcuts"))
	b.WriteByte('\n')
	b.WriteByte('\n')

	section("Global (any view)",
		"←/→, h/l, or 1-9    Switch views",
		"↑/↓ or j/k          Move cursor",
		"?                   Toggle this help",
		"u or Ctrl+R         Refresh from disk",
		"c                   Concatenate the full timeline into the final video",
		"o                   Open the project folder in the file manager",
		"q, Esc, or Ctrl+C   Quit",
	)

	switch viewKind {
	case "timeline":
		section("Timeline View",
			"r            Render all segments (runs powerhour render)",
			"a            Focus the Add slot (type a collection name or file path)",
			"J/K          Reorder the selected entry",
			"x            Delete the selected entry, or the output row",
			"e            On an entry row: open powerhour.yaml (press u after editing)",
			"E            On the output row: open the rendered concat file",
			"v            Play the selected entry (or output) in VLC",
			"V            Play the whole timeline as a VLC playlist",
		)
	case "cache":
		section("Cache View",
			"a            Focus the Add slot (paste a URL, YouTube ID, or local file path)",
			"e            Inline-edit the selected entry (Tab saves + next field, Enter saves + exits)",
			"f            Toggle filtered / all entries",
			"D            Review entries needing attention (cache doctor)",
			"x            Remove the selected cache entry",
			"v            Play the selected entry in VLC",
			"V            Play all visible entries as a VLC playlist",
		)
	case "tools":
		section("Tools View",
			"(browse only — no actions yet; see issue #60 to add update actions)",
		)
	default:
		section("Collection View",
			"a            Focus the Add Clip slot (paste a link/path, or CSV/TSV/YAML)",
			"Tab/Enter    Add slot: accept the highlighted cache match (Enter falls back to raw URL/batch)",
			"d            Duplicate the selected row",
			"J/K          Reorder the selected row",
			"x            Delete the selected row",
			"e            Inline-edit the selected row (Tab saves + next field, Enter saves + exits)",
			"Ctrl+R       While inline-editing a link: probe metadata for the current URL",
			"E            Open the collection's plan file in the OS default app",
			"f/F          Fetch the selected row / all rows",
			"r/R          Render the selected row / all rows",
			"v            Play the selected row in VLC",
			"Alt+V        Play the full untrimmed source (for finding a clip's start time)",
			"V            Play all rows as a VLC playlist",
		)
	}

	section("While editing (input, inline-edit, add-clip, confirm-delete)",
		"Tab          Save and move to the next field (or accept a suggestion)",
		"Enter        Save and exit the field",
		"Esc          Cancel",
		"y / Enter    Confirm a pending delete",
	)

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
