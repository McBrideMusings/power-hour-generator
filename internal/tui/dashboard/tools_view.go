package dashboard

import (
	"fmt"
	"strings"

	"powerhour/internal/tools"
)

// toolsHelpLines is the single inline help row every non-empty tools view
// reserves at the bottom, matching the collection/cache/timeline views.
const toolsHelpLines = 1

// toolsBlockLines is the number of lines one tool's block occupies: name,
// version, path, install method, update state, and a trailing blank.
const toolsBlockLines = 6

// toolsView shows tool status information as a full tab view. The cursor
// selects the tool that `u` acts on; the view scrolls to keep it visible.
type toolsView struct {
	tools      []ToolStatus
	cursor     int
	note       string
	noteIsErr  bool
	termWidth  int
	termHeight int
}

func newToolsView(tools []ToolStatus) toolsView {
	return toolsView{tools: tools}
}

// selected returns the tool under the cursor.
func (v toolsView) selected() (ToolStatus, bool) {
	if v.cursor < 0 || v.cursor >= len(v.tools) {
		return ToolStatus{}, false
	}
	return v.tools[v.cursor], true
}

// outdated returns every tool with a pending update that powerhour knows how
// to apply. `U` acts on exactly this list.
func (v toolsView) outdated() []ToolStatus {
	var out []ToolStatus
	for _, t := range v.tools {
		if t.UpdateAvail == "" {
			continue
		}
		if !tools.UpdateSupported(t.Name, t.InstallMethod) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// renderBlock renders one tool's detail block, marking it when it is under
// the cursor.
func (v toolsView) renderBlock(t ToolStatus, selected bool) string {
	var b strings.Builder

	marker := "  "
	if selected {
		marker = "> "
	}
	name := t.Name
	if selected {
		name = cursorStyle.Render(name)
	} else {
		name = bold.Render(name)
	}
	b.WriteString(marker + name)
	b.WriteByte('\n')

	b.WriteString(fmt.Sprintf("    Version:  %s\n", nonEmptyOrDash(t.Version)))
	b.WriteString(fmt.Sprintf("    Path:     %s\n", faint.Render(nonEmptyOrDash(t.Path))))
	b.WriteString(fmt.Sprintf("    Install:  %s\n", faint.Render(nonEmptyOrDash(t.InstallMethod))))

	switch {
	case t.Optional && !t.Available:
		b.WriteString(fmt.Sprintf("    Update:   %s\n", faint.Render("optional, not found")))
	case t.UpdateAvail != "":
		b.WriteString(fmt.Sprintf("    Update:   %s\n", countYellow.Render(t.UpdateAvail)))
	default:
		b.WriteString(fmt.Sprintf("    Update:   %s\n", countGreen.Render("up to date")))
	}
	b.WriteByte('\n')

	return b.String()
}

// renderHelpRow follows the shared priority ladder: a transient note from the
// last action wins, otherwise the default action hint for the cursor row.
func (v toolsView) renderHelpRow() string {
	noteStyle := editStyle
	if v.noteIsErr {
		noteStyle = errorNoteStyle
	}
	return resolveHelpRow(v.termWidth, nil,
		helpRowSource{text: v.note, style: noteStyle},
		helpRowSource{text: v.defaultHint(), style: footerStyle},
	)
}

func (v toolsView) defaultHint() string {
	parts := []string{"r refresh"}

	if t, ok := v.selected(); ok {
		if tools.UpdateSupported(t.Name, t.InstallMethod) {
			parts = append(parts, "u update "+t.Name)
		} else {
			parts = append(parts, "no update path for "+t.Name)
		}
	}

	if n := len(v.outdated()); n > 0 {
		parts = append(parts, fmt.Sprintf("U update all (%d outdated)", n))
	}

	return strings.Join(parts, " · ")
}

func (v toolsView) view() string {
	var b strings.Builder

	b.WriteString(sectionLabel.Render("TOOLS"))
	b.WriteByte('\n')
	b.WriteByte('\n')
	headerLines := 2

	if len(v.tools) == 0 {
		b.WriteString(faint.Render("  No tool information available."))
		b.WriteByte('\n')
		return b.String()
	}

	blocks := make([]string, len(v.tools))
	for i, t := range v.tools {
		blocks[i] = v.renderBlock(t, i == v.cursor)
	}

	maxLines := 0
	if v.termHeight > 0 {
		maxLines = v.termHeight - dashboardChromeLines
	}

	// No budget: emit everything.
	if maxLines <= 0 {
		for _, block := range blocks {
			b.WriteString(block)
		}
		b.WriteString(v.renderHelpRow())
		b.WriteByte('\n')
		return b.String()
	}

	budget := maxLines - headerLines - toolsHelpLines

	// Budget too small for even the chrome: fall back to blunt truncation so
	// the view never overflows its allotment.
	if budget < toolsBlockLines {
		for _, block := range blocks {
			b.WriteString(block)
		}
		return clampLines(b.String(), maxLines)
	}

	start, end := v.window(len(blocks), budget)

	if start > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteByte('\n')
	}
	for _, block := range blocks[start:end] {
		b.WriteString(block)
	}
	if notShown := len(blocks) - end; notShown > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  … and %d more", notShown)))
		b.WriteByte('\n')
	}

	b.WriteString(v.renderHelpRow())
	b.WriteByte('\n')

	return clampLines(b.String(), maxLines)
}

// window picks the slice of blocks to display: the largest run that fits the
// budget while still containing the cursor. Reserved lines for the
// above/below truncation notices are charged against the same budget.
func (v toolsView) window(count, budget int) (int, int) {
	for start := 0; start < count; start++ {
		avail := budget
		if start > 0 {
			avail-- // "↑ N more above"
		}

		fit := avail / toolsBlockLines
		if start+fit < count {
			// A "… and N more" line is needed; charge it and refit.
			fit = (avail - 1) / toolsBlockLines
		}
		if fit < 1 {
			fit = 1
		}

		end := min(start+fit, count)
		if v.cursor < end {
			return start, end
		}
	}
	return count - 1, count
}

// clampLines trims text so it never carries more than max newlines.
func clampLines(text string, max int) string {
	if max <= 0 || strings.Count(text, "\n") <= max {
		return text
	}
	parts := strings.SplitN(text, "\n", max+1)
	return strings.Join(parts[:max], "\n") + "\n"
}

func nonEmptyOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
