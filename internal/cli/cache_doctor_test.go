package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"powerhour/internal/cache"
	"powerhour/internal/cachedoctor"
	"powerhour/internal/config"
	"powerhour/internal/paths"
)

func TestInspectCacheEntryFindsProposedFix(t *testing.T) {
	entry := cache.Entry{
		Identifier: "youtube:abc",
		Title:      "A$AP Rocky - L$D (Official Video)",
		Artist:     "ASAPROCKYUPTOWN",
		Uploader:   "ASAPROCKYUPTOWN",
	}

	finding, ok, err := cachedoctor.InspectEntry(context.Background(), nil, cache.NormalizationConfig{
		ArtistAliases: map[string]string{
			"asaprockyuptown": "A$AP Rocky",
		},
	}, []string{"A$AP Rocky"}, entry, false)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !ok {
		t.Fatal("expected finding")
	}
	if finding.ProposedArtist != "A$AP Rocky" {
		t.Fatalf("artist = %q", finding.ProposedArtist)
	}
	if finding.ProposedTitle != "L$D" {
		t.Fatalf("title = %q", finding.ProposedTitle)
	}
}

func TestApplyAliasAcrossIndexUpdatesMatchingEntries(t *testing.T) {
	idx := &cache.Index{
		Entries: map[string]cache.Entry{
			"youtube:1": {Identifier: "youtube:1", Artist: "ASAPROCKYUPTOWN", Uploader: "ASAPROCKYUPTOWN", Title: "L$D"},
			"youtube:2": {Identifier: "youtube:2", Artist: "Someone Else", Title: "Track"},
		},
	}

	cachedoctor.ApplyAliasAcrossIndex(idx, cache.NormalizationConfig{
		ArtistAliases: map[string]string{
			"asaprockyuptown": "A$AP Rocky",
		},
	}, "ASAPROCKYUPTOWN")

	if got := idx.Entries["youtube:1"].Artist; got != "A$AP Rocky" {
		t.Fatalf("artist = %q", got)
	}
	if got := idx.Entries["youtube:2"].Artist; got != "Someone Else" {
		t.Fatalf("unexpected second artist = %q", got)
	}
}

func TestProjectReferencedIdentifiersReturnsCollectionLoadError(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Collections: map[string]config.CollectionConfig{
			"songs": {
				Plan:      "missing.yaml",
				OutputDir: "songs",
			},
		},
	}
	pp := paths.ProjectPaths{
		Root:        dir,
		SegmentsDir: filepath.Join(dir, "segments"),
	}

	_, err := projectReferencedIdentifiers(pp, cfg, &cache.Index{}, nil)
	if err == nil {
		t.Fatal("expected collection loading error")
	}
	if !strings.Contains(err.Error(), "missing.yaml") {
		t.Fatalf("error %q does not mention missing plan", err)
	}
}
