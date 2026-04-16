package dashboard

import (
	"fmt"
	"strings"
)

// toolsView shows tool status information as a full tab view.
type toolsView struct {
	tools     []ToolStatus
	termWidth int
}

func newToolsView(tools []ToolStatus) toolsView {
	return toolsView{tools: tools}
}

func (v toolsView) view() string {
	var b strings.Builder

	b.WriteString(sectionLabel.Render("TOOLS"))
	b.WriteByte('\n')
	b.WriteByte('\n')

	if len(v.tools) == 0 {
		b.WriteString(faint.Render("  No tool information available."))
		b.WriteByte('\n')
		return b.String()
	}

	for _, t := range v.tools {
		b.WriteString(bold.Render("  " + t.Name))
		b.WriteByte('\n')

		b.WriteString(fmt.Sprintf("    Version:  %s\n", nonEmptyOrDash(t.Version)))
		b.WriteString(fmt.Sprintf("    Path:     %s\n", faint.Render(nonEmptyOrDash(t.Path))))
		b.WriteString(fmt.Sprintf("    Install:  %s\n", faint.Render(nonEmptyOrDash(t.InstallMethod))))

		if t.Optional && !t.Available {
			b.WriteString(fmt.Sprintf("    Update:   %s\n", faint.Render("optional, not found")))
		} else if t.UpdateAvail != "" {
			b.WriteString(fmt.Sprintf("    Update:   %s\n", countYellow.Render(t.UpdateAvail)))
		} else {
			b.WriteString(fmt.Sprintf("    Update:   %s\n", countGreen.Render("up to date")))
		}
		b.WriteByte('\n')
	}

	return b.String()
}

func nonEmptyOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
