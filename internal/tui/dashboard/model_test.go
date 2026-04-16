package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestInlineEditReloadsParsedStartTimeFromDisk(t *testing.T) {
	m := testCollectionModel(t)
	m.mode = modeInlineEdit
	m.editFieldIdx = 2 // start_time
	m.editValue = "1:00"
	m.editOriginal = "0:15"
	m.collectionViews[0].editing = true
	m.collectionViews[0].editFieldIdx = 2
	m.collectionViews[0].editValue = "1:00"

	gotModel, _ := m.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := gotModel.(Model)

	row := got.collectionViews[0].rows[0]
	if row.StartRaw != "1:00" {
		t.Fatalf("StartRaw = %q, want 1:00", row.StartRaw)
	}
	if row.Start != time.Minute {
		t.Fatalf("Start = %v, want %v", row.Start, time.Minute)
	}
	if row.CustomFields["start_time"] != "1:00" {
		t.Fatalf("custom start_time = %q, want 1:00", row.CustomFields["start_time"])
	}
	if got.collectionViews[0].rowStatus[row.Index] != "note:saved" {
		t.Fatalf("row status = %q, want note:saved", got.collectionViews[0].rowStatus[row.Index])
	}
}

func TestInlineEditLeftRightMoveCaretNotField(t *testing.T) {
	m := testCollectionModel(t)
	m.mode = modeInlineEdit
	m.editFieldIdx = 0
	m.editValue = "First Song"
	m.editOriginal = "First Song"
	m.editCursor = len("First")
	m.collectionViews[0].editing = true
	m.collectionViews[0].editFieldIdx = 0
	m.collectionViews[0].editValue = "First Song"
	m.collectionViews[0].editCursor = len("First")

	gotModel, _ := m.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyRight})
	got := gotModel.(Model)

	if got.editFieldIdx != 0 {
		t.Fatalf("editFieldIdx = %d, want 0", got.editFieldIdx)
	}
	if got.editCursor != len("First ") {
		t.Fatalf("editCursor = %d, want %d", got.editCursor, len("First "))
	}

	gotModel, _ = got.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyLeft})
	got = gotModel.(Model)

	if got.editFieldIdx != 0 {
		t.Fatalf("editFieldIdx after left = %d, want 0", got.editFieldIdx)
	}
	if got.editCursor != len("First") {
		t.Fatalf("editCursor after left = %d, want %d", got.editCursor, len("First"))
	}
}

func TestInlineEditTabAndShiftTabSwitchFields(t *testing.T) {
	m := testCollectionModel(t)
	m.mode = modeInlineEdit
	m.editFieldIdx = 0
	m.editValue = "First Song"
	m.editOriginal = "First Song"
	m.editCursor = len(m.editValue)
	m.collectionViews[0].editing = true
	m.collectionViews[0].editFieldIdx = 0
	m.collectionViews[0].editValue = "First Song"
	m.collectionViews[0].editCursor = len("First Song")

	gotModel, _ := m.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyTab})
	got := gotModel.(Model)

	if got.editFieldIdx != 1 {
		t.Fatalf("editFieldIdx after tab = %d, want 1", got.editFieldIdx)
	}
	if got.editValue != "Artist A" {
		t.Fatalf("editValue after tab = %q, want Artist A", got.editValue)
	}

	gotModel, _ = got.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	got = gotModel.(Model)

	if got.editFieldIdx != 0 {
		t.Fatalf("editFieldIdx after shift-tab = %d, want 0", got.editFieldIdx)
	}
	if got.editValue != "First Song" {
		t.Fatalf("editValue after shift-tab = %q, want First Song", got.editValue)
	}
}

func TestInlineEditInsertAndBackspaceAtCaret(t *testing.T) {
	m := testCollectionModel(t)
	m.mode = modeInlineEdit
	m.editFieldIdx = 0
	m.editValue = "FirstSong"
	m.editOriginal = "FirstSong"
	m.editCursor = len("First")
	m.collectionViews[0].editing = true
	m.collectionViews[0].editFieldIdx = 0
	m.collectionViews[0].editValue = "FirstSong"
	m.collectionViews[0].editCursor = len("First")

	gotModel, _ := m.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	got := gotModel.(Model)

	if got.editValue != "First Song" {
		t.Fatalf("editValue after insert = %q, want First Song", got.editValue)
	}
	if got.editCursor != len("First ") {
		t.Fatalf("editCursor after insert = %d, want %d", got.editCursor, len("First "))
	}

	gotModel, _ = got.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyBackspace})
	got = gotModel.(Model)

	if got.editValue != "FirstSong" {
		t.Fatalf("editValue after backspace = %q, want FirstSong", got.editValue)
	}
	if got.editCursor != len("First") {
		t.Fatalf("editCursor after backspace = %d, want %d", got.editCursor, len("First"))
	}
}

func TestProcessDuplicateRowUsesInlineNote(t *testing.T) {
	m := testCollectionModel(t)

	got := m.processDuplicateRow(0)

	if got.collectionViews[0].cursor != 1 {
		t.Fatalf("cursor = %d, want 1", got.collectionViews[0].cursor)
	}
	if got.collectionViews[0].rowStatus[2] != "note:duplicated row 1" {
		t.Fatalf("row status = %q, want duplicated note", got.collectionViews[0].rowStatus[2])
	}
}

func TestProcessDeleteRowUsesInlineNoteOnRemainingRow(t *testing.T) {
	m := testTwoRowCollectionModel(t)

	gotModel, _ := m.processDeleteRow()
	got := gotModel.(Model)

	if len(got.collectionViews[0].rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(got.collectionViews[0].rows))
	}
	if got.collectionViews[0].rowStatus[1] != "note:removed row" {
		t.Fatalf("row status = %q, want removed note", got.collectionViews[0].rowStatus[1])
	}
}

func TestProcessAddTimelineEntryUsesInlineNote(t *testing.T) {
	m := testTimelineModel(t)

	gotModel, _ := m.processAddTimelineEntry("songs")
	got := gotModel.(Model)

	if len(got.timelineView.sequence) != 2 {
		t.Fatalf("sequence len = %d, want 2", len(got.timelineView.sequence))
	}
	if got.timelineView.seqStatus[1] != "note:added" {
		t.Fatalf("seq status = %q, want note:added", got.timelineView.seqStatus[1])
	}
}

func TestProcessDeleteTimelineEntryUsesInlineNote(t *testing.T) {
	m := testTimelineModel(t)
	m.timelineView.sequence = append(m.timelineView.sequence, config.SequenceEntry{Collection: "songs", Count: 2})
	m.cfg.Timeline.Sequence = append([]config.SequenceEntry(nil), m.timelineView.sequence...)

	gotModel, _ := m.processDeleteTimelineEntry()
	got := gotModel.(Model)

	if len(got.timelineView.sequence) != 1 {
		t.Fatalf("sequence len = %d, want 1", len(got.timelineView.sequence))
	}
	if got.timelineView.seqStatus[0] != "note:removed songs" {
		t.Fatalf("seq status = %q, want removed songs note", got.timelineView.seqStatus[0])
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
		Start:           15 * time.Second,
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

func testTwoRowCollectionModel(t *testing.T) Model {
	t.Helper()

	m := testCollectionModel(t)
	row := m.collectionViews[0].rows[0]
	row.Index = 2
	row.Link = "https://example.com/watch?v=2"
	row.CustomFields = map[string]string{
		"title":      "Second Song",
		"artist":     "Artist B",
		"link":       "https://example.com/watch?v=2",
		"start_time": "0:30",
		"duration":   "60",
	}
	row.StartRaw = "0:30"
	row.Start = 30 * time.Second

	m.collectionViews[0].rows = append(m.collectionViews[0].rows, row)
	m.collections["songs"] = project.Collection{
		Name:       m.collections["songs"].Name,
		Plan:       m.collections["songs"].Plan,
		OutputDir:  m.collections["songs"].OutputDir,
		Config:     m.collections["songs"].Config,
		Rows:       append([]csvplan.CollectionRow(nil), m.collectionViews[0].rows...),
		Headers:    m.collections["songs"].Headers,
		Delimiter:  m.collections["songs"].Delimiter,
		PlanFormat: m.collections["songs"].PlanFormat,
	}
	return m
}

func testTimelineModel(t *testing.T) Model {
	t.Helper()

	root := t.TempDir()
	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{{Collection: "songs"}},
		},
	}

	return Model{
		cfg: cfg,
		pp:  pp,
		timelineView: timelineView{
			sequence:       append([]config.SequenceEntry(nil), cfg.Timeline.Sequence...),
			seqStatus:      make(map[int]string),
			seqStatusUntil: make(map[int]int),
		},
		collectionNames: []string{"songs"},
		collections: map[string]project.Collection{
			"songs": {Name: "songs"},
		},
	}
}
