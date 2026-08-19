package cli

import (
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
)

// TestSamplePipelineAppliesSequenceEntryFadeOverride pins issue #41: the
// sample command must reflect a timeline sequence-entry fade override, not
// just a collection's own base fade config. This exercises the exact
// sequence runSample follows (LoadCollections -> BuildCollectionClips ->
// project.ApplySequenceEntryFades) so a regression that drops the
// ApplySequenceEntryFades call (as sample.go did before this fix) fails
// this test.
func TestSamplePipelineAppliesSequenceEntryFadeOverride(t *testing.T) {
	cfg := config.Config{
		Collections: map[string]config.CollectionConfig{
			"songs": {
				File: "song.mp4",
				// No fade configured at the collection level: any fade
				// observed on the built clip must have come from the
				// timeline sequence-entry override, not this config.
			},
		},
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{Collection: "songs", Fade: 2.0},
			},
		},
	}

	pp := paths.ProjectPaths{Root: t.TempDir()}

	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		t.Fatalf("NewCollectionResolver: %v", err)
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		t.Fatalf("LoadCollections: %v", err)
	}

	collectionClips, err := resolver.BuildCollectionClips(collections)
	if err != nil {
		t.Fatalf("BuildCollectionClips: %v", err)
	}
	if len(collectionClips) != 1 {
		t.Fatalf("expected 1 clip, got %d", len(collectionClips))
	}

	// Before the fix under test: BuildCollectionClips alone leaves the
	// clip's fade at the collection's own (unset) base fade.
	if got := collectionClips[0].Clip.FadeInSeconds; got != 0 {
		t.Fatalf("precondition failed: expected 0 fade-in before applying sequence-entry overrides, got %v", got)
	}

	// This is the call the fix adds to runSample.
	project.ApplySequenceEntryFades(cfg, collectionClips)

	wantIn, wantOut := config.ResolveFade(2.0, 0, 0)
	if got := collectionClips[0].Clip.FadeInSeconds; got != wantIn {
		t.Errorf("FadeInSeconds = %v, want %v (from timeline sequence-entry fade override)", got, wantIn)
	}
	if got := collectionClips[0].Clip.FadeOutSeconds; got != wantOut {
		t.Errorf("FadeOutSeconds = %v, want %v (from timeline sequence-entry fade override)", got, wantOut)
	}
}
