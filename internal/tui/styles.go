package tui

import "github.com/charmbracelet/lipgloss"

var (
	// HeaderStyle styles the column header row.
	HeaderStyle = lipgloss.NewStyle().Bold(true)

	statusStyles = map[string]lipgloss.Style{
		// Terminal states
		"downloaded": lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"copied":     lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"matched":    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"cached":     lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"rendered":   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"complete":   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),

		// Active states
		"resolving":   lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		"downloading": lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		"matching":    lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		"copying":     lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		"rendering":   lipgloss.NewStyle().Foreground(lipgloss.Color("4")),

		// Skipped / warning
		"skipped": lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"missing": lipgloss.NewStyle().Foreground(lipgloss.Color("3")),

		// Error
		"error": lipgloss.NewStyle().Foreground(lipgloss.Color("1")),

		// Pending
		"pending": lipgloss.NewStyle().Faint(true),
	}
)

// StatusStyle returns the lipgloss style for the given status string.
func StatusStyle(status string) lipgloss.Style {
	if s, ok := statusStyles[status]; ok {
		return s
	}
	return lipgloss.NewStyle()
}
