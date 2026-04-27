package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	tickInterval = 150 * time.Millisecond
	marqueeGap   = "   "
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// tickMsg drives animation (spinner, marquee).
type tickMsg time.Time

// Column defines a single column in the progress table.
type Column struct {
	Header string
	Width  int
	Flex   bool // if true, expands to fill remaining terminal width
}

// Row holds the field values for a single table row.
type Row struct {
	Key    string
	Fields []string
}

// ProgressModel is a bubbletea model that renders a tabular progress display.
// It is parameterized by column definitions so the same model handles all
// command variants (fetch, render, collection variants).
type ProgressModel struct {
	columns  []Column
	rows     []Row
	rowIndex map[string]int
	title    string
	done     bool
	err      error

	// statusCol caches the index of the STATUS column (-1 if absent).
	statusCol int

	// Animation state.
	tick int

	// Viewport state for scrolling when rows exceed terminal height.
	termHeight int
	termWidth  int
	scrollTop  int
}

// NewProgressModel creates a progress model with the given title and columns.
func NewProgressModel(title string, columns []Column) ProgressModel {
	statusCol := -1
	for i, c := range columns {
		if strings.EqualFold(c.Header, "STATUS") {
			statusCol = i
			break
		}
	}
	return ProgressModel{
		columns:   columns,
		rows:      nil,
		rowIndex:  make(map[string]int),
		title:     title,
		statusCol: statusCol,
	}
}

// AddRow pre-populates a row. Call this before the program starts.
func (m *ProgressModel) AddRow(key string, fields []string) {
	padded := make([]string, len(m.columns))
	copy(padded, fields)
	m.rowIndex[key] = len(m.rows)
	m.rows = append(m.rows, Row{Key: key, Fields: padded})
}

func scheduleTick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Init satisfies the tea.Model interface.
func (m ProgressModel) Init() tea.Cmd {
	return scheduleTick()
}

// Update satisfies the tea.Model interface.
func (m ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termHeight = msg.Height
		m.termWidth = msg.Width
		return m, nil

	case tickMsg:
		m.tick++
		if m.done {
			return m, nil
		}
		return m, scheduleTick()

	case RowUpdateMsg:
		m.applyRowUpdate(msg)
		if idx, ok := m.rowIndex[msg.Key]; ok {
			m.autoScroll(idx)
		}
		return m, nil

	case WorkDoneMsg:
		m.done = true
		return m, tea.Quit

	case ErrorMsg:
		m.err = msg.Err
		m.done = true
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// applyRowUpdate updates a row's fields from a RowUpdateMsg.
func (m *ProgressModel) applyRowUpdate(msg RowUpdateMsg) {
	idx, ok := m.rowIndex[msg.Key]
	if !ok {
		return
	}
	row := &m.rows[idx]
	for j, col := range m.columns {
		if val, exists := msg.Fields[col.Header]; exists {
			row.Fields[j] = val
		}
	}
}

// View satisfies the tea.Model interface.
func (m ProgressModel) View() string {
	if m.done && m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	// Calculate column widths. Non-flex columns use their specified width (at
	// least as wide as the header). Flex columns split any remaining terminal
	// width equally after accounting for fixed columns and separators.
	widths := make([]int, len(m.columns))
	flexCount := 0
	fixedTotal := 0
	for i, col := range m.columns {
		w := len(col.Header)
		if col.Width > w {
			w = col.Width
		}
		widths[i] = w
		if col.Flex {
			flexCount++
		} else {
			fixedTotal += w
		}
	}
	if flexCount > 0 && m.termWidth > 0 {
		separators := (len(m.columns) - 1) * 2
		remaining := m.termWidth - fixedTotal - separators
		if remaining > flexCount {
			perFlex := remaining / flexCount
			for i, col := range m.columns {
				if col.Flex && perFlex > widths[i] {
					widths[i] = perFlex
				}
			}
		}
	}

	var b strings.Builder

	// Header
	headerParts := make([]string, len(m.columns))
	for i, col := range m.columns {
		headerParts[i] = HeaderStyle.Render(pad(col.Header, widths[i]))
	}
	b.WriteString(strings.Join(headerParts, "  "))
	b.WriteByte('\n')

	// Determine visible row range (viewport).
	visibleRows := m.visibleRowCount()
	startRow := 0
	endRow := len(m.rows)
	if visibleRows > 0 && len(m.rows) > visibleRows {
		startRow = m.scrollTop
		endRow = startRow + visibleRows
		if endRow > len(m.rows) {
			endRow = len(m.rows)
		}
	}

	// Scroll-up indicator.
	if startRow > 0 {
		fmt.Fprintf(&b, "  ↑ %d more above\n", startRow)
	}

	// Rows
	for _, row := range m.rows[startRow:endRow] {
		parts := make([]string, len(m.columns))
		for i := range m.columns {
			val := ""
			if i < len(row.Fields) {
				val = row.Fields[i]
			}
			if !m.done && len(strings.TrimSpace(val)) > widths[i] {
				val = marqueeText(val, widths[i], m.tick)
			} else {
				val = TruncateWithEllipsis(val, widths[i])
			}
			if i == m.statusCol {
				parts[i] = StatusStyle(val).Render(pad(val, widths[i]))
			} else {
				parts[i] = pad(val, widths[i])
			}
		}
		b.WriteString(strings.Join(parts, "  "))
		b.WriteByte('\n')
	}

	// Scroll-down indicator.
	if endRow < len(m.rows) {
		fmt.Fprintf(&b, "  ↓ %d more below\n", len(m.rows)-endRow)
	}

	// Footer: spinner + progress counter while work is in progress.
	if !m.done {
		processed, total := m.progressCounts()
		spinner := spinnerFrames[m.tick%len(spinnerFrames)]
		fmt.Fprintf(&b, "\n%s Processing %d/%d...\n", spinner, processed, total)
	}

	return b.String()
}

// progressCounts returns (processed, total) based on how many rows have left "pending".
func (m ProgressModel) progressCounts() (int, int) {
	total := len(m.rows)
	processed := 0
	if m.statusCol < 0 {
		return 0, total
	}
	for _, row := range m.rows {
		if m.statusCol < len(row.Fields) {
			status := strings.TrimSpace(row.Fields[m.statusCol])
			if status != "" && status != "pending" {
				processed++
			}
		}
	}
	return processed, total
}

// Done returns whether the model has finished (work done or error).
func (m ProgressModel) Done() bool {
	return m.done
}

// Err returns any fatal error that occurred.
func (m ProgressModel) Err() error {
	return m.err
}

// visibleRowCount returns how many data rows fit in the terminal, accounting
// for the header line, footer (spinner + blank line), and possible scroll
// indicators. Returns 0 if the terminal height is unknown or all rows fit.
func (m ProgressModel) visibleRowCount() int {
	if m.termHeight <= 0 {
		return 0
	}
	// Reserve: 1 header + 2 footer (blank + spinner) + 2 scroll indicators.
	available := m.termHeight - 5
	if available <= 0 {
		available = 1
	}
	if len(m.rows) <= available {
		return 0 // all rows fit, no viewport needed
	}
	return available
}

// autoScroll adjusts scrollTop so that the given row index is visible.
func (m *ProgressModel) autoScroll(idx int) {
	visible := m.visibleRowCount()
	if visible <= 0 {
		return
	}
	if idx < m.scrollTop {
		m.scrollTop = idx
	} else if idx >= m.scrollTop+visible {
		m.scrollTop = idx - visible + 1
	}
}

// RenderFinalTable returns a plain-text rendering of the complete table
// (header + all rows) with status coloring. No spinner, no scroll indicators,
// no marquee — suitable for printing after bubbletea exits so the full table
// appears in terminal scrollback.
func (m ProgressModel) RenderFinalTable() string {
	widths := make([]int, len(m.columns))
	for i, col := range m.columns {
		widths[i] = len(col.Header)
		if col.Width > widths[i] {
			widths[i] = col.Width
		}
	}

	var b strings.Builder

	// Header
	headerParts := make([]string, len(m.columns))
	for i, col := range m.columns {
		headerParts[i] = HeaderStyle.Render(pad(col.Header, widths[i]))
	}
	b.WriteString(strings.Join(headerParts, "  "))
	b.WriteByte('\n')

	// All rows
	for _, row := range m.rows {
		parts := make([]string, len(m.columns))
		for i := range m.columns {
			val := ""
			if i < len(row.Fields) {
				val = row.Fields[i]
			}
			val = TruncateWithEllipsis(val, widths[i])
			if i == m.statusCol {
				parts[i] = StatusStyle(val).Render(pad(val, widths[i]))
			} else {
				parts[i] = pad(val, widths[i])
			}
		}
		b.WriteString(strings.Join(parts, "  "))
		b.WriteByte('\n')
	}

	return b.String()
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// marqueeText renders a scrolling window over text that exceeds the given width.
// The text slides left on each tick, with a gap between cycles.
func marqueeText(text string, width, tick int) string {
	text = strings.TrimSpace(text)
	if width <= 0 {
		return ""
	}
	if len(text) <= width {
		return text
	}
	cycle := text + marqueeGap
	cycleLen := len(cycle)
	offset := tick % cycleLen
	var result strings.Builder
	result.Grow(width)
	for i := 0; i < width; i++ {
		result.WriteByte(cycle[(offset+i)%cycleLen])
	}
	return result.String()
}

// NonEmptyOrDash returns "-" for empty/whitespace strings.
func NonEmptyOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

// TruncateWithEllipsis truncates a string and adds "..." if it exceeds max runes.
func TruncateWithEllipsis(value string, max int) string {
	if max <= 0 {
		return ""
	}
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
