package dashboard

import (
	"fmt"
	"strings"

	"powerhour/internal/tools"
)

// toolsBlockLines is the number of lines one tool's block occupies: name,
// version, path, install method, update state, and a trailing blank.
const toolsBlockLines = 6

// toolsView shows tool status information as a full tab view. The cursor
// selects the tool that `u` acts on; the view scrolls to keep it visible.
type toolsView struct {
	tools  []ToolStatus
	cursor int

	// rowStatus holds transient per-tool notes keyed by tool name, expired by
	// rowStatusUntil the same way every other view's row notes are. Keying by
	// name rather than cursor position is what keeps "Updated yt-dlp." under
	// yt-dlp when a re-detect reorders the list or moves the cursor.
	rowStatus      map[string]string
	rowStatusUntil map[string]int

	tick       int
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

// renderNote renders one tool's transient note as a row directly beneath that
// tool. Hotkeys are not rendered here — every view's key reference lives in
// the one footer built by renderFooter.
func (v toolsView) renderNote(tool string) string {
	raw := v.rowStatus[tool]
	note := inlineRowNote(raw, v.tick)
	if note == "" {
		return ""
	}
	return helpRowText(note, noteStyleFor(raw), v.termWidth)
}

// noteLines is the vertical budget the notes claim across the whole list.
func (v toolsView) noteLines() int {
	n := 0
	for _, t := range v.tools {
		if inlineRowNote(v.rowStatus[t.Name], v.tick) != "" {
			n++
		}
	}
	return n
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

	// writeBlocks emits blocks[from:to], slipping each tool's transient note
	// in directly beneath the tool it reports on.
	writeBlocks := func(from, to int) {
		for i := from; i < to; i++ {
			b.WriteString(blocks[i])
			if note := v.renderNote(v.tools[i].Name); note != "" {
				b.WriteString(note)
				b.WriteByte('\n')
			}
		}
	}

	// No budget: emit everything.
	if maxLines <= 0 {
		writeBlocks(0, len(blocks))
		return b.String()
	}

	budget := maxLines - headerLines - v.noteLines()

	// Budget too small for even the chrome: fall back to blunt truncation so
	// the view never overflows its allotment.
	if budget < toolsBlockLines {
		writeBlocks(0, len(blocks))
		return clampLines(b.String(), maxLines)
	}

	start, end := v.window(len(blocks), budget)

	if start > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteByte('\n')
	}
	writeBlocks(start, end)
	if notShown := len(blocks) - end; notShown > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  … and %d more", notShown)))
		b.WriteByte('\n')
	}

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
