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
	countGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // cached count (green)
	countYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // tool warnings (yellow)
	countCyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // rendered count (cyan)
	countRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // incomplete rendered count (red)

	// Footer.
	footerStyle = lipgloss.NewStyle().Faint(true)

	// Timeline view.
	sectionLabel  = lipgloss.NewStyle().Faint(true)
	typeBadgeFile = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))  // purple
	typeBadgeColl = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	cursorStyle   = lipgloss.NewStyle().Bold(true)
	fadeDim       = lipgloss.NewStyle().Faint(true)

	// Playback-readiness dots (timeline PLAYBACK ORDER panel): green = rendered
	// segment ready at this position, yellow half-dot = rendered at an older
	// position so only the burned-in number is stale, amber = falls back to
	// the raw/uncut source, red = nothing playable exists yet. Amber matches
	// rowNotRendered's color (214) below — same meaning, same hue — rather
	// than ANSI 3, which reads too close to red in many terminal themes.
	dotCached = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("●")
	// Rendered, but while the row sat at a different playback position — the
	// clip is right, the burned-in number is stale.
	dotMisnumbered = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("◐")
	dotFallback    = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("●")
	dotUnavailable = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("●")

	// Playback order panel: lock glyph, a locked slot's label, and the slot
	// currently marked for a swap.
	lockGlyphStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	lockedRowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cycleSlotStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	cycleArrowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	// The far side of a pending swap: italic, because the change is shown but
	// not yet saved.
	cyclePendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Italic(true)

	// Collection view.
	colHeader = lipgloss.NewStyle().Bold(true).Faint(true)

	// Inline confirm prompt (e.g. delete? [y/n]).
	confirmStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)

	// Inline error note (e.g. a failed fetch).
	errorNoteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)

	// Inline edit styles.
	editStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Underline(true).Background(lipgloss.AdaptiveColor{Light: "#e4e4e4", Dark: "#262626"})
	editRowStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Background(lipgloss.AdaptiveColor{Light: "#e4e4e4", Dark: "#262626"})
	editRowBgOnly            = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#e4e4e4", Dark: "#262626"})
	matchStyle               = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	addSuggestionActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	addSuggestionOtherStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
