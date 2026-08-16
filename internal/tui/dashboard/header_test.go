package dashboard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"powerhour/internal/paths"
)

func TestRenderHeaderTruncatesToTerminalWidth(t *testing.T) {
	collectionNames := []string{
		"songs", "intros", "outros", "intermissions", "credits",
		"a-very-long-collection-name-that-goes-on-and-on",
	}
	summaries := map[string]collectionSummary{}
	for _, name := range collectionNames {
		summaries[name] = collectionSummary{Total: 20, Cached: 18}
	}

	m := Model{
		pp:              paths.ProjectPaths{Root: "/full/project/path"},
		viewNames:       []string{"Timeline", "Songs", "Intros", "Outros", "Intermissions", "Credits"},
		activeView:      0,
		collectionNames: collectionNames,
		summaries:       summaries,
		toolWarning:     "yt-dlp is out of date",
		termWidth:       40,
	}

	out := renderHeader(m)
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.termWidth {
			t.Fatalf("line %d width = %d, want <= %d (line: %q)", i, w, m.termWidth, line)
		}
	}
}

func TestRenderHeaderUnchangedAtWideTerminal(t *testing.T) {
	collectionNames := []string{"songs", "intros"}
	summaries := map[string]collectionSummary{
		"songs":  {Total: 20, Cached: 18},
		"intros": {Total: 5, Cached: 5},
	}

	m := Model{
		pp:              paths.ProjectPaths{Root: "/full/project/path"},
		viewNames:       []string{"Timeline", "Songs", "Intros"},
		activeView:      1,
		collectionNames: collectionNames,
		summaries:       summaries,
		toolWarning:     "yt-dlp is out of date",
		termWidth:       200,
	}

	out := renderHeader(m)

	for _, want := range []string{
		"POWER HOUR",
		"1:Timeline",
		"2:Songs",
		"3:Intros",
		"songs: ",
		"18",
		"/20",
		"intros: ",
		"5",
		"/5",
		"⚠ yt-dlp is out of date",
		"/full/project/path",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q; got:\n%s", want, out)
		}
	}
}

func TestRenderSegmentsWithBudgetEmoji(t *testing.T) {
	// Each 🎬 is 1 rune but 2 terminal columns; budgeting by rune count would
	// let this segment overflow the width.
	segments := []headerSegment{
		{"🎬🎬🎬🎬🎬", lipgloss.NewStyle()}, // 5 runes, 10 columns
	}

	for _, width := range []int{1, 4, 5, 9, 10, 11} {
		out := renderSegmentsWithBudget(segments, width)
		if w := runewidth.StringWidth(out); w > width {
			t.Fatalf("width=%d: rendered width = %d, want <= %d (out: %q)", width, w, width, out)
		}
	}
}

func TestRenderSegmentsWithBudgetCJK(t *testing.T) {
	// Each CJK character is 1 rune but 2 terminal columns.
	segments := []headerSegment{
		{"日本語テキスト", lipgloss.NewStyle()}, // 7 runes, 14 columns
	}

	for _, width := range []int{1, 4, 5, 13, 14, 15} {
		out := renderSegmentsWithBudget(segments, width)
		if w := runewidth.StringWidth(out); w > width {
			t.Fatalf("width=%d: rendered width = %d, want <= %d (out: %q)", width, w, width, out)
		}
	}
}

func TestRenderHeaderZeroWidthDoesNotPanic(t *testing.T) {
	m := Model{
		pp:              paths.ProjectPaths{Root: "/full/project/path"},
		viewNames:       []string{"Timeline", "Songs"},
		activeView:      0,
		collectionNames: []string{"songs"},
		summaries:       map[string]collectionSummary{"songs": {Total: 1, Cached: 1}},
		termWidth:       0,
	}

	out := renderHeader(m)
	if !strings.Contains(out, "POWER HOUR") {
		t.Fatalf("output missing POWER HOUR at width 0; got:\n%s", out)
	}
}
