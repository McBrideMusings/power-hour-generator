package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/cache"
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
	if got.collectionViews[0].confirmDelete == "" {
		t.Fatal("confirmDelete empty, want inline prompt set on the active collection view")
	}
	if !strings.Contains(got.collectionViews[0].confirmDelete, "[y/n]") {
		t.Fatalf("confirmDelete = %q, want [y/n] prompt", got.collectionViews[0].confirmDelete)
	}
	if len(got.collectionViews[0].rows) != 1 {
		t.Fatalf("rows changed before confirmation: %d", len(got.collectionViews[0].rows))
	}
}

func TestHandleConfirmDeleteKeyClearsInlinePrompt(t *testing.T) {
	m := testCollectionModel(t)
	m.collectionViews[0].confirmDelete = "Delete row 1? [y/n]"
	m.cacheView.confirmDelete = "Delete cache entry? [y/n]"
	m.timelineView.confirmDelete = "Delete sequence? [y/n]"
	m.mode = modeConfirmDelete
	m.deleteDesc = `row 1 "First Song"`

	gotModel, _ := m.handleConfirmDeleteKey(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("n"),
	})
	got := gotModel.(Model)

	if got.collectionViews[0].confirmDelete != "" {
		t.Fatalf("collection confirmDelete = %q, want empty after cancel", got.collectionViews[0].confirmDelete)
	}
	if got.cacheView.confirmDelete != "" {
		t.Fatalf("cache confirmDelete = %q, want empty after cancel", got.cacheView.confirmDelete)
	}
	if got.timelineView.confirmDelete != "" {
		t.Fatalf("timeline confirmDelete = %q, want empty after cancel", got.timelineView.confirmDelete)
	}
}

func TestHandleTimelineKeyWithMutationsDeleteUsesX(t *testing.T) {
	m := Model{
		activeView: 0,
		timelineView: timelineView{
			sequence: []config.SequenceEntry{{
				Collection: "songs",
				Slice:      "start:2",
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
	if got.timelineView.confirmDelete == "" {
		t.Fatal("confirmDelete empty, want inline prompt set on the timeline view")
	}
}

func TestHandleTimelineKeyWithMutationsEditOpensProjectConfig(t *testing.T) {
	m := testTimelineModel(t)

	var opened string
	prev := openExternalPath
	openExternalPath = func(path string) error {
		opened = path
		return nil
	}
	defer func() {
		openExternalPath = prev
	}()

	gotModel, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("e"),
	})
	got := gotModel.(Model)

	if opened != m.pp.ConfigFile {
		t.Fatalf("opened path = %q, want %q", opened, m.pp.ConfigFile)
	}
	if !strings.Contains(got.statusMsg, filepath.Base(m.pp.ConfigFile)) {
		t.Fatalf("statusMsg = %q, want config filename", got.statusMsg)
	}
	if !strings.Contains(got.statusMsg, "timeline.sequence") {
		t.Fatalf("statusMsg = %q, want timeline.sequence hint", got.statusMsg)
	}
}

func TestHandleTimelineKeyWithMutationsEditExternalOpensProjectConfig(t *testing.T) {
	m := testTimelineModel(t)

	var opened string
	prev := openExternalPath
	openExternalPath = func(path string) error {
		opened = path
		return nil
	}
	defer func() {
		openExternalPath = prev
	}()

	gotModel, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("E"),
	})
	got := gotModel.(Model)

	if opened != m.pp.ConfigFile {
		t.Fatalf("opened path = %q, want %q", opened, m.pp.ConfigFile)
	}
	if !strings.Contains(got.statusMsg, "press u to refresh") {
		t.Fatalf("statusMsg = %q, want refresh hint", got.statusMsg)
	}
}

func TestHandleTimelineKeyWithMutationsEditOpensOutputWhenSelected(t *testing.T) {
	m := testTimelineModel(t)
	m.timelineView.concatFocus = true
	m.timelineView.concatExists = true
	m.timelineView.concatPath = filepath.Join(m.pp.Root, "powerhour.mp4")

	var opened string
	prev := openExternalPath
	openExternalPath = func(path string) error {
		opened = path
		return nil
	}
	defer func() {
		openExternalPath = prev
	}()

	gotModel, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("e"),
	})
	got := gotModel.(Model)

	if opened != m.timelineView.concatPath {
		t.Fatalf("opened path = %q, want %q", opened, m.timelineView.concatPath)
	}
	if !strings.Contains(got.statusMsg, "Opened powerhour.mp4") {
		t.Fatalf("statusMsg = %q, want output open message", got.statusMsg)
	}
}

func TestHandleTimelineKeyWithMutationsDeleteOutputUsesX(t *testing.T) {
	m := testTimelineModel(t)
	m.timelineView.concatFocus = true
	m.timelineView.concatExists = true
	m.timelineView.concatPath = filepath.Join(m.pp.Root, "powerhour.mp4")

	gotModel, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("x"),
	})
	got := gotModel.(Model)

	if got.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want %v", got.mode, modeConfirmDelete)
	}
	if !strings.Contains(got.deleteDesc, "output") {
		t.Fatalf("deleteDesc = %q, want output", got.deleteDesc)
	}
	if got.timelineView.confirmDelete == "" {
		t.Fatal("confirmDelete empty, want inline prompt set for output delete")
	}
}

func TestHandleTimelineKeyWithMutationsDownMovesFromSequenceToPlaybackOrder(t *testing.T) {
	m := testTimelineModel(t)
	m.timelineView.sequence = []config.SequenceEntry{{Collection: "songs"}}
	m.timelineView.seqCursor = 0
	m.timelineView.focusPanel = 0
	m.timelineView.resolved = []project.TimelineEntry{
		{Collection: "songs", Index: 1, Sequence: 1},
		{Collection: "songs", Index: 2, Sequence: 2},
	}

	gotModel, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{
		Type: tea.KeyDown,
	})
	got := gotModel.(Model)

	if got.timelineView.focusPanel != 1 {
		t.Fatalf("focusPanel = %d, want 1", got.timelineView.focusPanel)
	}
	if got.timelineView.resCursor != 0 {
		t.Fatalf("resCursor = %d, want 0", got.timelineView.resCursor)
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

func TestProcessAddRowUsesCachedLinkMetadata(t *testing.T) {
	m := testCollectionModel(t)
	m.cacheIdx = &cache.Index{
		Entries: map[string]cache.Entry{
			"youtube:abc123": {
				Identifier: "youtube:abc123",
				Source:     "https://youtube.com/watch?v=abc123",
				Title:      "Cache Song",
				Artist:     "Cache Artist",
			},
		},
		Links: map[string]string{
			"https://youtube.com/watch?v=abc123": "youtube:abc123",
		},
	}

	gotModel, cmd := m.processAddRow("https://youtube.com/watch?v=abc123&list=foo")
	got := gotModel.(Model)

	if cmd != nil {
		t.Fatal("expected no probe command for cached link")
	}
	if len(got.collectionViews[0].rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(got.collectionViews[0].rows))
	}
	row := got.collectionViews[0].rows[1]
	if row.CustomFields["title"] != "Cache Song" {
		t.Fatalf("title = %q, want Cache Song", row.CustomFields["title"])
	}
	if row.CustomFields["artist"] != "Cache Artist" {
		t.Fatalf("artist = %q, want Cache Artist", row.CustomFields["artist"])
	}
	if got.collectionViews[0].rowStatus[row.Index] != "note:recognized cached link https://youtube.com/watch?v=abc123" {
		t.Fatalf("row status = %q", got.collectionViews[0].rowStatus[row.Index])
	}
}

func TestInlineEditTabDoesNotApplyFuzzyCacheMatch(t *testing.T) {
	m := testCollectionModel(t)
	m.cacheIdx = &cache.Index{
		Entries: map[string]cache.Entry{
			"youtube:match1": {
				Identifier: "youtube:match1",
				Source:     "https://example.com/watch?v=match1",
				Title:      "Midnight City",
				Artist:     "M83",
			},
		},
		Links: map[string]string{
			"https://example.com/watch?v=match1": "youtube:match1",
		},
	}
	m.mode = modeInlineEdit
	m.editFieldIdx = 0
	m.editValue = "midnight"
	m.editOriginal = ""
	m.editCursor = len(m.editValue)
	m.collectionViews[0].editing = true
	m.collectionViews[0].editFieldIdx = 0
	m.collectionViews[0].editValue = m.editValue
	m.collectionViews[0].editCursor = m.editCursor
	m.collectionViews[0].rows[0].CustomFields["title"] = ""
	m.collectionViews[0].rows[0].CustomFields["artist"] = ""
	m.collectionViews[0].rows[0].CustomFields["link"] = ""

	gotModel, _ := m.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyTab})
	got := gotModel.(Model)

	if got.editFieldIdx != 1 {
		t.Fatalf("editFieldIdx = %d, want 1", got.editFieldIdx)
	}
	if got.editValue != "" {
		t.Fatalf("editValue = %q, want empty artist field", got.editValue)
	}
}

func TestAddClipTabAddsBestCachedMatch(t *testing.T) {
	m := testCollectionModel(t)
	m.collectionViews[0].rows = nil
	m.collections["songs"] = project.Collection{
		Name:       m.collections["songs"].Name,
		Plan:       m.collections["songs"].Plan,
		OutputDir:  m.collections["songs"].OutputDir,
		Config:     m.collections["songs"].Config,
		Rows:       nil,
		Headers:    m.collections["songs"].Headers,
		Delimiter:  m.collections["songs"].Delimiter,
		PlanFormat: m.collections["songs"].PlanFormat,
	}
	m.cacheIdx = &cache.Index{
		Entries: map[string]cache.Entry{
			"youtube:ninara": {
				Identifier: "youtube:ninara",
				Source:     "https://example.com/watch?v=ninara",
				Title:      "Ninara",
				Artist:     "Kora",
			},
		},
		Links: map[string]string{
			"https://example.com/watch?v=ninara": "youtube:ninara",
		},
	}
	m.cfg.Cache = config.Default().Cache
	collCfg := m.cfg.Collections["songs"]
	collCfg.CacheSearchProfile = "song_lookup"
	m.cfg.Collections["songs"] = collCfg
	coll := m.collections["songs"]
	coll.Config.CacheSearchProfile = "song_lookup"
	m.collections["songs"] = coll
	m.mode = modeAddClip
	m.addCvIdx = 0
	m.addBuffer = "Ninara"
	m.collectionViews[0].addFocus = true
	m.collectionViews[0].addBuffer = "Ninara"
	m = m.refreshAddClipHint(0)

	gotModel, cmd := m.handleAddClipKey(tea.KeyMsg{Type: tea.KeyTab})
	got := gotModel.(Model)

	if cmd != nil {
		t.Fatal("expected no async command when adding fuzzy cache match")
	}
	if len(got.collectionViews[0].rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(got.collectionViews[0].rows))
	}
	row := got.collectionViews[0].rows[0]
	if row.CustomFields["title"] != "Ninara" {
		t.Fatalf("title = %q, want Ninara", row.CustomFields["title"])
	}
	if row.CustomFields["artist"] != "Kora" {
		t.Fatalf("artist = %q, want Kora", row.CustomFields["artist"])
	}
	if row.CustomFields["link"] != "https://example.com/watch?v=ninara" {
		t.Fatalf("link = %q, want cached link", row.CustomFields["link"])
	}
	if got.collectionViews[0].addBuffer != "" {
		t.Fatalf("addBuffer = %q, want empty", got.collectionViews[0].addBuffer)
	}
}

func TestAddClipArrowKeysSelectSuggestionForTab(t *testing.T) {
	m := testCollectionModel(t)
	m.collectionViews[0].rows = nil
	m.collections["songs"] = project.Collection{
		Name:       m.collections["songs"].Name,
		Plan:       m.collections["songs"].Plan,
		OutputDir:  m.collections["songs"].OutputDir,
		Config:     m.collections["songs"].Config,
		Rows:       nil,
		Headers:    m.collections["songs"].Headers,
		Delimiter:  m.collections["songs"].Delimiter,
		PlanFormat: m.collections["songs"].PlanFormat,
	}
	m.cacheIdx = &cache.Index{
		Entries: map[string]cache.Entry{
			"one": {Identifier: "one", Source: "https://example.com/1", Title: "Ninara", Artist: "Kora"},
			"two": {Identifier: "two", Source: "https://example.com/2", Title: "Nine Ball", Artist: "Zach Bryan"},
		},
		Links: map[string]string{
			"https://example.com/1": "one",
			"https://example.com/2": "two",
		},
	}
	m.mode = modeAddClip
	m.addCvIdx = 0
	m.addBuffer = "ni"
	m.collectionViews[0].addFocus = true
	m.collectionViews[0].addBuffer = "ni"
	m = m.refreshAddClipHint(0)

	downModel, _ := m.handleAddClipKey(tea.KeyMsg{Type: tea.KeyDown})
	down := downModel.(Model)
	if down.collectionViews[0].addSelected != 1 {
		t.Fatalf("addSelected = %d, want 1", down.collectionViews[0].addSelected)
	}

	gotModel, cmd := down.handleAddClipKey(tea.KeyMsg{Type: tea.KeyTab})
	got := gotModel.(Model)
	if cmd != nil {
		t.Fatal("expected no async command when adding selected fuzzy cache match")
	}
	row := got.collectionViews[0].rows[0]
	if row.CustomFields["title"] != "Nine Ball" {
		t.Fatalf("title = %q, want Nine Ball", row.CustomFields["title"])
	}
}

func TestAddClipBackspaceAndCaretEditBuffer(t *testing.T) {
	m := testCollectionModel(t)
	m.mode = modeAddClip
	m.addCvIdx = 0
	m.addBuffer = "https://youtu.be/abc123?si=test"
	m.addCursor = len(m.addBuffer)
	m.collectionViews[0].addFocus = true
	m.collectionViews[0].addBuffer = m.addBuffer
	m.collectionViews[0].addCursor = m.addCursor

	leftModel, _ := m.handleAddClipKey(tea.KeyMsg{Type: tea.KeyLeft})
	left := leftModel.(Model)
	if left.addCursor != len(m.addBuffer)-1 {
		t.Fatalf("addCursor after left = %d", left.addCursor)
	}

	gotModel, _ := left.handleAddClipKey(tea.KeyMsg{Type: tea.KeyBackspace})
	got := gotModel.(Model)
	want := "https://youtu.be/abc123?si=tet"
	if got.addBuffer != want {
		t.Fatalf("addBuffer = %q, want %q", got.addBuffer, want)
	}
	if got.collectionViews[0].addBuffer != want {
		t.Fatalf("view addBuffer = %q, want %q", got.collectionViews[0].addBuffer, want)
	}
	if got.addCursor != len(want)-1 {
		t.Fatalf("addCursor = %d, want %d", got.addCursor, len(want)-1)
	}
}

func TestInlineEditLinkCtrlRStartsProbe(t *testing.T) {
	m := testCollectionModel(t)
	m.mode = modeInlineEdit
	m.editFieldIdx = 4
	m.editValue = "https://example.com/watch?v=2"
	m.editOriginal = "https://example.com/watch?v=1"
	m.editCursor = len(m.editValue)
	m.collectionViews[0].editing = true
	m.collectionViews[0].editFieldIdx = 4
	m.collectionViews[0].editValue = m.editValue
	m.collectionViews[0].editCursor = m.editCursor

	gotModel, cmd := m.handleInlineEditKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	got := gotModel.(Model)

	if cmd == nil {
		t.Fatal("expected probe command")
	}
	row := got.collectionViews[0].rows[0]
	if row.CustomFields["link"] != "https://example.com/watch?v=2" {
		t.Fatalf("link = %q, want updated link", row.CustomFields["link"])
	}
	if got.collectionViews[0].rowStatus[row.Index] != "probing" {
		t.Fatalf("row status = %q, want probing", got.collectionViews[0].rowStatus[row.Index])
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
	m.timelineView.sequence = append(m.timelineView.sequence, config.SequenceEntry{Collection: "songs", Slice: "start:2"})
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

func TestProcessDeleteTimelineOutputRemovesFile(t *testing.T) {
	m := testTimelineModel(t)
	outputPath := filepath.Join(m.pp.Root, "powerhour.mp4")
	if err := os.WriteFile(outputPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}
	m.timelineView.concatFocus = true
	m.timelineView.concatPath = outputPath
	m.timelineView.concatExists = true

	got := m.processDeleteTimelineOutput()

	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("output still exists, stat err = %v", err)
	}
	if got.timelineView.concatExists {
		t.Fatal("concatExists = true, want false after delete")
	}
}

func TestRenderFooterTimelineIncludesEditShortcuts(t *testing.T) {
	m := testTimelineModel(t)

	footer := renderFooter(m)
	if !strings.Contains(footer, "e/E edit/ext") {
		t.Fatalf("footer = %q, want e/E edit/ext", footer)
	}
}

func TestRenderHelpOverlayTimelineIncludesEditShortcuts(t *testing.T) {
	help := renderHelpOverlay(0, 120, 40)
	if !strings.Contains(help, "Open selected output or project config") {
		t.Fatalf("help overlay missing timeline edit shortcut text: %q", help)
	}
}

func TestTimelineViewRendersOutputBeforeSequenceAndPreview(t *testing.T) {
	m := testTimelineModel(t)
	m.timelineView.concatExists = true
	m.timelineView.concatPath = filepath.Join(m.pp.Root, "powerhour.mp4")
	m.timelineView.concatSize = 128
	m.timelineView.concatModTime = time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	m.timelineView.termWidth = 120
	m.timelineView.termHeight = 40

	view := m.timelineView.view(nil)
	outputPos := strings.Index(view, "POWER HOUR")
	seqPos := strings.Index(view, "TIMELINE SEQUENCE")
	previewPos := strings.Index(view, "PLAYBACK ORDER")
	if outputPos < 0 || seqPos < 0 || previewPos < 0 {
		t.Fatalf("missing section label in view: %q", view)
	}
	if !(outputPos < seqPos && seqPos < previewPos) {
		t.Fatalf("section order invalid: output=%d seq=%d preview=%d", outputPos, seqPos, previewPos)
	}
}

func TestTimelineViewConcatFocusDoesNotAlsoHighlightSequence(t *testing.T) {
	m := testTimelineModel(t)
	m.timelineView.concatFocus = true
	m.timelineView.concatExists = true
	m.timelineView.concatPath = filepath.Join(m.pp.Root, "powerhour.mp4")
	m.timelineView.termWidth = 120
	m.timelineView.termHeight = 30

	view := m.timelineView.view(nil)
	if strings.Count(view, "▸ ") != 1 {
		t.Fatalf("view has %d visible cursors, want exactly 1\n%s", strings.Count(view, "▸ "), view)
	}
}

func TestTimelineViewRendersPlaybackOrderCursor(t *testing.T) {
	m := testTimelineModel(t)
	m.timelineView.focusPanel = 1
	m.timelineView.resolved = []project.TimelineEntry{{Collection: "songs", Index: 1, Sequence: 1}}
	m.timelineView.termWidth = 120
	m.timelineView.termHeight = 30
	m.collections["songs"] = project.Collection{
		Name: "songs",
		Rows: []csvplan.CollectionRow{{
			Index:           1,
			DurationSeconds: 60,
			CustomFields: map[string]string{
				"title":  "First Song",
				"artist": "Artist A",
			},
		}},
	}
	m.timelineView.collections = m.collections

	view := m.timelineView.view(nil)
	if !strings.Contains(view, "▸ ● 01 First Song") {
		t.Fatalf("playback order cursor missing:\n%s", view)
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
		Config:     config.CollectionConfig{OutputDir: "songs", CacheSearchProfile: "song_lookup"},
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
			Cache: config.Default().Cache,
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
