package project

import (
	"os"
	"path/filepath"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

func makeProjectPaths(t *testing.T) paths.ProjectPaths {
	t.Helper()
	root := t.TempDir()
	return paths.ProjectPaths{
		Root:        root,
		SegmentsDir: filepath.Join(root, "segments"),
	}
}

func TestNewCollectionResolver(t *testing.T) {
	pp := makeProjectPaths(t)

	t.Run("no collections", func(t *testing.T) {
		cfg := config.Config{}
		r, err := NewCollectionResolver(cfg, pp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r == nil {
			t.Fatal("expected non-nil resolver")
		}
	})

	t.Run("protected header rejected", func(t *testing.T) {
		cfg := config.Config{
			Collections: map[string]config.CollectionConfig{
				"bad": {Plan: "plan.csv", LinkHeader: "index"},
			},
		}
		_, err := NewCollectionResolver(cfg, pp)
		if err == nil {
			t.Fatal("expected error for protected header")
		}
	})
}

func writeCSV(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCollections(t *testing.T) {
	pp := makeProjectPaths(t)

	t.Run("no collections returns nil", func(t *testing.T) {
		cfg := config.Config{}
		r, _ := NewCollectionResolver(cfg, pp)
		colls, err := r.LoadCollections()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if colls != nil {
			t.Errorf("expected nil, got %v", colls)
		}
	})

	t.Run("empty plan path rejected by validation", func(t *testing.T) {
		cfg := config.Config{
			Collections: map[string]config.CollectionConfig{
				"songs": {Plan: "  "},
			},
		}
		_, err := NewCollectionResolver(cfg, pp)
		if err == nil {
			t.Fatal("expected error for empty plan path")
		}
	})

	t.Run("loads valid collection", func(t *testing.T) {
		csvContent := "link,title,artist,start_time\nhttps://example.com/1,Song One,Artist A,0:30\nhttps://example.com/2,Song Two,Artist B,1:00\n"
		writeCSV(t, pp.Root, "valid.csv", csvContent)

		cfg := config.Config{
			Collections: map[string]config.CollectionConfig{
				"songs": {Plan: "valid.csv"},
			},
		}
		r, _ := NewCollectionResolver(cfg, pp)
		colls, err := r.LoadCollections()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(colls) != 1 {
			t.Fatalf("len(colls) = %d, want 1", len(colls))
		}

		songs := colls["songs"]
		if songs.Name != "songs" {
			t.Errorf("Name = %q, want %q", songs.Name, "songs")
		}
		if len(songs.Rows) != 2 {
			t.Errorf("len(Rows) = %d, want 2", len(songs.Rows))
		}
		if songs.Rows[0].Link != "https://example.com/1" {
			t.Errorf("Row[0].Link = %q", songs.Rows[0].Link)
		}
	})

	t.Run("empty plan file returns collection with no rows", func(t *testing.T) {
		// A CSV with headers but no data rows should not be an error
		csvContent := "link,title,artist,start_time\n"
		writeCSV(t, pp.Root, "empty.csv", csvContent)

		cfg := config.Config{
			Collections: map[string]config.CollectionConfig{
				"interstitials": {Plan: "empty.csv"},
			},
		}
		r, _ := NewCollectionResolver(cfg, pp)
		colls, err := r.LoadCollections()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(colls) != 1 {
			t.Fatalf("len(colls) = %d, want 1", len(colls))
		}
		coll := colls["interstitials"]
		if len(coll.Rows) != 0 {
			t.Errorf("len(Rows) = %d, want 0", len(coll.Rows))
		}
	})

	t.Run("loads with overlays", func(t *testing.T) {
		csvContent := "link,start_time\nhttps://example.com/1,0:30\n"
		writeCSV(t, pp.Root, "overlaid.csv", csvContent)

		cfg := config.Config{
			Collections: map[string]config.CollectionConfig{
				"songs": {
					Plan:     "overlaid.csv",
					Overlays: []config.OverlayEntry{{Type: "song-info"}},
				},
			},
		}
		r, _ := NewCollectionResolver(cfg, pp)
		colls, err := r.LoadCollections()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		songs := colls["songs"]
		if len(songs.Config.Overlays) != 1 {
			t.Errorf("expected 1 overlay, got %d", len(songs.Config.Overlays))
		}
	})
}

func TestFlattenCollections(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		got := FlattenCollections(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		got := FlattenCollections(map[string]Collection{})
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("flattens multiple collections", func(t *testing.T) {
		colls := map[string]Collection{
			"songs": {
				Name: "songs",
				Rows: []csvplan.CollectionRow{
					{Index: 1, Link: "https://a.com"},
					{Index: 2, Link: "https://b.com"},
				},
			},
			"intros": {
				Name: "intros",
				Rows: []csvplan.CollectionRow{
					{Index: 1, Link: "https://c.com"},
				},
			},
		}

		flat := FlattenCollections(colls)
		if len(flat) != 3 {
			t.Fatalf("len = %d, want 3", len(flat))
		}

		// Check that collection names are preserved
		nameCount := map[string]int{}
		for _, row := range flat {
			nameCount[row.CollectionName]++
		}
		if nameCount["songs"] != 2 {
			t.Errorf("songs count = %d, want 2", nameCount["songs"])
		}
		if nameCount["intros"] != 1 {
			t.Errorf("intros count = %d, want 1", nameCount["intros"])
		}
	})
}

func TestBuildCollectionClips(t *testing.T) {
	pp := makeProjectPaths(t)

	t.Run("nil collections", func(t *testing.T) {
		cfg := config.Config{}
		r, _ := NewCollectionResolver(cfg, pp)
		clips, err := r.BuildCollectionClips(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if clips != nil {
			t.Errorf("expected nil, got %v", clips)
		}
	})

	t.Run("builds clips with overlays", func(t *testing.T) {
		cfg := config.Config{
			Collections: map[string]config.CollectionConfig{
				"songs": {
					Plan:     "songs.csv",
					Overlays: []config.OverlayEntry{{Type: "song-info"}},
				},
			},
		}
		r, _ := NewCollectionResolver(cfg, pp)

		colls := map[string]Collection{
			"songs": {
				Name:      "songs",
				OutputDir: "/out/songs",
				Config:    cfg.Collections["songs"],
				Rows: []csvplan.CollectionRow{
					{Index: 1, Link: "https://a.com", DurationSeconds: 60, CustomFields: map[string]string{"title": "A"}},
					{Index: 2, Link: "https://b.com", DurationSeconds: 45, CustomFields: map[string]string{"title": "B"}},
				},
			},
		}

		clips, err := r.BuildCollectionClips(colls)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(clips) != 2 {
			t.Fatalf("len = %d, want 2", len(clips))
		}

		c := clips[0]
		if c.CollectionName != "songs" {
			t.Errorf("CollectionName = %q", c.CollectionName)
		}
		if c.OutputDir != "/out/songs" {
			t.Errorf("OutputDir = %q", c.OutputDir)
		}
		if c.Clip.SourceKind != SourceKindPlan {
			t.Errorf("SourceKind = %q, want %q", c.Clip.SourceKind, SourceKindPlan)
		}
		if c.Clip.ClipType != "songs" {
			t.Errorf("ClipType = %q, want %q", c.Clip.ClipType, "songs")
		}
		if len(c.Overlays) != 1 || c.Overlays[0].Type != "song-info" {
			t.Errorf("Overlays = %v, want [{Type: song-info}]", c.Overlays)
		}
	})

	t.Run("sequence numbers are sequential", func(t *testing.T) {
		cfg := config.Config{}
		r, _ := NewCollectionResolver(cfg, pp)

		colls := map[string]Collection{
			"a": {
				Name: "a",
				Rows: []csvplan.CollectionRow{
					{Index: 1, Link: "https://1.com", CustomFields: map[string]string{}},
					{Index: 2, Link: "https://2.com", CustomFields: map[string]string{}},
				},
			},
		}

		clips, err := r.BuildCollectionClips(colls)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for i, c := range clips {
			if c.Clip.Sequence != i+1 {
				t.Errorf("clip[%d].Sequence = %d, want %d", i, c.Clip.Sequence, i+1)
			}
		}
	})
}
