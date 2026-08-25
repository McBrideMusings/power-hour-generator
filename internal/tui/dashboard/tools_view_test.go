package dashboard

import (
	"fmt"
	"strings"
	"testing"
)

func TestToolsViewTruncationNotice(t *testing.T) {
	tests := []struct {
		name       string
		numTools   int
		termHeight int
		wantNotice bool
		wantN      int    // only checked if wantNotice is true
		checkLine  string // partial string to check for in output
	}{
		{
			name:       "tall terminal, all tools fit, no notice",
			numTools:   3,
			termHeight: 100,
			wantNotice: false,
			checkLine:  "Tool 1",
		},
		{
			name:       "exactly two tools fit, one truncated",
			numTools:   3,
			termHeight: 22, // 2 header + 6*2 tools + 1 notice = 17 lines total, budget = 17, fits 2 tools + notice
			wantNotice: true,
			wantN:      1,
			checkLine:  "… and 1 more",
		},
		{
			name:       "first tool fits, second and third truncated",
			numTools:   3,
			termHeight: 17, // budget = 12, fits 1 tool (7 lines: header 2 + tool 6) + notice (1) = 8 within budget
			wantNotice: true,
			wantN:      2,
			checkLine:  "… and 2 more",
		},
		{
			name:       "no tools fit due to budget, no notice (edge case)",
			numTools:   3,
			termHeight: 6, // budget = 1 (too small to fit header + any tool + notice)
			wantNotice: false,
			checkLine:  "Tool 1", // will be truncated away but no partial/negative notice
		},
		{
			name:       "termHeight 0 means no truncation budget",
			numTools:   3,
			termHeight: 0,
			wantNotice: false,
			checkLine:  "Tool 1",
		},
		{
			name:       "empty tools list",
			numTools:   0,
			termHeight: 50,
			wantNotice: false,
			checkLine:  "No tool information available",
		},
		{
			name:       "single tool, tall terminal",
			numTools:   1,
			termHeight: 50,
			wantNotice: false,
			checkLine:  "Tool 1",
		},
		{
			name:       "single tool truncated",
			numTools:   1,
			termHeight: 8, // budget = 3 (header 2 + notice 1), tool doesn't fit, no notice
			wantNotice: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := makeTestTools(tt.numTools)
			v := toolsView{
				tools:      tools,
				termHeight: tt.termHeight,
			}

			output := v.view()

			// Check that output respects the line budget.
			maxLines := tt.termHeight - 5
			if maxLines > 0 {
				lineCount := strings.Count(output, "\n")
				if lineCount > maxLines {
					t.Errorf("output has %d newlines, exceeds budget of %d", lineCount, maxLines)
				}
			}

			// Check for notice presence.
			hasNotice := strings.Contains(output, "… and")
			if hasNotice != tt.wantNotice {
				t.Errorf("hasNotice = %v, want %v", hasNotice, tt.wantNotice)
			}

			// Check the notice's N value if present.
			if tt.wantNotice {
				wantStr := fmt.Sprintf("… and %d more", tt.wantN)
				if !strings.Contains(output, wantStr) {
					t.Errorf("output missing expected notice: %q", wantStr)
				}
			}

			// Check that checkLine appears or is absent based on truncation.
			hasCheckLine := strings.Contains(output, tt.checkLine)
			if !hasCheckLine && tt.checkLine != "" {
				// For very small budgets, the checkLine might be truncated away.
				// Only fail if we expected it and it was supposed to fit.
				if maxLines > 10 {
					t.Errorf("output missing expected content: %q", tt.checkLine)
				}
			}
		})
	}
}

func TestToolsViewNoticeLineCountIsExact(t *testing.T) {
	// This test verifies the off-by-one scenario:
	// When the notice's reserved line pushes out one more tool,
	// N must account for that reserved line and be exact.
	// With termHeight 21: budget = 16, header 2, tool 6 each.
	// Fits tool1(6) + tool2(6) + notice(1) = 15 within budget 16,
	// so tool3 is the one truncated. N = 1.

	numTools := 3
	termHeight := 21 // budget = 16: fits header(2) + tool(6) + tool(6) + notice(1) = 15 within 16

	tools := makeTestTools(numTools)
	v := toolsView{
		tools:      tools,
		termHeight: termHeight,
	}

	output := v.view()

	// The notice should report 1 tool not shown (tool 3).
	if !strings.Contains(output, "… and 1 more") {
		t.Errorf("expected notice '… and 1 more', output:\n%s", output)
	}

	// Verify tool1 is in the output.
	if !strings.Contains(output, "Tool 1") {
		t.Error("Tool 1 should be in output")
	}

	// Verify tool2 is in the output.
	if !strings.Contains(output, "Tool 2") {
		t.Error("Tool 2 should be in output")
	}

	// Verify tool3 is NOT in the output (truncated).
	if strings.Contains(output, "Tool 3") {
		t.Error("Tool 3 should not be in output (should be truncated)")
	}
}

func TestToolsViewNoticeIsLastLine(t *testing.T) {
	// The truncation notice closes the view. Hotkeys are not rendered here —
	// renderFooter owns the whole key reference.
	numTools := 3
	termHeight := 17 // Small budget to force truncation.

	tools := makeTestTools(numTools)
	v := toolsView{
		tools:      tools,
		termHeight: termHeight,
	}

	output := v.view()

	if !strings.Contains(output, "\u2026 and") {
		t.Fatalf("expected a truncation notice, output:\n%s", output)
	}

	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	last := stripANSI(lines[len(lines)-1])
	if !strings.Contains(last, "\u2026") {
		t.Errorf("notice should be the last line, got: %q", last)
	}
	if strings.Contains(output, "refresh") || strings.Contains(output, "update all") {
		t.Errorf("tools view must not render hotkeys; that is renderFooter's job:\n%s", output)
	}
}

func TestToolsViewCursorScrollsIntoWindow(t *testing.T) {
	// A cursor past the visible window scrolls the view down and swaps the
	// "… and N more" notice for an "↑ N more above" one.
	v := toolsView{
		tools:      makeTestTools(4),
		cursor:     3,
		termHeight: 17, // budget fits a single 6-line block
	}

	output := stripANSI(v.view())

	if !strings.Contains(output, "Tool 4") {
		t.Errorf("cursor tool should be visible, output:\n%s", output)
	}
	if strings.Contains(output, "Tool 1") {
		t.Errorf("Tool 1 should have scrolled out of view, output:\n%s", output)
	}
	if !strings.Contains(output, "more above") {
		t.Errorf("expected an above-notice, output:\n%s", output)
	}
}

func TestToolsViewNoteSitsUnderItsOwnTool(t *testing.T) {
	// The cursor is on Tool 1 but the note belongs to Tool 2: it must render
	// under Tool 2, not under the cursor.
	v := toolsView{
		tools:      makeTestTools(3),
		cursor:     0,
		rowStatus:  map[string]string{"Tool 2": "note:Updated."},
		termWidth:  120,
		termHeight: 100,
	}

	lines := strings.Split(stripANSI(v.view()), "\n")

	noteIdx, toolTwoIdx, toolThreeIdx := -1, -1, -1
	for i, line := range lines {
		switch {
		case strings.Contains(line, "Updated."):
			noteIdx = i
		case strings.Contains(line, "Tool 2"):
			toolTwoIdx = i
		case strings.Contains(line, "Tool 3"):
			toolThreeIdx = i
		}
	}

	if noteIdx < 0 {
		t.Fatalf("note missing from output:\n%s", strings.Join(lines, "\n"))
	}
	if noteIdx < toolTwoIdx || noteIdx > toolThreeIdx {
		t.Errorf("note at line %d should sit between Tool 2 (%d) and Tool 3 (%d)", noteIdx, toolTwoIdx, toolThreeIdx)
	}
}

// Helper to create test tools.
func makeTestTools(count int) []ToolStatus {
	tools := make([]ToolStatus, count)
	for i := 0; i < count; i++ {
		tools[i] = ToolStatus{
			Name:          fmt.Sprintf("Tool %d", i+1),
			Optional:      false,
			Available:     true,
			Version:       "1.0.0",
			Path:          "/usr/local/bin/tool",
			InstallMethod: "homebrew",
			UpdateAvail:   "",
		}
	}
	return tools
}

// stripANSI removes ANSI escape codes from a string for testing.
func stripANSI(s string) string {
	// Simple regex-free approach: look for the pattern ESC[...m and remove it.
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
		} else if inEscape && r == 'm' {
			inEscape = false
		} else if !inEscape {
			result.WriteRune(r)
		}
	}
	return result.String()
}
