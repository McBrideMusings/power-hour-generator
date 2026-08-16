package dashboard

import (
	"fmt"
	"strings"
)

// toolsView shows tool status information as a full tab view.
type toolsView struct {
	tools      []ToolStatus
	termWidth  int
	termHeight int
}

func newToolsView(tools []ToolStatus) toolsView {
	return toolsView{tools: tools}
}

func (v toolsView) view() string {
	var b strings.Builder

	// Render header (2 lines).
	b.WriteString(sectionLabel.Render("TOOLS"))
	b.WriteByte('\n')
	b.WriteByte('\n')
	headerLines := 2

	// Handle empty tools list.
	if len(v.tools) == 0 {
		b.WriteString(faint.Render("  No tool information available."))
		b.WriteByte('\n')
		return b.String()
	}

	// Render each tool into a block with its line count.
	type toolBlock struct {
		content string
		lines   int
	}
	var blocks []toolBlock

	for _, t := range v.tools {
		var toolB strings.Builder

		toolB.WriteString(bold.Render("  " + t.Name))
		toolB.WriteByte('\n')

		toolB.WriteString(fmt.Sprintf("    Version:  %s\n", nonEmptyOrDash(t.Version)))
		toolB.WriteString(fmt.Sprintf("    Path:     %s\n", faint.Render(nonEmptyOrDash(t.Path))))
		toolB.WriteString(fmt.Sprintf("    Install:  %s\n", faint.Render(nonEmptyOrDash(t.InstallMethod))))

		if t.Optional && !t.Available {
			toolB.WriteString(fmt.Sprintf("    Update:   %s\n", faint.Render("optional, not found")))
		} else if t.UpdateAvail != "" {
			toolB.WriteString(fmt.Sprintf("    Update:   %s\n", countYellow.Render(t.UpdateAvail)))
		} else {
			toolB.WriteString(fmt.Sprintf("    Update:   %s\n", countGreen.Render("up to date")))
		}
		toolB.WriteByte('\n')

		blockContent := toolB.String()
		blockLines := strings.Count(blockContent, "\n")
		blocks = append(blocks, toolBlock{content: blockContent, lines: blockLines})
	}

	// Determine if we need to truncate based on termHeight budget.
	maxLines := 0
	if v.termHeight > 0 {
		maxLines = v.termHeight - 5
	}

	// If no budget or budget is zero/negative, emit all blocks (no truncation).
	if maxLines <= 0 {
		for _, block := range blocks {
			b.WriteString(block.content)
		}
		return b.String()
	}

	// Check if all blocks fit in the budget.
	totalLines := headerLines
	for _, block := range blocks {
		totalLines += block.lines
	}

	if totalLines <= maxLines {
		// All blocks fit; emit them all without notice.
		for _, block := range blocks {
			b.WriteString(block.content)
		}
		return b.String()
	}

	// Not all blocks fit; reserve one line for the notice.
	budget := maxLines - 1

	// If budget is too small to fit the notice line alongside the header,
	// fall back to blunt truncation (no notice).
	if budget <= headerLines {
		for _, block := range blocks {
			b.WriteString(block.content)
		}
		output := b.String()
		parts := strings.SplitN(output, "\n", maxLines+1)
		return strings.Join(parts[:maxLines], "\n") + "\n"
	}

	// Greedily accept blocks while fitting in the budget.
	accumulatedLines := headerLines
	shown := 0
	for i, block := range blocks {
		if accumulatedLines+block.lines <= budget {
			b.WriteString(block.content)
			accumulatedLines += block.lines
			shown = i + 1
		} else {
			break
		}
	}

	// Append the notice with the correct count.
	notShown := len(v.tools) - shown
	b.WriteString(faint.Render(fmt.Sprintf("  … and %d more", notShown)))
	b.WriteByte('\n')

	output := b.String()

	// Defensive clamp to ensure output never exceeds maxLines newlines.
	if strings.Count(output, "\n") > maxLines {
		parts := strings.SplitN(output, "\n", maxLines+1)
		output = strings.Join(parts[:maxLines], "\n") + "\n"
	}

	return output
}

func nonEmptyOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
