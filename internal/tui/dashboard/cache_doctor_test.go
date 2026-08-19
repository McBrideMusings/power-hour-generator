package dashboard

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"powerhour/internal/cache"
	"powerhour/internal/cachedoctor"
	"powerhour/internal/paths"
)

// newTestDoctorOverlay builds a doctor overlay with a fully-populated
// context block (uploader/channel/track/album) and the given set of known
// artists driving the fuzzy-suggestion list, so tests can control exactly
// how many optional lines are in play.
func newTestDoctorOverlay(knownArtists []string, w, h int) cacheDoctorOverlay {
	entry := cache.Entry{
		Source:     "https://youtube.com/watch?v=abc123",
		CachedPath: "/cache/some/long/path/to/file.mp4",
		Uploader:   "Some Uploader",
		Channel:    "Some Channel",
		Track:      "Some Track",
		Album:      "Some Album",
	}
	finding := cachedoctor.Finding{
		CurrentTitle:   "old title",
		CurrentArtist:  "old artist",
		ProposedTitle:  "New Title",
		ProposedArtist: "New Artist",
		Confidence:     "medium",
		Reasons:        []string{"used track as title", "applied artist alias"},
	}
	items := []doctorItem{{entry: entry, finding: finding}}
	o := newCacheDoctorOverlay(items, knownArtists, w, h, 0)
	// Put the ARTIST field in edit mode with a query that fuzzy-matches
	// every "Artist X" entry in knownArtists, so artistSuggestions() is
	// non-empty and under our control via len(knownArtists).
	o.activeField = 1
	o.editArtist = "art"
	o.artistCursor = len(o.editArtist)
	o.artistTouched = true
	return o
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n")
}

// TestDoctorOverlayViewRespectsHeightBudget asserts the termHeight-5
// invariant holds across a range of terminal heights, including
// pathologically short ones where even the required sections don't fit.
func TestDoctorOverlayViewRespectsHeightBudget(t *testing.T) {
	known := []string{"Artist A", "Artist B", "Artist C", "Artist D", "Artist E", "Artist F"}

	for _, h := range []int{0, 1, 5, 10, 14, 17, 19, 22, 25, 40} {
		o := newTestDoctorOverlay(known, 80, h)
		out := o.view()
		if h <= 0 {
			continue // no cap when termHeight is unset — matches existing callers.
		}
		max := h - 5
		if max < 0 {
			max = 0
		}
		if got := countLines(out); got > max {
			t.Errorf("termHeight=%d: view() produced %d lines, want <= %d (maxLines)\noutput:\n%s", h, got, max, out)
		}
	}
}

// TestDoctorOverlayNeverDroppableSectionsSurviveTallTerminal is the
// control case: on a tall terminal nothing is dropped and every
// never-droppable section renders, along with the full optional content.
func TestDoctorOverlayNeverDroppableSectionsSurviveTallTerminal(t *testing.T) {
	known := []string{"Artist A", "Artist B"}
	o := newTestDoctorOverlay(known, 80, 40)
	out := o.view()

	for _, want := range []string{"TITLE", "ARTIST", "Current:", "New:", "used track metadata as title", "CONTEXT", "Uploader", "Channel", "Track", "Album"} {
		if !strings.Contains(out, want) {
			t.Errorf("tall terminal: expected output to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hidden") || strings.Contains(out, "more") {
		t.Errorf("tall terminal: expected no omission markers, got:\n%s", out)
	}
}

// TestDoctorOverlayDropsContextBeforeSuggestions verifies the priority
// order: when the budget is too tight for everything but still has room
// for the suggestion list once context is reduced to a marker, context is
// the one that gets dropped — the suggestion list survives intact.
func TestDoctorOverlayDropsContextBeforeSuggestions(t *testing.T) {
	// Two known artists -> exactly two suggestion lines. Sized so that:
	// requiredCount=13, contextFull=6, suggestFull=2 (full=21).
	// termHeight=22 -> maxLines=17 -> budget=4: too small for full
	// context (6) but enough for a 1-line marker + both suggestions (3).
	known := []string{"Artist A", "Artist B"}
	o := newTestDoctorOverlay(known, 80, 22)
	out := o.view()

	if !strings.Contains(out, "hidden") {
		t.Errorf("expected context to be marked hidden when budget is tight, got:\n%s", out)
	}
	if strings.Contains(out, "Uploader") || strings.Contains(out, "Channel") {
		t.Errorf("expected context fields to be dropped, got:\n%s", out)
	}
	if !strings.Contains(out, "Artist A") || !strings.Contains(out, "Artist B") {
		t.Errorf("expected both suggestions to survive while context was dropped, got:\n%s", out)
	}
	if strings.Contains(out, "more") {
		t.Errorf("expected suggestions to be intact (no cap marker) once context absorbed the cut, got:\n%s", out)
	}
	if got, max := countLines(out), 22-5; got > max {
		t.Errorf("got %d lines, want <= %d", got, max)
	}
}

// TestDoctorOverlayMarksOmissionsInline verifies that once context is
// already reduced, further budget pressure caps the suggestion list too —
// and that every cut leaves a visible marker rather than disappearing
// silently.
func TestDoctorOverlayMarksOmissionsInline(t *testing.T) {
	known := []string{"Artist A", "Artist B"}
	// termHeight=19 -> maxLines=14 -> budget=1: only enough for the
	// context marker itself; suggestions get dropped entirely.
	o := newTestDoctorOverlay(known, 80, 19)
	out := o.view()

	if !strings.Contains(out, "hidden") {
		t.Errorf("expected context omission marker, got:\n%s", out)
	}
	if !strings.Contains(out, "more") {
		t.Errorf("expected suggestions omission marker, got:\n%s", out)
	}
	if strings.Contains(out, "Artist A") || strings.Contains(out, "Artist B") {
		t.Errorf("expected suggestion names to be fully dropped, got:\n%s", out)
	}
	// Never-droppable content must still be present.
	for _, want := range []string{"TITLE", "ARTIST", "used track metadata as title"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected never-droppable content %q to survive, got:\n%s", want, out)
		}
	}
	if got, max := countLines(out), 19-5; got > max {
		t.Errorf("got %d lines, want <= %d", got, max)
	}
}

// TestDoctorOverlayForwardDelete tests the Delete key handler for removing
// the character to the right of the cursor. It verifies that multi-byte runes
// are deleted whole and that the cursor position is preserved.
func TestDoctorOverlayForwardDelete(t *testing.T) {
	tests := []struct {
		name           string
		activeField    int
		initialText    string
		initialCursor  int
		expectedText   string
		expectedCursor int
	}{
		{
			name:           "artist field: delete mid-string ASCII char",
			activeField:    1,
			initialText:    "Sample Artist",
			initialCursor:  6, // cursor after "Sample" before space
			expectedText:   "SampleArtist",
			expectedCursor: 6,
		},
		{
			name:           "title field: delete multi-byte rune",
			activeField:    0,
			initialText:    "Café Song",
			initialCursor:  3, // cursor at start of 'é'
			expectedText:   "Caf Song",
			expectedCursor: 3,
		},
		{
			name:           "title field: cursor at end of text (no-op)",
			activeField:    0,
			initialText:    "Test Title",
			initialCursor:  len("Test Title"),
			expectedText:   "Test Title",
			expectedCursor: len("Test Title"),
		},
		{
			name:           "artist field: delete at start of text",
			activeField:    1,
			initialText:    "Artist Name",
			initialCursor:  0,
			expectedText:   "rtist Name",
			expectedCursor: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newTestDoctorOverlay([]string{"Artist A"}, 80, 40)
			o.activeField = tc.activeField

			if tc.activeField == 0 {
				o.editTitle = tc.initialText
				o.titleCursor = tc.initialCursor
				o.titleTouched = false
			} else {
				o.editArtist = tc.initialText
				o.artistCursor = tc.initialCursor
				o.artistTouched = false
			}

			// Simulate Delete key press
			o.handleKey(tea.KeyMsg{Type: tea.KeyDelete})

			// Verify the result
			var gotText string
			var gotCursor int

			if tc.activeField == 0 {
				gotText = o.editTitle
				gotCursor = o.titleCursor
				if !o.titleTouched && tc.initialCursor < len(tc.initialText) {
					t.Errorf("titleTouched should be set when deletion occurs")
				}
			} else {
				gotText = o.editArtist
				gotCursor = o.artistCursor
				if !o.artistTouched && tc.initialCursor < len(tc.initialText) {
					t.Errorf("artistTouched should be set when deletion occurs")
				}
			}

			if gotText != tc.expectedText {
				t.Errorf("text mismatch: got %q, want %q", gotText, tc.expectedText)
			}
			if gotCursor != tc.expectedCursor {
				t.Errorf("cursor mismatch: got %d, want %d", gotCursor, tc.expectedCursor)
			}
		})
	}
}

// twoItemDoctorOverlay builds a doctor overlay with two entries carrying
// distinct stable Identifiers, for tests that need to move the cursor
// between entries mid-requery.
func twoItemDoctorOverlay() cacheDoctorOverlay {
	items := []doctorItem{
		{
			entry: cache.Entry{Identifier: "id-0", Source: "https://youtube.com/watch?v=aaa000"},
			finding: cachedoctor.Finding{
				Identifier:     "id-0",
				CurrentTitle:   "old title 0",
				CurrentArtist:  "old artist 0",
				ProposedTitle:  "Proposed Title 0",
				ProposedArtist: "Proposed Artist 0",
				Confidence:     "medium",
			},
		},
		{
			entry: cache.Entry{Identifier: "id-1", Source: "https://youtube.com/watch?v=bbb111"},
			finding: cachedoctor.Finding{
				Identifier:     "id-1",
				CurrentTitle:   "old title 1",
				CurrentArtist:  "old artist 1",
				ProposedTitle:  "Proposed Title 1",
				ProposedArtist: "Proposed Artist 1",
				Confidence:     "medium",
			},
		},
	}
	return newCacheDoctorOverlay(items, nil, 80, 40, 0)
}

// TestDoctorOverlayRequeryIgnoresStaleTarget covers the race: a requery is
// dispatched against entry 0, the cursor moves to entry 1 before the result
// lands, and applyRequery must write the result into entry 0 (by
// identifier) rather than clobbering whatever is now under the cursor
// (entry 1) or the edit buffers currently on screen.
func TestDoctorOverlayRequeryIgnoresStaleTarget(t *testing.T) {
	o := twoItemDoctorOverlay()

	// Snapshot entry 0's identifier as startDoctorRequery would.
	queriedID := o.findings[0].finding.Identifier
	o.requerying = true
	o.requeryID = queriedID

	// Cursor moves to entry 1 (and the visible edit buffers follow) before
	// the async requery result arrives.
	o.cursor = 1
	o.loadCurrentEntry()
	entry1TitleBefore := o.editTitle
	entry1ArtistBefore := o.editArtist

	normCfg := cache.NormalizationConfig{}
	info := cache.RemoteIDInfo{Title: "Fresh Title", Artist: "Fresh Artist"}
	o.applyRequery(queriedID, info, normCfg)

	// Entry 0 (queried) received the update.
	if o.findings[0].finding.ProposedTitle != "Fresh Title" {
		t.Errorf("entry 0 ProposedTitle = %q, want %q", o.findings[0].finding.ProposedTitle, "Fresh Title")
	}
	if o.findings[0].finding.ProposedArtist != "Fresh Artist" {
		t.Errorf("entry 0 ProposedArtist = %q, want %q", o.findings[0].finding.ProposedArtist, "Fresh Artist")
	}

	// Entry 1 (now under the cursor) is untouched.
	if o.findings[1].finding.ProposedTitle != "Proposed Title 1" {
		t.Errorf("entry 1 ProposedTitle mutated: got %q", o.findings[1].finding.ProposedTitle)
	}
	if o.findings[1].finding.ProposedArtist != "Proposed Artist 1" {
		t.Errorf("entry 1 ProposedArtist mutated: got %q", o.findings[1].finding.ProposedArtist)
	}

	// The visible edit buffers (for entry 1) are unchanged by entry 0's result.
	if o.editTitle != entry1TitleBefore {
		t.Errorf("editTitle changed: got %q, want unchanged %q", o.editTitle, entry1TitleBefore)
	}
	if o.editArtist != entry1ArtistBefore {
		t.Errorf("editArtist changed: got %q, want unchanged %q", o.editArtist, entry1ArtistBefore)
	}

	// requerying/requeryID cleared on every exit path.
	if o.requerying {
		t.Error("requerying should be cleared after applyRequery")
	}
	if o.requeryID != "" {
		t.Errorf("requeryID should be cleared, got %q", o.requeryID)
	}
}

// TestDoctorOverlayRequeryDiscardsUnknownIdentifier covers the case where
// the queried entry has vanished from findings entirely (e.g. removed)
// before the result arrives: the result must be discarded, not applied to
// whatever is under the cursor, and requerying must still clear so the
// overlay doesn't freeze (handleKey swallows all input while requerying).
func TestDoctorOverlayRequeryDiscardsUnknownIdentifier(t *testing.T) {
	o := twoItemDoctorOverlay()
	o.requerying = true
	o.requeryID = "id-gone"

	entry0TitleBefore := o.findings[0].finding.ProposedTitle
	entry0ArtistBefore := o.findings[0].finding.ProposedArtist
	entry1TitleBefore := o.findings[1].finding.ProposedTitle
	entry1ArtistBefore := o.findings[1].finding.ProposedArtist

	normCfg := cache.NormalizationConfig{}
	info := cache.RemoteIDInfo{Title: "Should Not Apply", Artist: "Should Not Apply"}
	o.applyRequery("id-gone", info, normCfg)

	if o.findings[0].finding.ProposedTitle != entry0TitleBefore || o.findings[0].finding.ProposedArtist != entry0ArtistBefore {
		t.Errorf("entry 0 mutated by discarded requery: got title=%q artist=%q", o.findings[0].finding.ProposedTitle, o.findings[0].finding.ProposedArtist)
	}
	if o.findings[1].finding.ProposedTitle != entry1TitleBefore || o.findings[1].finding.ProposedArtist != entry1ArtistBefore {
		t.Errorf("entry 1 mutated by discarded requery: got title=%q artist=%q", o.findings[1].finding.ProposedTitle, o.findings[1].finding.ProposedArtist)
	}
	if o.requerying {
		t.Error("requerying should be cleared after a discarded applyRequery")
	}
	if o.requeryStatus == "" {
		t.Error("requeryStatus should explain the discard")
	}
}

// TestDoctorRequeryEmptyIdentifierNotAppliedByCursor covers entries with no
// stable Identifier: applyRequery must never fall back to applying the
// result to whatever is under the cursor just because the snapshotted
// identifier was empty (that was the bug behind #176). It must also clear
// requerying so the overlay doesn't freeze (handleKey swallows all input
// while requerying is true).
func TestDoctorRequeryEmptyIdentifierNotAppliedByCursor(t *testing.T) {
	items := []doctorItem{
		{
			entry: cache.Entry{Identifier: "", Source: "https://youtube.com/watch?v=ccc222"},
			finding: cachedoctor.Finding{
				Identifier:     "",
				CurrentTitle:   "old title",
				CurrentArtist:  "old artist",
				ProposedTitle:  "Proposed Title",
				ProposedArtist: "Proposed Artist",
				Confidence:     "medium",
			},
		},
	}
	o := newCacheDoctorOverlay(items, nil, 80, 40, 0)
	o.requerying = true
	o.requeryID = ""
	o.cursor = 0

	normCfg := cache.NormalizationConfig{}
	info := cache.RemoteIDInfo{Title: "Should Not Apply", Artist: "Should Not Apply"}
	o.applyRequery("", info, normCfg)

	if o.findings[0].finding.ProposedTitle != "Proposed Title" {
		t.Errorf("finding mutated by empty-identifier requery: got title=%q", o.findings[0].finding.ProposedTitle)
	}
	if o.findings[0].finding.ProposedArtist != "Proposed Artist" {
		t.Errorf("finding mutated by empty-identifier requery: got artist=%q", o.findings[0].finding.ProposedArtist)
	}
	if o.requerying {
		t.Error("requerying should be cleared after a discarded applyRequery, or the overlay freezes")
	}
	if o.requeryStatus == "" {
		t.Error("requeryStatus should explain the discard")
	}
}

// TestDoctorRequeryFromPreviousOverlayInstanceIsIgnored covers the
// close-and-reopen-mid-requery case: the doctor overlay is closed while a
// requery is still in flight, reopened as a new instance, and the stale
// result from the first instance arrives. It must be dropped entirely,
// including when it coincidentally carries the same (or an equally empty)
// identifier as something in the new overlay — the instance token, not the
// identifier, is what disambiguates.
func TestDoctorRequeryFromPreviousOverlayInstanceIsIgnored(t *testing.T) {
	cases := []struct {
		name       string
		identifier string
		requeryID  string
	}{
		{name: "non-empty identifier", identifier: "id-0", requeryID: "id-0"},
		{name: "empty identifier coincidental match", identifier: "", requeryID: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			firstOverlay := twoItemDoctorOverlay()
			firstOverlay.instance = 1
			firstOverlay.requerying = true
			firstOverlay.requeryID = tc.requeryID

			// Close the first overlay, reopen a second instance (as
			// openDoctorOverlay would, with an incremented instance number).
			secondOverlay := twoItemDoctorOverlay()
			secondOverlay.instance = 2
			secondOverlay.requeryID = tc.requeryID

			origTitle0 := secondOverlay.findings[0].finding.ProposedTitle
			origArtist0 := secondOverlay.findings[0].finding.ProposedArtist

			m := Model{
				overlay:       overlayDoctor,
				doctorOverlay: &secondOverlay,
			}

			msg := doctorRequeryDoneMsg{
				identifier: tc.identifier,
				info:       cache.RemoteIDInfo{Title: "Stale Title", Artist: "Stale Artist"},
				instance:   firstOverlay.instance,
			}

			updated, _ := m.Update(msg)
			um := updated.(Model)

			if um.doctorOverlay.findings[0].finding.ProposedTitle != origTitle0 {
				t.Errorf("finding mutated by stale-instance requery: got title=%q, want %q", um.doctorOverlay.findings[0].finding.ProposedTitle, origTitle0)
			}
			if um.doctorOverlay.findings[0].finding.ProposedArtist != origArtist0 {
				t.Errorf("finding mutated by stale-instance requery: got artist=%q, want %q", um.doctorOverlay.findings[0].finding.ProposedArtist, origArtist0)
			}
			if um.doctorOverlay.requeryStatus != "" {
				t.Errorf("requeryStatus should be untouched by a stale-instance message, got %q", um.doctorOverlay.requeryStatus)
			}
		})
	}
}

// TestRememberArtistGrowsPool verifies that rememberArtist appends a new
// artist name to the overlay's knownArtists pool.
func TestRememberArtistGrowsPool(t *testing.T) {
	o := newTestDoctorOverlay([]string{"Beatles", "Rolling Stones"}, 80, 40)
	initialLen := len(o.knownArtists)

	o.rememberArtist("Queen")

	if got := len(o.knownArtists); got != initialLen+1 {
		t.Errorf("knownArtists length = %d, want %d", got, initialLen+1)
	}
	if !contains(o.knownArtists, "Queen") {
		t.Errorf("Queen not found in knownArtists: %v", o.knownArtists)
	}
}

// TestRememberArtistDeduplicates verifies that rememberArtist does not add
// duplicate artist names, including case-insensitive duplicates.
func TestRememberArtistDeduplicates(t *testing.T) {
	o := newTestDoctorOverlay([]string{"Beatles", "Rolling Stones"}, 80, 40)
	initialLen := len(o.knownArtists)

	// Exact duplicate
	o.rememberArtist("Beatles")
	if got := len(o.knownArtists); got != initialLen {
		t.Errorf("after exact duplicate: knownArtists length = %d, want %d (no growth)", got, initialLen)
	}

	// Case variant
	o.rememberArtist("beatles")
	if got := len(o.knownArtists); got != initialLen {
		t.Errorf("after case variant: knownArtists length = %d, want %d (no growth)", got, initialLen)
	}

	// Different name should grow
	o.rememberArtist("Queen")
	if got := len(o.knownArtists); got != initialLen+1 {
		t.Errorf("after new name: knownArtists length = %d, want %d", got, initialLen+1)
	}
}

// TestRememberArtistTrimsWhitespace verifies that rememberArtist trims
// leading/trailing whitespace and skips empty names.
func TestRememberArtistTrimsWhitespace(t *testing.T) {
	o := newTestDoctorOverlay([]string{"Beatles"}, 80, 40)
	initialLen := len(o.knownArtists)

	o.rememberArtist("  Queen  ")
	if got := len(o.knownArtists); got != initialLen+1 {
		t.Errorf("after whitespace trim: length = %d, want %d", got, initialLen+1)
	}
	if !contains(o.knownArtists, "Queen") {
		t.Errorf("Queen (trimmed) not found in knownArtists: %v", o.knownArtists)
	}

	// Empty/whitespace-only names should be skipped
	o.rememberArtist("   ")
	if got := len(o.knownArtists); got != initialLen+1 {
		t.Errorf("after empty string: length = %d, want %d (no growth)", got, initialLen+1)
	}
}

// TestSaveAndAutocompleteSamePerson verifies the core use case: save an
// artist name on one entry, then see it offered as an autocomplete
// suggestion on a later entry in the same session.
func TestSaveAndAutocompleteSamePerson(t *testing.T) {
	// Build the doctor overlay with two entries and empty knownArtists.
	items := []doctorItem{
		{
			entry: cache.Entry{
				Identifier: "id-0",
				Source:     "https://youtube.com/watch?v=aaa000",
				Title:      "Song A",
				Artist:     "Old Artist A",
			},
			finding: cachedoctor.Finding{
				Identifier:     "id-0",
				CurrentTitle:   "Song A",
				CurrentArtist:  "Old Artist A",
				ProposedTitle:  "Song A",
				ProposedArtist: "Radiohead",
				Confidence:     "high",
			},
		},
		{
			entry: cache.Entry{
				Identifier: "id-1",
				Source:     "https://youtube.com/watch?v=bbb111",
				Title:      "Song B",
				Artist:     "Old Artist B",
			},
			finding: cachedoctor.Finding{
				Identifier:     "id-1",
				CurrentTitle:   "Song B",
				CurrentArtist:  "Old Artist B",
				ProposedTitle:  "Song B",
				ProposedArtist: "Pink Floyd",
				Confidence:     "high",
			},
		},
	}
	o := newCacheDoctorOverlay(items, []string{}, 80, 40, 0)

	// Simulate saving "Radiohead" on entry 0.
	o.cursor = 0
	o.loadCurrentEntry()
	o.rememberArtist("Radiohead")
	if len(o.knownArtists) != 1 || o.knownArtists[0] != "Radiohead" {
		t.Fatalf("rememberArtist failed to add Radiohead: %v", o.knownArtists)
	}

	// Move to entry 1 and set up for autocomplete.
	o.cursor = 1
	o.loadCurrentEntry()
	o.activeField = 1
	o.editArtist = "rad"
	o.artistCursor = len(o.editArtist)
	o.artistTouched = true

	// Query for "rad" should match "Radiohead" from the pool.
	suggestions := o.artistSuggestions()
	if !contains(suggestions, "Radiohead") {
		t.Errorf("Radiohead not found in autocomplete suggestions for query 'rad': %v", suggestions)
	}
}

// Helper to check if a slice contains a string.
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func TestDoctorFooterRequeryNamesTargetEntry(t *testing.T) {
	tests := []struct {
		name       string
		entry      cache.Entry
		finding    cachedoctor.Finding
		wantSubstr string
	}{
		{
			name: "prefers entry title",
			entry: cache.Entry{
				Identifier: "id-0",
				Source:     "https://youtube.com/watch?v=aaa000",
				Title:      "My Favorite Song",
			},
			finding:    cachedoctor.Finding{Identifier: "id-0"},
			wantSubstr: "My Favorite Song",
		},
		{
			name: "falls back to source URL when title empty",
			entry: cache.Entry{
				Identifier: "id-0",
				Source:     "https://youtube.com/watch?v=aaa000",
			},
			finding:    cachedoctor.Finding{Identifier: "id-0"},
			wantSubstr: "https://youtube.com/watch?v=aaa000",
		},
		{
			name:       "falls back to identifier when title and source empty",
			entry:      cache.Entry{Identifier: "id-0"},
			finding:    cachedoctor.Finding{Identifier: "id-0"},
			wantSubstr: "id-0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items := []doctorItem{{entry: tc.entry, finding: tc.finding}}
			o := newCacheDoctorOverlay(items, nil, 80, 40, 0)
			o.requerying = true
			o.requeryID = "id-0"

			footer := o.doctorFooter()
			if !strings.Contains(footer, tc.wantSubstr) {
				t.Errorf("doctorFooter() = %q, want substring %q", footer, tc.wantSubstr)
			}
			if !strings.Contains(footer, "Waiting for yt-dlp") {
				t.Errorf("doctorFooter() = %q, want to still say waiting for yt-dlp", footer)
			}
		})
	}
}

func TestDoctorFooterRequeryTruncatesLongLabel(t *testing.T) {
	longTitle := strings.Repeat("Very Long Song Title ", 10)
	items := []doctorItem{{
		entry:   cache.Entry{Identifier: "id-0", Title: longTitle},
		finding: cachedoctor.Finding{Identifier: "id-0"},
	}}
	o := newCacheDoctorOverlay(items, nil, 40, 40, 0)
	o.requerying = true
	o.requeryID = "id-0"

	footer := o.doctorFooter()
	if strings.Contains(footer, longTitle) {
		t.Errorf("doctorFooter() did not truncate long title: %q", footer)
	}
	visible := runewidth.StringWidth(stripANSI(footer))
	if visible > o.termWidth {
		t.Errorf("doctorFooter() visible width %d exceeds termWidth %d: %q", visible, o.termWidth, footer)
	}
}

func TestDoctorFooterRequeryUnresolvableTargetFallsBackToPlainMessage(t *testing.T) {
	items := []doctorItem{{
		entry:   cache.Entry{Identifier: "id-0", Title: "Song A"},
		finding: cachedoctor.Finding{Identifier: "id-0"},
	}}
	o := newCacheDoctorOverlay(items, nil, 80, 40, 0)
	o.requerying = true
	o.requeryID = "id-gone" // no longer present in findings

	footer := o.doctorFooter()
	if !strings.Contains(footer, "Waiting for yt-dlp...") {
		t.Errorf("doctorFooter() = %q, want plain waiting message for unresolvable target", footer)
	}
	if strings.Contains(footer, "Song A") {
		t.Errorf("doctorFooter() = %q, should not name an unrelated entry", footer)
	}
}

// TestApplyCurrentDoctorEntrySavesArtistAlias is the discriminating test for
// issue #48: correcting an artist in the cache doctor overlay must persist
// the correction as an alias for future normalization, not just save it on
// the one entry being edited. It calls applyCurrentDoctorEntry() directly —
// the real accept-flow function — rather than reimplementing its
// alias-detection condition inline, so reverting the production wiring
// makes this test fail.
func TestApplyCurrentDoctorEntrySavesArtistAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	projectRoot := t.TempDir()
	pp := paths.ProjectPaths{
		Root:      projectRoot,
		IndexFile: filepath.Join(projectRoot, ".powerhour", "index.json"),
	}

	const identifier = "yt:corrected123"
	idx := &cache.Index{Entries: map[string]cache.Entry{}}
	idx.SetEntry(cache.Entry{
		Identifier: identifier,
		Source:     "https://youtube.com/watch?v=corrected123",
		SourceType: cache.SourceTypeURL,
		Title:      "Some Song",
		Artist:     "Old Artist Name",
		Uploader:   "SomeUploaderChannel",
	})
	if err := cache.Save(pp, idx); err != nil {
		t.Fatalf("seed cache save: %v", err)
	}

	entry, _ := idx.GetByIdentifier(identifier)
	finding := cachedoctor.Finding{
		Identifier:     identifier,
		CurrentTitle:   entry.Title,
		CurrentArtist:  entry.Artist,
		ProposedTitle:  entry.Title,
		ProposedArtist: entry.Artist,
		AliasCandidate: entry.Uploader, // "SomeUploaderChannel"
	}

	overlay := newCacheDoctorOverlay([]doctorItem{{entry: entry, finding: finding}}, nil, 80, 40, 0)
	overlay.editTitle = entry.Title
	overlay.editArtist = "Corrected Artist Name"
	overlay.artistCursor = len(overlay.editArtist)
	overlay.artistTouched = true

	m := Model{pp: pp, doctorOverlay: &overlay}

	m = m.applyCurrentDoctorEntry()

	// The entry itself must be updated (existing behavior).
	updatedIdx, err := cache.Load(pp)
	if err != nil {
		t.Fatalf("reload cache: %v", err)
	}
	updatedEntry, ok := updatedIdx.GetByIdentifier(identifier)
	if !ok {
		t.Fatalf("entry %q missing after apply", identifier)
	}
	if updatedEntry.Artist != "Corrected Artist Name" {
		t.Errorf("entry.Artist = %q, want %q", updatedEntry.Artist, "Corrected Artist Name")
	}

	// The alias must be persisted for future normalization (the fix under test).
	normCfg := cache.LoadNormalizationConfig()
	got, ok := normCfg.ArtistAliases[strings.ToLower(strings.TrimSpace(finding.AliasCandidate))]
	if !ok {
		t.Fatalf("no alias saved for %q; ArtistAliases = %#v", finding.AliasCandidate, normCfg.ArtistAliases)
	}
	if got != "Corrected Artist Name" {
		t.Errorf("saved alias = %q, want %q", got, "Corrected Artist Name")
	}
}
