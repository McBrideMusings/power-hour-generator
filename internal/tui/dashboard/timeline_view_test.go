package dashboard

import (
	"strings"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

// TestTimelineViewFitsContentBudgetWithBothScrollIndicators verifies that when
// both scroll indicators are visible (sequence panel scrolled mid-list, resolved
// panel scrolled mid-list), the total rendered lines stay within the termHeight-5
// content budget and the footer remains visible.
//
// This is a regression test for the #61 / #127 fix: each scroll indicator must
// be subtracted from the visible-row budget before rendering, so indicators
// don't push content past the height limit. The fix is conditional on whether
// the indicator will actually be rendered.
func TestTimelineViewFitsContentBudgetWithBothScrollIndicators(t *testing.T) {
	m := testTimelineModel(t)

	// Small terminal with just enough height to force scrolling in both panels.
	termHeight := 24
	m.timelineView.termWidth = 120
	m.timelineView.termHeight = termHeight

	// Populate sequence with enough entries to force scrolling.
	// seqPanelHeight auto-grows to sequenceLinesNeeded, so we need entries
	// that exceed (termHeight - 13) / 4 ≈ 2.75 ≈ 3 lines before clamping.
	m.timelineView.sequence = make([]config.SequenceEntry, 20)
	for i := range m.timelineView.sequence {
		m.timelineView.sequence[i] = config.SequenceEntry{Collection: "songs"}
	}

	// Populate resolved preview with enough entries to force scrolling.
	m.timelineView.resolved = make([]project.TimelineEntry, 40)
	for i := range m.timelineView.resolved {
		m.timelineView.resolved[i] = project.TimelineEntry{
			Collection: "songs",
			Index:      i + 1, // 1-based index for row lookup
			Sequence:   i,
		}
	}

	// Set up collections so entries can be rendered.
	songsCollection := project.Collection{
		Name: "songs",
		Rows: []csvplan.CollectionRow{},
	}
	for i := 0; i < 40; i++ {
		songsCollection.Rows = append(songsCollection.Rows, csvplan.CollectionRow{
			Index:           i,
			DurationSeconds: 60,
			CustomFields: map[string]string{
				"title":  "Song " + string(rune('A'+i%26)),
				"artist": "Artist",
			},
		})
	}
	m.collections["songs"] = songsCollection
	m.timelineView.collections = m.collections

	// Scroll both panels to mid-list so both up and down indicators will render.
	m.timelineView.seqScrollTop = 10 // not at top → up indicator
	m.timelineView.resScrollTop = 20 // not at top → up indicator

	view := m.timelineView.view(nil)

	// Verify both up and down indicators are present in the output.
	upCount := strings.Count(view, "↑ ")
	downCount := strings.Count(view, "↓ ")
	if upCount < 2 {
		t.Errorf("expected at least 2 up indicators, got %d", upCount)
	}
	if downCount < 2 {
		t.Errorf("expected at least 2 down indicators, got %d", downCount)
	}

	// Count newlines in the view. The content area has termHeight - 5 lines
	// (termHeight - 13 for chrome/padding + 8 for section labels = termHeight - 5).
	// The footer is an additional line, so total is termHeight - 4.
	// Assert we don't exceed termHeight - 5 for content.
	lineCount := strings.Count(view, "\n")
	contentBudget := termHeight - 5
	if lineCount > contentBudget {
		t.Errorf("view has %d newlines, content budget is %d (termHeight %d - 5)",
			lineCount, contentBudget, termHeight)
		t.Logf("view:\n%s", view)
	}
}

// TestTimelineViewWastesNoRowWithoutScrollIndicators verifies that when neither
// scroll indicator is shown (sequence at top or no scroll needed, resolved at top
// or no scroll needed), no row is wasted by an unconditional subtraction.
//
// This ensures that the conditional `visible--` logic (only when indicator will
// render) doesn't accidentally drop a row when the condition is false.
func TestTimelineViewWastesNoRowWithoutScrollIndicators(t *testing.T) {
	m := testTimelineModel(t)

	m.timelineView.termWidth = 120
	m.timelineView.termHeight = 40

	// Small sequence that fits without scrolling (no scrollbars).
	m.timelineView.sequence = []config.SequenceEntry{
		{Collection: "songs"},
		{Collection: "songs"},
		{Collection: "songs"},
	}

	// Small resolved list that also fits without scrolling.
	// Choose a count small enough that it doesn't trigger the down indicator.
	m.timelineView.resolved = []project.TimelineEntry{
		{Collection: "songs", Index: 1, Sequence: 0}, // 1-based index for row lookup
		{Collection: "songs", Index: 2, Sequence: 1},
		{Collection: "songs", Index: 3, Sequence: 2},
	}

	m.collections["songs"] = project.Collection{
		Name: "songs",
		Rows: []csvplan.CollectionRow{
			{
				Index:           0,
				DurationSeconds: 60,
				CustomFields:    map[string]string{"title": "Song A", "artist": "Artist"},
			},
			{
				Index:           1,
				DurationSeconds: 60,
				CustomFields:    map[string]string{"title": "Song B", "artist": "Artist"},
			},
			{
				Index:           2,
				DurationSeconds: 60,
				CustomFields:    map[string]string{"title": "Song C", "artist": "Artist"},
			},
		},
	}
	m.timelineView.collections = m.collections

	// Keep scroll at top so no indicators render.
	m.timelineView.seqScrollTop = 0
	m.timelineView.resScrollTop = 0

	view := m.timelineView.view(nil)

	// Verify no scroll indicators are present.
	if strings.Contains(view, "↑") || strings.Contains(view, "↓") {
		t.Errorf("view should not contain scroll indicators when scrolled to top and content fits")
	}

	// Verify all resolved entries are present in the output.
	// The titles should all appear since none were dropped.
	if !strings.Contains(view, "Song A") {
		t.Error("Song A missing from view; row was dropped")
		t.Logf("view:\n%s", view)
	}
	if !strings.Contains(view, "Song B") {
		t.Error("Song B missing from view; row was dropped")
		t.Logf("view:\n%s", view)
	}
	if !strings.Contains(view, "Song C") {
		t.Error("Song C missing from view; row was dropped")
		t.Logf("view:\n%s", view)
	}
}
