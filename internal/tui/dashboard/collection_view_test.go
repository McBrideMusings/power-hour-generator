package dashboard

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"powerhour/internal/tui"
)

func TestGutterStatusPaddingWithVisualWidth(t *testing.T) {
	tests := []struct {
		name          string
		status        string
		statusWidth   int
		expectedWidth int
	}{
		{"ASCII status", "cached", 5, 5},
		{"ASCII status short", "---", 5, 5},
		{"ASCII status render", "render", 5, 5},
		{"emoji status", "🎵 ok", 5, 5},
		{"wide char status", "日本語", 5, 5},
		{"mixed emoji and text", "✓ done", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the padding logic from the fix
			truncatedStatus := tui.TruncateWithEllipsis(tt.status, tt.statusWidth)
			paddedStatus := lipgloss.NewStyle().Width(tt.statusWidth).Render(truncatedStatus)

			// Verify the padded status has the correct visual width
			visualWidth := lipgloss.Width(paddedStatus)
			if visualWidth != tt.expectedWidth {
				t.Errorf("paddedStatus visual width = %d, want %d (status=%q, truncated=%q, rendered=%q)",
					visualWidth, tt.expectedWidth, tt.status, truncatedStatus, paddedStatus)
			}
		})
	}
}

func TestGutterGutterFormatConsistency(t *testing.T) {
	// Test that the gutter format consistently produces stable visual width
	// across different status strings (ASCII, emoji, wide chars)
	statusTests := []string{
		"-----",
		"cached",
		"render",
		"🎵",      // single emoji
		"✓ ok",    // emoji with text
		"日本語",  // wide characters
		"📺 test", // emoji and ASCII
	}

	statusWidth := 5

	for _, status := range statusTests {
		t.Run(status, func(t *testing.T) {
			// Simulate the gutter construction:
			// cursor (2 visual width) + idx (2 visual width) + space (1) + paddedStatus (5 visual width)
			truncatedStatus := tui.TruncateWithEllipsis(status, statusWidth)
			paddedStatus := lipgloss.NewStyle().Width(statusWidth).Render(truncatedStatus)

			// The gutter format is: "%s%s %s" where cursor is 2 chars, idx is 2 chars, paddedStatus is statusWidth
			cursor := "  " // either "  " or "▸ " both 2 chars
			idx := "01"    // 2 chars

			// Construct gutter the same way as in the actual code
			gutter := cursor + idx + " " + paddedStatus

			// Verify the gutter has stable visual width
			expectedGutterWidth := 2 + 2 + 1 + statusWidth // cursor + idx + space + paddedStatus
			actualGutterWidth := lipgloss.Width(gutter)

			if actualGutterWidth != expectedGutterWidth {
				t.Errorf("gutter visual width = %d, want %d (status=%q, paddedStatus=%q, gutter=%q)",
					actualGutterWidth, expectedGutterWidth, status, paddedStatus, gutter)
			}
		})
	}
}
