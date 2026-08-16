package dashboard

import (
	"strings"
	"testing"

	"powerhour/internal/cache"
	"powerhour/internal/cachedoctor"
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
	o := newCacheDoctorOverlay(items, knownArtists, w, h)
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
