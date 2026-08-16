package dashboard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

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
