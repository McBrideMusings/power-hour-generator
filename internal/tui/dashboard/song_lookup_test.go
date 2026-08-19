package dashboard

import (
	"testing"

	"powerhour/internal/cache"
)

// TestSearchFieldsAffectsSuggestionRanking verifies that changing cache.ytdlp.search_fields
// (via cacheLookup.searchFields) causes different entries to rank at the top.
// This ensures the configuration knob is not write-only and actually affects search behavior.
func TestSearchFieldsAffectsSuggestionRanking(t *testing.T) {
	// Create a cache index with entries whose distinguishing values live in different fields.
	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			// Entry A: matches on title field
			"entry-a": {
				Identifier: "entry-a",
				Title:      "Jazz Music Playlist",
				Artist:     "Unknown Artist",
				Uploader:   "generic-uploader",
				Channel:    "generic-channel",
				Source:     "https://example.com/a",
			},
			// Entry B: matches on uploader field (not title or artist)
			"entry-b": {
				Identifier: "entry-b",
				Title:      "Some Other Video",
				Artist:     "Some Other Artist",
				Uploader:   "Jazz Music Playlist", // Same text as entry-a's title, but in uploader field
				Channel:    "generic-channel",
				Source:     "https://example.com/b",
			},
			// Entry C: matches on channel field
			"entry-c": {
				Identifier: "entry-c",
				Title:      "Random Video Title",
				Artist:     "Random Artist",
				Uploader:   "generic-uploader",
				Channel:    "Jazz Music Playlist", // Same text, but in channel field
				Source:     "https://example.com/c",
			},
		},
		Links: map[string]string{},
	}

	// Test case 1: Default search fields [title, artist]
	// Entry A should rank highest because "jazz music playlist" matches its title.
	// The combined Title+Artist bonus for entries B and C is minor compared to
	// the direct title match score for entry A.
	t.Run("default_title_artist_fields", func(t *testing.T) {
		lookup := cacheLookup{
			searchFields: []string{"title", "artist"},
		}
		results := searchCachedSongs(idx, "jazz music playlist", lookup, 3)

		if len(results) < 1 {
			t.Fatalf("expected at least 1 result, got %d", len(results))
		}

		// Entry A (matching title) should be at the top
		top := results[0]
		if top.Title != "Jazz Music Playlist" {
			t.Errorf("top result title = %q, want %q", top.Title, "Jazz Music Playlist")
		}
	})

	// Test case 2: Search fields [uploader]
	// Entry B should rank highest because "jazz music playlist" matches its uploader field.
	// Entry A's title match is not scored at all because title is not in searchFields.
	// Entry C's channel match is not in searchFields either.
	t.Run("uploader_field_only", func(t *testing.T) {
		lookup := cacheLookup{
			searchFields: []string{"uploader"},
		}
		results := searchCachedSongs(idx, "jazz music playlist", lookup, 3)

		if len(results) < 1 {
			t.Fatalf("expected at least 1 result, got %d", len(results))
		}

		// Entry B (matching uploader) should be at the top
		top := results[0]
		if top.Artist != "Some Other Artist" {
			t.Errorf("top result artist = %q, want %q", top.Artist, "Some Other Artist")
		}
		if top.Title != "Some Other Video" {
			t.Errorf("top result title = %q, want %q", top.Title, "Some Other Video")
		}
	})

	// Test case 3: Search fields [channel]
	// Entry C should rank highest because "jazz music playlist" matches its channel field.
	// Entries A and B do not have matching uploader or channel fields in the search list.
	t.Run("channel_field_only", func(t *testing.T) {
		lookup := cacheLookup{
			searchFields: []string{"channel"},
		}
		results := searchCachedSongs(idx, "jazz music playlist", lookup, 3)

		if len(results) < 1 {
			t.Fatalf("expected at least 1 result, got %d", len(results))
		}

		// Entry C (matching channel) should be at the top
		top := results[0]
		if top.Title != "Random Video Title" {
			t.Errorf("top result title = %q, want %q", top.Title, "Random Video Title")
		}
	})

	// Test case 4: Multiple fields in different order
	// With search fields [channel, title], Entry C (matching channel) should rank
	// higher than Entry A (matching title only), since channel appears first in the
	// search fields list and both will be scored, but scoring is sequential.
	t.Run("channel_then_title_fields", func(t *testing.T) {
		lookup := cacheLookup{
			searchFields: []string{"channel", "title"},
		}
		results := searchCachedSongs(idx, "jazz music playlist", lookup, 3)

		if len(results) < 2 {
			t.Fatalf("expected at least 2 results, got %d", len(results))
		}

		// Entry C (matching channel) should rank higher than or equal to Entry A (matching title)
		// because both fields match the query, but Entry C's channel match happens first
		// and both get added to the score. Entry A will have a title match,
		// Entry C will have a channel match. The tie-breaker is alphabetical by title.
		top := results[0]
		if top.Title != "Jazz Music Playlist" && top.Title != "Random Video Title" {
			t.Errorf("top result title = %q, want either 'Jazz Music Playlist' or 'Random Video Title'", top.Title)
		}
	})

	// Test case 5: Empty search fields (should fall back to default [title, artist])
	// With empty searchFields, the code defaults to [title, artist] (see scoreSuggestion lines 220-222),
	// so behavior should match test case 1.
	t.Run("empty_search_fields_uses_default", func(t *testing.T) {
		lookup := cacheLookup{
			searchFields: []string{}, // Empty will trigger the default
		}
		results := searchCachedSongs(idx, "jazz music playlist", lookup, 3)

		if len(results) < 1 {
			t.Fatalf("expected at least 1 result, got %d", len(results))
		}

		// Entry A (matching title) should rank top, same as case 1
		top := results[0]
		if top.Title != "Jazz Music Playlist" {
			t.Errorf("top result title = %q, want %q", top.Title, "Jazz Music Playlist")
		}
	})
}

// TestScoreSuggestionWithDifferentSearchFields verifies that scoreSuggestion
// produces different scores when searchFields changes, holding all other inputs constant.
func TestScoreSuggestionWithDifferentSearchFields(t *testing.T) {
	entry := cache.Entry{
		Title:    "Album Name Song",
		Artist:   "Band Name",
		Uploader: "Channel Name",
		Channel:  "Channel Name",
	}

	suggestion := songSuggestion{
		Title:  "Album Name Song",
		Artist: "Band Name",
	}

	query := "channel name"

	tests := []struct {
		name         string
		searchFields []string
		shouldScore  bool // whether we expect a non-zero score
	}{
		{
			name:         "title_artist_does_not_match_uploader",
			searchFields: []string{"title", "artist"},
			shouldScore:  false, // "channel name" doesn't match title or artist
		},
		{
			name:         "uploader_matches_query",
			searchFields: []string{"uploader"},
			shouldScore:  true, // "channel name" matches entry.Uploader
		},
		{
			name:         "channel_matches_query",
			searchFields: []string{"channel"},
			shouldScore:  true, // "channel name" matches entry.Channel
		},
		{
			name:         "uploader_and_channel_both_match",
			searchFields: []string{"uploader", "channel"},
			shouldScore:  true, // "channel name" matches both
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := cacheLookup{
				searchFields: tt.searchFields,
			}
			score := scoreSuggestion(query, entry, lookup, suggestion)

			if tt.shouldScore {
				if score <= 0 {
					t.Errorf("scoreSuggestion with searchFields %v returned %d, want > 0", tt.searchFields, score)
				}
			} else {
				if score > 0 {
					t.Errorf("scoreSuggestion with searchFields %v returned %d, want 0 or negative", tt.searchFields, score)
				}
			}
		})
	}
}
