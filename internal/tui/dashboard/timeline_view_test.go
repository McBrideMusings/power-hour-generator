package dashboard

import (
	"strings"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

// TestTimelineViewPlaybackOrderScrollIndicatorFitsContentBudget verifies
// that when the playback order panel is scrolled mid-list, its scroll
// indicators are subtracted from the visible-row budget before rendering
// (#61 / #127) rather than pushing content past the footer. The sequence
// panel is fixed-height and never scrolls, so it never produces an
// indicator — only the playback order panel is exercised here.
func TestTimelineViewPlaybackOrderScrollIndicatorFitsContentBudget(t *testing.T) {
	m := testTimelineModel(t)

	termHeight := 24
	m.timelineView.termWidth = 120
	m.timelineView.termHeight = termHeight

	// A short, fixed sequence — it renders in full regardless of terminal
	// height, so it doesn't compete with the playback order panel's budget.
	m.timelineView.sequence = []config.SequenceEntry{{Collection: "songs"}}

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

	// Scroll the playback order panel to mid-list so both indicators render.
	m.timelineView.resScrollTop = 20

	view := m.timelineView.view(nil)

	upCount := strings.Count(view, "↑ ")
	downCount := strings.Count(view, "↓ ")
	if upCount < 1 {
		t.Errorf("expected at least 1 up indicator, got %d", upCount)
	}
	if downCount < 1 {
		t.Errorf("expected at least 1 down indicator, got %d", downCount)
	}

	// Count newlines in the view; assert it doesn't exceed the content budget.
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
		Name:   "songs",
		Config: config.CollectionConfig{Display: "{title}"},
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

// TestConfirmDeletePromptRoutesThroughFooterInConfirmStyle verifies that,
// unlike the collection view (which inserts the prompt beneath the target
// row), the timeline view has no per-row insertion point — confirmDelete
// is routed through the single shared footer help row via renderHelpRow,
// styled with confirmStyle, and its presence does not change the total
// rendered line count (no row drift) versus the no-prompt render.
func TestConfirmDeletePromptRoutesThroughFooterInConfirmStyle(t *testing.T) {
	withANSIColorProfile(t)

	m := testTimelineModel(t)
	m.timelineView.termWidth = 100
	m.timelineView.termHeight = 40
	m.timelineView.sequence = []config.SequenceEntry{
		{Collection: "songs"},
		{Collection: "songs"},
	}
	m.timelineView.resolved = []project.TimelineEntry{
		{Collection: "songs", Index: 1, Sequence: 0},
		{Collection: "songs", Index: 2, Sequence: 1},
	}
	m.collections["songs"] = project.Collection{
		Name: "songs",
		Rows: []csvplan.CollectionRow{
			{Index: 0, DurationSeconds: 60, CustomFields: map[string]string{"title": "Song A", "artist": "Artist"}},
			{Index: 1, DurationSeconds: 60, CustomFields: map[string]string{"title": "Song B", "artist": "Artist"}},
		},
	}
	m.timelineView.collections = m.collections

	without := m.timelineView.view(nil)
	withoutLines := strings.Count(without, "\n")

	m.timelineView.confirmDelete = "Delete sequence entry 0? [y/n]"
	with := m.timelineView.view(nil)
	withLines := strings.Count(with, "\n")

	if withLines != withoutLines {
		t.Errorf("expected total line count to stay unchanged (prompt replaces the footer, not adds a row): without=%d with=%d\nwith:\n%s", withoutLines, withLines, with)
	}

	wantFooter := helpRowText(m.timelineView.confirmDelete, confirmStyle, m.timelineView.termWidth)
	if !strings.Contains(with, wantFooter) {
		t.Errorf("footer does not carry the confirmStyle-rendered prompt.\nwant substring: %q\ngot view:\n%s", wantFooter, with)
	}

	// Sanity: styled footer must carry real ANSI SGR codes, not have
	// degraded to plain text. Search for the line containing an ANSI escape
	// sequence rather than relying on position heuristic.
	lines := strings.Split(with, "\n")
	var footerLine string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "\x1b[") {
			footerLine = lines[i]
			break
		}
	}
	if footerLine == "" {
		t.Errorf("footer line not found; no line contains ANSI escape codes in view:\n%s", with)
	}
}

// TestEntryLabelUsesCollectionDisplayTemplate verifies that the playback
// order label is driven by the collection's display template (ADR 0002) —
// no hardcoded title/artist ladder — and that an unset display falls back to
// a cleaned basename of the row's link.
func TestEntryLabelUsesCollectionDisplayTemplate(t *testing.T) {
	m := testTimelineModel(t)

	m.collections["interstitials"] = project.Collection{
		Name:   "interstitials",
		Config: config.CollectionConfig{Display: "{label}"},
		Rows: []csvplan.CollectionRow{
			{
				Link:         "/media/Spaced.(1999).S01E06.Epiphanies.WEBDL-1080p.x264.AAC.[EN].MuTT.mkv",
				CustomFields: map[string]string{"label": "Epiphanies clip"},
			},
			{
				Link:         "/media/Spaced.(1999).S01E06.Epiphanies.WEBDL-1080p.x264.AAC.[EN].MuTT.mkv",
				CustomFields: map[string]string{"label": ""},
			},
		},
	}
	m.timelineView.collections = m.collections

	withLabel := m.timelineView.entryLabel(project.TimelineEntry{Collection: "interstitials", Index: 1})
	if want := "Epiphanies clip"; withLabel != want {
		t.Errorf("entryLabel with display template: got %q, want %q", withLabel, want)
	}

	fallback := m.timelineView.entryLabel(project.TimelineEntry{Collection: "interstitials", Index: 2})
	if want := "Spaced (1999) S01E06 Epiphanies"; fallback != want {
		t.Errorf("entryLabel falling back on empty label: got %q, want %q", fallback, want)
	}
}

// A long sequence must not consume the whole content budget: resPanelHeight
// is contentHeight minus seqPanelHeight, and at zero the playback order panel
// disappears along with the footer beneath it.
func TestSequencePanelCapLeavesRoomForPlaybackOrder(t *testing.T) {
	v := timelineView{termHeight: 30}
	for i := 0; i < 200; i++ {
		v.sequence = append(v.sequence, config.SequenceEntry{Collection: "songs"})
	}

	if got := v.resPanelHeight(); got < minPlaybackPanelLines {
		t.Fatalf("playback order panel starved: resPanelHeight()=%d, want >= %d", got, minPlaybackPanelLines)
	}
	if v.seqPanelHeight() >= v.contentHeight() {
		t.Fatalf("sequence panel took the whole budget: seq=%d content=%d", v.seqPanelHeight(), v.contentHeight())
	}
	if v.sequenceOverflow() == 0 {
		t.Fatal("200 entries in a 30-line terminal should report overflow")
	}
}

// The cap must not bind for an ordinary project — the panel still fits its
// entries exactly, with nothing clipped and no overflow line.
func TestSequencePanelFitsExactlyWhenShort(t *testing.T) {
	v := timelineView{termHeight: 40}
	for i := 0; i < 4; i++ {
		v.sequence = append(v.sequence, config.SequenceEntry{Collection: "songs"})
	}

	if got := v.seqPanelHeight(); got != 4 {
		t.Fatalf("seqPanelHeight() = %d, want 4", got)
	}
	if got := v.sequenceOverflow(); got != 0 {
		t.Fatalf("sequenceOverflow() = %d, want 0", got)
	}
}
