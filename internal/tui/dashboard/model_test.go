package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

func TestLooksLikeBatchImportIgnoresTrailingNewlineOnSingleURL(t *testing.T) {
	if looksLikeBatchImport("https://youtu.be/abc123?si=test\n") {
		t.Fatal("single URL with trailing newline should not be treated as batch import")
	}
}

func TestHandleCollectionKeyWithMutationsDuplicateRow(t *testing.T) {
	m := testCollectionModel(t)

	gotModel, _ := m.handleCollectionKeyWithMutations(0, tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("d"),
	})
	got := gotModel.(Model)

	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	if len(got.collectionViews[0].rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(got.collectionViews[0].rows))
	}
	if got.collectionViews[0].cursor != 1 {
		t.Fatalf("cursor = %d, want 1", got.collectionViews[0].cursor)
	}
	if got.collectionViews[0].rows[1].CustomFields["title"] != "First Song" {
		t.Fatalf("duplicated title = %q", got.collectionViews[0].rows[1].CustomFields["title"])
	}

	got.collectionViews[0].rows[1].CustomFields["title"] = "Changed Copy"
	if got.collectionViews[0].rows[0].CustomFields["title"] != "First Song" {
		t.Fatalf("original row title mutated to %q", got.collectionViews[0].rows[0].CustomFields["title"])
	}
}

func TestHandleCollectionKeyWithMutationsDeleteUsesX(t *testing.T) {
	m := testCollectionModel(t)

	gotModel, _ := m.handleCollectionKeyWithMutations(0, tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("x"),
	})
	got := gotModel.(Model)

	if got.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want %v", got.mode, modeConfirmDelete)
	}
	if !strings.Contains(got.deleteDesc, "row 1") {
		t.Fatalf("deleteDesc = %q, want row 1", got.deleteDesc)
	}
	if len(got.collectionViews[0].rows) != 1 {
		t.Fatalf("rows changed before confirmation: %d", len(got.collectionViews[0].rows))
	}
}

func TestHandleTimelineKeyWithMutationsDeleteUsesX(t *testing.T) {
	m := Model{
		activeView: 0,
		timelineView: timelineView{
			sequence: []config.SequenceEntry{{
				Collection: "songs",
				Count:      2,
			}},
		},
	}

	gotModel, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("x"),
	})
	got := gotModel.(Model)

	if got.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want %v", got.mode, modeConfirmDelete)
	}
	if !strings.Contains(got.deleteDesc, "songs") {
		t.Fatalf("deleteDesc = %q, want songs", got.deleteDesc)
	}
}

func testCollectionModel(t *testing.T) Model {
	t.Helper()

	root := t.TempDir()
	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	planPath := filepath.Join(root, "songs.csv")
	if err := os.WriteFile(planPath, []byte("title,artist,link,start_time,duration\n"), 0o644); err != nil {
		t.Fatalf("write plan file: %v", err)
	}

	row := csvplan.CollectionRow{
		Index:           1,
		Link:            "https://example.com/watch?v=1",
		StartRaw:        "0:15",
		DurationSeconds: 60,
		CustomFields: map[string]string{
			"title":      "First Song",
			"artist":     "Artist A",
			"link":       "https://example.com/watch?v=1",
			"start_time": "0:15",
			"duration":   "60",
		},
	}

	coll := project.Collection{
		Name:       "songs",
		Plan:       planPath,
		OutputDir:  "songs",
		Config:     config.CollectionConfig{OutputDir: "songs"},
		Rows:       []csvplan.CollectionRow{row},
		Headers:    []string{"title", "artist", "link", "start_time", "duration"},
		Delimiter:  ',',
		PlanFormat: "csv",
	}

	return Model{
		cfg: config.Config{
			Collections: map[string]config.CollectionConfig{
				"songs": coll.Config,
			},
		},
		pp:              pp,
		collections:     map[string]project.Collection{"songs": coll},
		collectionNames: []string{"songs"},
		activeView:      1,
		collectionViews: []collectionView{{
			name:     "songs",
			planPath: planPath,
			rows:     []csvplan.CollectionRow{row},
			columns:  discoverColumns([]csvplan.CollectionRow{row}, coll.Headers),
		}},
	}
}
