package dashboard

import "github.com/charmbracelet/lipgloss"

var (
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Faint(true)

	// Header bar styles.
	headerProject   = lipgloss.NewStyle().Bold(true)
	headerTabActive = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Underline(true)
	headerTab       = lipgloss.NewStyle().Faint(true)
	headerSep       = lipgloss.NewStyle().Faint(true)

	// Status count colors.
	countGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	countYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// Footer.
	footerStyle = lipgloss.NewStyle().Faint(true)

	// Timeline view.
	sectionLabel  = lipgloss.NewStyle().Faint(true)
	typeBadgeFile = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))  // purple
	typeBadgeColl = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	cursorStyle   = lipgloss.NewStyle().Bold(true)
	fadeDim       = lipgloss.NewStyle().Faint(true)

	// Cache dots.
	dotCached  = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("●")
	dotMissing = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("●")

	// Collection view.
	colHeader = lipgloss.NewStyle().Bold(true).Faint(true)

	// Inline confirm prompt (e.g. delete? [y/n]).
	confirmStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)

	// Inline edit styles.
	editStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Underline(true)
	editRowStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	matchStyle               = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	addSuggestionActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	addSuggestionOtherStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
