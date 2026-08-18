package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	m.cfg.Collections["songs"] = collCfg
	coll := m.collections["songs"]
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

func TestAddClipEnterAddsBestCachedMatch(t *testing.T) {
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
	m.cfg.Collections["songs"] = collCfg
	coll := m.collections["songs"]
	m.collections["songs"] = coll
	m.mode = modeAddClip
	m.addCvIdx = 0
	m.addBuffer = "Ninara"
	m.collectionViews[0].addFocus = true
	m.collectionViews[0].addBuffer = "Ninara"
	m = m.refreshAddClipHint(0)

	gotModel, cmd := m.handleAddClipKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := gotModel.(Model)

	if cmd != nil {
		t.Fatal("expected no async command when Enter adds fuzzy cache match")
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
	if row.Link != "https://example.com/watch?v=ninara" {
		t.Fatalf("link = %q, want cached source URL", row.Link)
	}
}

func TestAddClipEnterWithSelectedSuggestion(t *testing.T) {
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
	gotModel, cmd := down.handleAddClipKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := gotModel.(Model)
	if cmd != nil {
		t.Fatal("expected no async command when Enter adds selected fuzzy cache match")
	}
	row := got.collectionViews[0].rows[0]
	if row.CustomFields["title"] != "Nine Ball" {
		t.Fatalf("title = %q, want Nine Ball", row.CustomFields["title"])
	}
}

func TestAddClipEnterRefusesRawNonURLWithoutMatch(t *testing.T) {
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
	m.cacheIdx = &cache.Index{Entries: map[string]cache.Entry{}, Links: map[string]string{}}
	m.cfg.Cache = config.Default().Cache
	collCfg := m.cfg.Collections["songs"]
	m.cfg.Collections["songs"] = collCfg
	coll := m.collections["songs"]
	m.collections["songs"] = coll
	m.mode = modeAddClip
	m.addCvIdx = 0
	m.addBuffer = "xyzzy"
	m.collectionViews[0].addFocus = true
	m.collectionViews[0].addBuffer = "xyzzy"
	m = m.refreshAddClipHint(0)

	gotModel, cmd := m.handleAddClipKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := gotModel.(Model)
	if cmd != nil {
		t.Fatal("expected no async command on refusal")
	}
	if len(got.collectionViews[0].rows) != 0 {
		t.Fatalf("rows = %d, want 0 (row should not be added)", len(got.collectionViews[0].rows))
	}
	if got.statusMsg == "" {
		t.Fatal("expected an inline refusal note, got empty statusMsg")
	}
	if !got.collectionViews[0].addFocus {
		t.Fatal("expected add slot to remain focused after refusal")
	}
}

func TestAddClipEnterAddsUnknownRawURLVerbatim(t *testing.T) {
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
	m.cacheIdx = &cache.Index{Entries: map[string]cache.Entry{}, Links: map[string]string{}}
	m.cfg.Cache = config.Default().Cache
	collCfg := m.cfg.Collections["songs"]
	m.cfg.Collections["songs"] = collCfg
	coll := m.collections["songs"]
	m.collections["songs"] = coll
	m.mode = modeAddClip
	m.addCvIdx = 0
	rawURL := "https://example.com/videos/unknown-clip?ref=xyz&utm=1"
	m.addBuffer = rawURL
	m.collectionViews[0].addFocus = true
	m.collectionViews[0].addBuffer = rawURL
	m = m.refreshAddClipHint(0)

	gotModel, cmd := m.handleAddClipKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := gotModel.(Model)

	if cmd == nil {
		t.Fatal("expected a probe command for an unknown raw URL")
	}
	if len(got.collectionViews[0].rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(got.collectionViews[0].rows))
	}
	row := got.collectionViews[0].rows[0]
	if row.Link != rawURL {
		t.Fatalf("link = %q, want verbatim %q", row.Link, rawURL)
	}
	status := got.collectionViews[0].rowStatus[row.Index]
	if status != "probing" {
		t.Fatalf("row status = %q, want %q", status, "probing")
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

func TestRenderFooterCacheDescribesHandledKeys(t *testing.T) {
	m := testTimelineModel(t)
	m.activeView = len(m.collectionNames) + 1

	footer := renderFooter(m)
	if !strings.Contains(footer, "e edit") {
		t.Fatalf("footer = %q, want e edit", footer)
	}
	if !strings.Contains(footer, "D doctor") {
		t.Fatalf("footer = %q, want D doctor", footer)
	}
	if strings.Contains(footer, "d doctor") {
		t.Fatalf("footer = %q, should not advertise unbound d doctor", footer)
	}
	if strings.Contains(footer, "D all") {
		t.Fatalf("footer = %q, should not describe D as all", footer)
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

func TestMetadataProbeEmptyResultFlowsIntoInlineEdit(t *testing.T) {
	m := testCollectionModel(t)

	blankLink := "https://example.com/watch?v=blank"
	blankRow := csvplan.CollectionRow{
		Index: 2,
		Link:  blankLink,
		CustomFields: map[string]string{
			"title":      "",
			"artist":     "",
			"link":       blankLink,
			"start_time": "",
			"duration":   "",
		},
	}
	m.collectionViews[0].rows = append(m.collectionViews[0].rows, blankRow)
	coll := m.collections["songs"]
	coll.Rows = append([]csvplan.CollectionRow(nil), m.collectionViews[0].rows...)
	m.collections["songs"] = coll

	gotModel, _ := m.Update(metadataProbeMsg{collectionIdx: 0, link: blankLink, title: "", artist: "", err: nil})
	got := gotModel.(Model)

	if got.mode != modeInlineEdit {
		t.Fatalf("mode = %v, want modeInlineEdit", got.mode)
	}
	if !got.collectionViews[0].editing {
		t.Fatalf("collectionViews[0].editing = false, want true")
	}
	if got.collectionViews[0].cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (the blank row)", got.collectionViews[0].cursor)
	}
	titleIdx := -1
	for i, col := range got.collectionViews[0].columns {
		if col.field == "title" {
			titleIdx = i
			break
		}
	}
	if titleIdx == -1 {
		t.Fatalf("no title column found")
	}
	if got.editFieldIdx != titleIdx {
		t.Fatalf("editFieldIdx = %d, want %d (title column)", got.editFieldIdx, titleIdx)
	}
}

func TestMetadataProbeEmptyResultSkipsInlineEditWhenFieldNotDeclared(t *testing.T) {
	m := testCollectionModel(t)

	blankLink := "https://example.com/watch?v=blank"
	blankRow := csvplan.CollectionRow{
		Index: 2,
		Link:  blankLink,
		CustomFields: map[string]string{
			"link":       blankLink,
			"start_time": "",
			"duration":   "",
		},
	}
	m.collectionViews[0].rows = append(m.collectionViews[0].rows, blankRow)

	coll := m.collections["songs"]
	coll.Headers = []string{"link", "start_time", "duration"}
	coll.Rows = append([]csvplan.CollectionRow(nil), m.collectionViews[0].rows...)
	m.collections["songs"] = coll
	m.collectionViews[0].columns = discoverColumns(m.collectionViews[0].rows, coll.Headers)

	gotModel, _ := m.Update(metadataProbeMsg{collectionIdx: 0, link: blankLink, title: "", artist: "", err: nil})
	got := gotModel.(Model)

	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal (title/artist not declared on this collection)", got.mode)
	}
	if got.collectionViews[0].editing {
		t.Fatalf("collectionViews[0].editing = true, want false")
	}
}

func TestMetadataProbeFailureFlowsIntoInlineEdit(t *testing.T) {
	m := testCollectionModel(t)

	blankLink := "https://example.com/watch?v=blank"
	blankRow := csvplan.CollectionRow{
		Index: 2,
		Link:  blankLink,
		CustomFields: map[string]string{
			"title":      "",
			"artist":     "",
			"link":       blankLink,
			"start_time": "",
			"duration":   "",
		},
	}
	m.collectionViews[0].rows = append(m.collectionViews[0].rows, blankRow)
	coll := m.collections["songs"]
	coll.Rows = append([]csvplan.CollectionRow(nil), m.collectionViews[0].rows...)
	m.collections["songs"] = coll

	// Send probe message with error (network failure, yt-dlp error, etc.)
	gotModel, _ := m.Update(metadataProbeMsg{collectionIdx: 0, link: blankLink, title: "", artist: "", err: fmt.Errorf("network error")})
	got := gotModel.(Model)

	if got.mode != modeInlineEdit {
		t.Fatalf("mode = %v, want modeInlineEdit", got.mode)
	}
	if !got.collectionViews[0].editing {
		t.Fatalf("collectionViews[0].editing = false, want true")
	}
	if got.collectionViews[0].cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (the blank row)", got.collectionViews[0].cursor)
	}
	titleIdx := -1
	for i, col := range got.collectionViews[0].columns {
		if col.field == "title" {
			titleIdx = i
			break
		}
	}
	if titleIdx == -1 {
		t.Fatalf("no title column found")
	}
	if got.editFieldIdx != titleIdx {
		t.Fatalf("editFieldIdx = %d, want %d (title column)", got.editFieldIdx, titleIdx)
	}
}

func TestMetadataProbeFailureSkipsInlineEditWhenFieldNotDeclared(t *testing.T) {
	m := testCollectionModel(t)

	blankLink := "https://example.com/watch?v=blank"
	blankRow := csvplan.CollectionRow{
		Index: 2,
		Link:  blankLink,
		CustomFields: map[string]string{
			"link":       blankLink,
			"start_time": "",
			"duration":   "",
		},
	}
	m.collectionViews[0].rows = append(m.collectionViews[0].rows, blankRow)

	coll := m.collections["songs"]
	coll.Headers = []string{"link", "start_time", "duration"}
	coll.Rows = append([]csvplan.CollectionRow(nil), m.collectionViews[0].rows...)
	m.collections["songs"] = coll
	m.collectionViews[0].columns = discoverColumns(m.collectionViews[0].rows, coll.Headers)

	// Send probe message with error, even though title/artist would be empty.
	// Since they're not declared on this collection, inline edit should not happen.
	gotModel, _ := m.Update(metadataProbeMsg{collectionIdx: 0, link: blankLink, title: "", artist: "", err: fmt.Errorf("network error")})
	got := gotModel.(Model)

	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal (title/artist not declared on this collection)", got.mode)
	}
	if got.collectionViews[0].editing {
		t.Fatalf("collectionViews[0].editing = true, want false")
	}
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

func TestAddRowToEmptyYAMLCollectionMergesNewFields(t *testing.T) {
	t.Helper()

	root := t.TempDir()
	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	// Create an empty YAML collection with declared columns
	planPath := filepath.Join(root, "songs.yaml")
	yamlContent := `columns: [link, start_time, duration]
rows: []
`
	if err := os.WriteFile(planPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write yaml plan: %v", err)
	}

	// Load the empty YAML collection
	result, err := csvplan.LoadCollectionYAML(planPath, csvplan.CollectionOptions{})
	if err != nil {
		t.Fatalf("load yaml: %v", err)
	}

	// Build the collection with empty rows
	coll := project.Collection{
		Name:       "songs",
		Plan:       planPath,
		OutputDir:  "songs",
		Config:     config.CollectionConfig{OutputDir: "songs"},
		Rows:       result.Rows,
		Headers:    result.Columns,
		Defaults:   result.Defaults,
		Delimiter:  ',',
		PlanFormat: "yaml",
	}

	// Create a model with the empty collection
	m := Model{
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
			rows:     result.Rows,
			columns:  discoverColumns(result.Rows, result.Columns),
		}},
	}

	// Add a row with an undeclared field (mood)
	newRow := csvplan.CollectionRow{
		Index:           1,
		Link:            "https://example.com/watch?v=1",
		StartRaw:        "0:15",
		Start:           15 * time.Second,
		DurationSeconds: 60,
		CustomFields: map[string]string{
			"link":       "https://example.com/watch?v=1",
			"start_time": "0:15",
			"duration":   "60",
			"mood":       "upbeat", // undeclared field
		},
	}

	_, _ = m.addCollectionRow(0, newRow, addRowOutcome{})

	// After add+write+reload, the collection headers should include 'mood'
	reloadedColl := m.collections["songs"]
	found := false
	for _, h := range reloadedColl.Headers {
		if h == "mood" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("after add+write+reload, Headers = %v, want 'mood' included", reloadedColl.Headers)
	}

	// Verify write/reload round trip: reload the YAML file fresh and check columns
	reloaded, err := csvplan.LoadCollectionYAML(planPath, csvplan.CollectionOptions{})
	if err != nil {
		t.Fatalf("reload yaml: %v", err)
	}
	found = false
	for _, col := range reloaded.Columns {
		if col == "mood" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("after reload from disk, Columns = %v, want 'mood' included", reloaded.Columns)
	}
	if len(reloaded.Rows) != 1 {
		t.Fatalf("after reload, Rows len = %d, want 1", len(reloaded.Rows))
	}
	if reloaded.Rows[0].CustomFields["mood"] != "upbeat" {
		t.Fatalf("after reload, mood value = %q, want 'upbeat'", reloaded.Rows[0].CustomFields["mood"])
	}
}

func TestReloadYAMLCollectionWithUnmergedColumnsMergesRowFields(t *testing.T) {
	t.Helper()

	root := t.TempDir()
	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	// Create a YAML plan where columns omit a field present on a row
	// (simulating a manual edit or data drift)
	planPath := filepath.Join(root, "songs.yaml")
	yamlContent := `columns: [link, start_time, duration]
rows:
  - link: "https://example.com/watch?v=1"
    start_time: "0:15"
    duration: "60"
    mood: "upbeat"
`
	if err := os.WriteFile(planPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write yaml plan: %v", err)
	}

	// Load this collection (mood is in rows but not in columns)
	result, err := csvplan.LoadCollectionYAML(planPath, csvplan.CollectionOptions{})
	if err != nil {
		t.Fatalf("load yaml: %v", err)
	}

	// Build the collection
	coll := project.Collection{
		Name:       "songs",
		Plan:       planPath,
		OutputDir:  "songs",
		Config:     config.CollectionConfig{OutputDir: "songs"},
		Rows:       result.Rows,
		Headers:    result.Columns,
		Defaults:   result.Defaults,
		Delimiter:  ',',
		PlanFormat: "yaml",
	}

	// Create a model
	m := Model{
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
			rows:     result.Rows,
			columns:  discoverColumns(result.Rows, result.Columns),
		}},
	}

	// Verify that initially, the collection's Headers do NOT include 'mood'
	// (this is the state before the fix)
	if len(m.collections["songs"].Headers) != 3 {
		t.Fatalf("initial Headers len = %d, want 3 (link, start_time, duration)", len(m.collections["songs"].Headers))
	}
	moodFound := false
	for _, h := range m.collections["songs"].Headers {
		if h == "mood" {
			moodFound = true
		}
	}
	if moodFound {
		t.Fatal("initial Headers should not include mood, but it does")
	}

	// Reload the collection (this is where reloadCollection is called)
	m = reloadCollection(m, 0)

	// After fix, the collection headers should now include 'mood'
	// (because reloadCollection should merge row fields into Headers)
	found := false
	for _, h := range m.collections["songs"].Headers {
		if h == "mood" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("after reloadCollection, Headers = %v, want 'mood' included", m.collections["songs"].Headers)
	}
}

func TestReloadCSVCollectionWithExternallyAddedColumnReloadsHeaders(t *testing.T) {
	t.Helper()

	root := t.TempDir()
	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	// Create initial CSV file with headers
	planPath := filepath.Join(root, "songs.csv")
	initialCSV := "title,artist,link,start_time,duration\nFirst Song,Artist A,https://example.com/watch?v=1,0:15,60"
	if err := os.WriteFile(planPath, []byte(initialCSV), 0o644); err != nil {
		t.Fatalf("write csv plan: %v", err)
	}

	// Load the collection
	rows, err := csvplan.LoadCollection(planPath, csvplan.CollectionOptions{})
	if err != nil {
		t.Fatalf("load collection: %v", err)
	}

	// Build the collection
	coll := project.Collection{
		Name:       "songs",
		Plan:       planPath,
		OutputDir:  "songs",
		Config:     config.CollectionConfig{OutputDir: "songs"},
		Rows:       rows,
		Headers:    []string{"title", "artist", "link", "start_time", "duration"},
		Delimiter:  ',',
		PlanFormat: "csv",
	}

	// Build a model with the collection
	m := Model{
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
			rows:     rows,
			columns:  discoverColumns(rows, coll.Headers),
		}},
	}

	// Now externally add a new column to the CSV file on disk (simulating user editing outside the app)
	updatedCSV := "title,artist,link,start_time,duration,mood\nFirst Song,Artist A,https://example.com/watch?v=1,0:15,60,upbeat"
	if err := os.WriteFile(planPath, []byte(updatedCSV), 0o644); err != nil {
		t.Fatalf("update csv plan: %v", err)
	}

	// Call reloadCollection to simulate user re-entering the TUI
	m = reloadCollection(m, 0)

	// Verify the new column 'mood' appears in Headers
	reloadedColl := m.collections["songs"]
	found := false
	for _, h := range reloadedColl.Headers {
		if h == "mood" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("after reload, Headers = %v, want 'mood' included", reloadedColl.Headers)
	}

	// Verify delimiter is preserved
	if reloadedColl.Delimiter != ',' {
		t.Fatalf("after reload, Delimiter = %v, want ','", reloadedColl.Delimiter)
	}

	// Verify data-discovered fields are still merged in (mood should be in the row)
	if len(reloadedColl.Rows) > 0 {
		if reloadedColl.Rows[0].CustomFields["mood"] != "upbeat" {
			t.Fatalf("after reload, mood value = %q, want 'upbeat'", reloadedColl.Rows[0].CustomFields["mood"])
		}
	}
}

// TestReloadEmptiedPlanParity covers #178: an emptied CSV plan (header row,
// no data rows) must reload the same way an emptied YAML plan (rows: [])
// does -- zero rows, refreshed headers, no "Reload error:" status.
func TestReloadEmptiedPlanParity(t *testing.T) {
	buildModel := func(t *testing.T, root, planPath string, coll project.Collection, rows []csvplan.CollectionRow) Model {
		t.Helper()
		pp, err := paths.Resolve(root)
		if err != nil {
			t.Fatalf("resolve paths: %v", err)
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
				rows:     rows,
				columns:  discoverColumns(rows, coll.Headers),
			}},
		}
	}

	t.Run("csv", func(t *testing.T) {
		root := t.TempDir()
		planPath := filepath.Join(root, "songs.csv")
		initialCSV := "title,artist,link,start_time,duration,mood\nFirst Song,Artist A,https://example.com/watch?v=1,0:15,60,upbeat"
		if err := os.WriteFile(planPath, []byte(initialCSV), 0o644); err != nil {
			t.Fatalf("write csv plan: %v", err)
		}

		rows, err := csvplan.LoadCollection(planPath, csvplan.CollectionOptions{})
		if err != nil {
			t.Fatalf("load collection: %v", err)
		}

		coll := project.Collection{
			Name:       "songs",
			Plan:       planPath,
			OutputDir:  "songs",
			Config:     config.CollectionConfig{OutputDir: "songs"},
			Rows:       rows,
			Headers:    []string{"title", "artist", "link", "start_time", "duration", "mood"},
			Delimiter:  ',',
			PlanFormat: "csv",
		}

		m := buildModel(t, root, planPath, coll, rows)

		// Externally empty the plan down to a header row with a changed
		// column set (mood dropped, energy added).
		emptiedCSV := "title,artist,link,start_time,duration,energy\n"
		if err := os.WriteFile(planPath, []byte(emptiedCSV), 0o644); err != nil {
			t.Fatalf("rewrite csv plan: %v", err)
		}

		m = reloadCollection(m, 0)

		if strings.Contains(m.statusMsg, "Reload error:") {
			t.Fatalf("statusMsg = %q, want no reload error", m.statusMsg)
		}
		reloaded := m.collections["songs"]
		if len(reloaded.Rows) != 0 {
			t.Fatalf("after reload, rows = %d, want 0", len(reloaded.Rows))
		}
		wantHeaders := []string{"title", "artist", "link", "start_time", "duration", "energy"}
		if !reflect.DeepEqual(reloaded.Headers, wantHeaders) {
			t.Fatalf("after reload, Headers = %v, want %v", reloaded.Headers, wantHeaders)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		root := t.TempDir()
		planPath := filepath.Join(root, "songs.yaml")
		initialYAML := "columns: [title, artist, link, start_time, duration, mood]\n" +
			"rows:\n" +
			"  - title: First Song\n" +
			"    artist: Artist A\n" +
			"    link: https://example.com/watch?v=1\n" +
			"    start_time: \"0:15\"\n" +
			"    duration: 60\n" +
			"    mood: upbeat\n"
		if err := os.WriteFile(planPath, []byte(initialYAML), 0o644); err != nil {
			t.Fatalf("write yaml plan: %v", err)
		}

		result, err := csvplan.LoadCollectionYAML(planPath, csvplan.CollectionOptions{})
		if err != nil {
			t.Fatalf("load collection yaml: %v", err)
		}

		coll := project.Collection{
			Name:       "songs",
			Plan:       planPath,
			OutputDir:  "songs",
			Config:     config.CollectionConfig{OutputDir: "songs"},
			Rows:       result.Rows,
			Headers:    csvplan.MergeHeaders(result.Columns, result.Rows),
			PlanFormat: "yaml",
		}

		m := buildModel(t, root, planPath, coll, result.Rows)

		// Externally empty the plan with a changed column set (mood
		// dropped, energy added), matching the CSV case.
		emptiedYAML := "columns: [title, artist, link, start_time, duration, energy]\nrows: []\n"
		if err := os.WriteFile(planPath, []byte(emptiedYAML), 0o644); err != nil {
			t.Fatalf("rewrite yaml plan: %v", err)
		}

		m = reloadCollection(m, 0)

		if strings.Contains(m.statusMsg, "Reload error:") {
			t.Fatalf("statusMsg = %q, want no reload error", m.statusMsg)
		}
		reloaded := m.collections["songs"]
		if len(reloaded.Rows) != 0 {
			t.Fatalf("after reload, rows = %d, want 0", len(reloaded.Rows))
		}
		wantHeaders := []string{"title", "artist", "link", "start_time", "duration", "energy"}
		if !reflect.DeepEqual(reloaded.Headers, wantHeaders) {
			t.Fatalf("after reload, Headers = %v, want %v", reloaded.Headers, wantHeaders)
		}
	})
}

func TestRevealCommandForGOOS(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		path    string
		wantCmd string
		wantArg string
	}{
		{
			name:    "darwin opens with open command",
			goos:    "darwin",
			path:    "/Users/test/project",
			wantCmd: "open",
			wantArg: "/Users/test/project",
		},
		{
			name:    "windows opens with explorer command",
			goos:    "windows",
			path:    "C:\\Users\\test\\project",
			wantCmd: "explorer",
			wantArg: "C:\\Users\\test\\project",
		},
		{
			name:    "linux opens with xdg-open command",
			goos:    "linux",
			path:    "/home/test/project",
			wantCmd: "xdg-open",
			wantArg: "/home/test/project",
		},
		{
			name:    "default (freebsd) opens with xdg-open command",
			goos:    "freebsd",
			path:    "/home/test/project",
			wantCmd: "xdg-open",
			wantArg: "/home/test/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotArgs := revealCommandForGOOS(tt.goos, tt.path)
			if gotCmd != tt.wantCmd {
				t.Errorf("command: got %q, want %q", gotCmd, tt.wantCmd)
			}
			if len(gotArgs) != 1 || gotArgs[0] != tt.wantArg {
				t.Errorf("args: got %v, want [%q]", gotArgs, tt.wantArg)
			}
		})
	}
}

// newTestCacheModel builds a Model wired for processDeleteCacheEntry tests: a
// real ProjectPaths rooted at root, idx persisted to disk via cache.Save (so
// the reload triggered by delete round-trips through real files), and a
// "songs" collection whose rows reference referencedLinks (used to make
// cacheView's filtered/all entry sets differ where a test needs that).
func newTestCacheModel(t *testing.T, root string, idx *cache.Index, referencedLinks []string) Model {
	t.Helper()

	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if err := cache.Save(pp, idx); err != nil {
		t.Fatalf("save index: %v", err)
	}

	var rows []csvplan.CollectionRow
	for _, link := range referencedLinks {
		rows = append(rows, csvplan.CollectionRow{Link: link})
	}
	coll := project.Collection{
		Name: "songs",
		Rows: rows,
	}

	cfg := config.Config{
		Cache:       config.Default().Cache,
		Collections: map[string]config.CollectionConfig{"songs": coll.Config},
	}

	m := Model{
		cfg:             cfg,
		pp:              pp,
		collections:     map[string]project.Collection{"songs": coll},
		collectionNames: []string{"songs"},
		collectionViews: []collectionView{{name: "songs", rows: rows}},
		cacheIdx:        idx,
	}
	m.cacheView = newCacheView(cfg, idx, buildCollectionLinks(m.collections))
	return m
}

// writeTestCacheFile writes a small real file under dir and returns its path,
// so os.Remove (URL-sourced deletes) and os.Stat assertions have something
// real to act on.
func writeTestCacheFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}
	return path
}

// buildNCacheEntries builds n URL-sourced cache entries, each with a real
// cached file under root and a link recorded in idx.Links, and returns the
// index plus the list of links (in entry order, for use as referencedLinks).
func buildNCacheEntries(t *testing.T, root string, n int) (*cache.Index, []string) {
	t.Helper()

	idx := &cache.Index{Entries: map[string]cache.Entry{}, Links: map[string]string{}}
	var links []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("entry%d", i)
		link := fmt.Sprintf("https://example.com/watch?v=%d", i)
		path := writeTestCacheFile(t, root, fmt.Sprintf("entry%d.mp4", i))
		idx.Entries[id] = cache.Entry{
			Key:        id,
			Identifier: id,
			Source:     link,
			SourceType: cache.SourceTypeURL,
			CachedPath: path,
			Title:      fmt.Sprintf("Song %d", i),
		}
		idx.Links[link] = id
		links = append(links, link)
	}
	return idx, links
}

func TestProcessDeleteCacheEntryRemovesURLSourcedFile(t *testing.T) {
	root := t.TempDir()
	cachedPath := writeTestCacheFile(t, root, "url-entry.mp4")
	link := "https://example.com/watch?v=url1"

	idx := &cache.Index{
		Entries: map[string]cache.Entry{
			"url1": {
				Key:        "url1",
				Identifier: "url1",
				Source:     link,
				SourceType: cache.SourceTypeURL,
				CachedPath: cachedPath,
				Title:      "URL Song",
			},
		},
		Links: map[string]string{link: "url1"},
	}

	m := newTestCacheModel(t, root, idx, []string{link})
	m.cacheView.cursor = 0

	gotModel, _ := m.processDeleteCacheEntry()
	got := gotModel.(Model)

	if _, err := os.Stat(cachedPath); !os.IsNotExist(err) {
		t.Fatalf("cached file stat after delete: err = %v, want IsNotExist", err)
	}
	if _, ok := got.cacheIdx.GetByIdentifier("url1"); ok {
		t.Fatal("index entry \"url1\" still present after delete")
	}
	if _, ok := got.cacheIdx.Links[link]; ok {
		t.Fatalf("link mapping %q still present after delete", link)
	}
}

func TestProcessDeleteCacheEntryPreservesLocalSourcedFile(t *testing.T) {
	root := t.TempDir()
	cachedPath := writeTestCacheFile(t, root, "local-entry.mp4")

	idx := &cache.Index{
		Entries: map[string]cache.Entry{
			"local1": {
				Key:        "local1",
				Identifier: "local1",
				Source:     cachedPath,
				SourceType: cache.SourceTypeLocal,
				CachedPath: cachedPath,
				Title:      "Local Song",
			},
		},
	}

	// A local-sourced entry never carries a URL Source, so newCacheView's
	// project-referenced check (which only matches URLs) can never place it
	// in filteredEntries — show all so it is reachable at cursor 0.
	m := newTestCacheModel(t, root, idx, nil)
	m.cacheView.showAll = true
	m.cacheView.cursor = 0

	gotModel, _ := m.processDeleteCacheEntry()
	got := gotModel.(Model)

	if _, err := os.Stat(cachedPath); err != nil {
		t.Fatalf("local file removed after delete (should be preserved): %v", err)
	}
	if _, ok := got.cacheIdx.GetByIdentifier("local1"); ok {
		t.Fatal("index entry \"local1\" still present after delete")
	}
}

// TestProcessDeleteCacheEntryCursorResetsWhenLastRowDeleted documents the
// actual current behavior of the post-delete cursor-adjustment block: since
// reloadState rebuilds m.cacheView via newCacheView (whose struct literal
// never sets cursor), m.cacheView.cursor is unconditionally 0 by the time the
// "cursor >= len(entries) && cursor > 0" guard runs — so the guard's second
// clause is never true and the decrement never fires. The observable result
// is that cursor always ends at 0, regardless of which row was deleted.
func TestProcessDeleteCacheEntryCursorResetsWhenLastRowDeleted(t *testing.T) {
	root := t.TempDir()
	idx, links := buildNCacheEntries(t, root, 2)
	m := newTestCacheModel(t, root, idx, links)
	m.cacheView.cursor = 1 // last row

	gotModel, _ := m.processDeleteCacheEntry()
	got := gotModel.(Model)

	if len(got.cacheView.entries()) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.cacheView.entries()))
	}
	if got.cacheView.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after deleting the last row", got.cacheView.cursor)
	}
}

func TestProcessDeleteCacheEntryCursorResetsWhenMiddleRowDeleted(t *testing.T) {
	root := t.TempDir()
	idx, links := buildNCacheEntries(t, root, 3)
	m := newTestCacheModel(t, root, idx, links)
	m.cacheView.cursor = 1 // middle row

	gotModel, _ := m.processDeleteCacheEntry()
	got := gotModel.(Model)

	if len(got.cacheView.entries()) != 2 {
		t.Fatalf("entries = %d, want 2", len(got.cacheView.entries()))
	}
	if got.cacheView.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after deleting a middle row", got.cacheView.cursor)
	}

	remaining := got.cacheView.entries()
	note := got.cacheView.rowStatus[remaining[got.cacheView.cursor].Identifier]
	if !strings.HasPrefix(note, "note:removed ") {
		t.Fatalf("row note for surviving cursor row = %q, want prefix \"note:removed \"", note)
	}
}

func TestProcessDeleteCacheEntryLastRemainingEntryNoNote(t *testing.T) {
	root := t.TempDir()
	idx, links := buildNCacheEntries(t, root, 1)
	m := newTestCacheModel(t, root, idx, links)
	m.cacheView.cursor = 0

	gotModel, _ := m.processDeleteCacheEntry()
	got := gotModel.(Model)

	if len(got.cacheView.entries()) != 0 {
		t.Fatalf("entries = %d, want 0", len(got.cacheView.entries()))
	}
	if got.cacheView.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", got.cacheView.cursor)
	}
	if len(got.cacheView.rowStatus) != 0 {
		t.Fatalf("rowStatus = %v, want empty (no note set when no entries remain)", got.cacheView.rowStatus)
	}
}

func TestProcessDeleteCacheEntryPreservesFilterModeShowAllTrue(t *testing.T) {
	root := t.TempDir()
	refLink := "https://example.com/watch?v=ref"
	refPath := writeTestCacheFile(t, root, "ref.mp4")
	otherPath := writeTestCacheFile(t, root, "other.mp4")

	idx := &cache.Index{
		Entries: map[string]cache.Entry{
			"ref": {
				Key: "ref", Identifier: "ref", Source: refLink,
				SourceType: cache.SourceTypeURL, CachedPath: refPath, Title: "Referenced",
			},
			"other": {
				Key: "other", Identifier: "other", Source: "https://example.com/watch?v=other",
				SourceType: cache.SourceTypeURL, CachedPath: otherPath, Title: "Not Referenced",
			},
		},
		Links: map[string]string{refLink: "ref"},
	}

	m := newTestCacheModel(t, root, idx, []string{refLink})
	m.cacheView.showAll = true
	if len(m.cacheView.allEntries) != 2 || len(m.cacheView.filteredEntries) != 1 {
		t.Fatalf("fixture setup: allEntries=%d filteredEntries=%d, want 2 and 1 (filter must discriminate)",
			len(m.cacheView.allEntries), len(m.cacheView.filteredEntries))
	}
	m.cacheView.cursor = 0

	gotModel, _ := m.processDeleteCacheEntry()
	got := gotModel.(Model)

	if !got.cacheView.showAll {
		t.Fatal("showAll = false, want true (filter mode must be preserved across the delete-triggered reload)")
	}
}

func TestProcessDeleteCacheEntryPreservesFilterModeShowAllFalse(t *testing.T) {
	root := t.TempDir()
	refLink := "https://example.com/watch?v=ref"
	refPath := writeTestCacheFile(t, root, "ref.mp4")
	otherPath := writeTestCacheFile(t, root, "other.mp4")

	idx := &cache.Index{
		Entries: map[string]cache.Entry{
			"ref": {
				Key: "ref", Identifier: "ref", Source: refLink,
				SourceType: cache.SourceTypeURL, CachedPath: refPath, Title: "Referenced",
			},
			"other": {
				Key: "other", Identifier: "other", Source: "https://example.com/watch?v=other",
				SourceType: cache.SourceTypeURL, CachedPath: otherPath, Title: "Not Referenced",
			},
		},
		Links: map[string]string{refLink: "ref"},
	}

	m := newTestCacheModel(t, root, idx, []string{refLink})
	m.cacheView.showAll = false
	if len(m.cacheView.entries()) != 1 {
		t.Fatalf("fixture setup: filtered entries = %d, want 1 (filter must discriminate)", len(m.cacheView.entries()))
	}
	m.cacheView.cursor = 0

	gotModel, _ := m.processDeleteCacheEntry()
	got := gotModel.(Model)

	if got.cacheView.showAll {
		t.Fatal("showAll = true, want false (filter mode must be preserved across the delete-triggered reload)")
	}
}

// TestUpdateGlobalKeyFiresRegardlessOfView proves the arrow-key view switch
// in handleKey is handled by the global key block before any view-specific
// delegation runs, independent of which view is currently active.
func TestUpdateGlobalKeyFiresRegardlessOfView(t *testing.T) {
	m := testCollectionModel(t)
	m.viewNames = []string{"timeline", "songs", "cache", "tools"}

	gotModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	got := gotModel.(Model)

	if got.activeView != 2 {
		t.Fatalf("activeView = %d, want 2 (right arrow must advance the active view via the global key switch)", got.activeView)
	}
}

// TestUpdateConfirmDeleteSwallowsCollectionKey proves that while the model
// is in modeConfirmDelete, a key that would normally be handled by the
// collection view's own key dispatch (here "e", which opens inline edit)
// does not leak through to that dispatch. handleConfirmDeleteKey's default
// branch resets the mode to modeNormal for any non-y/Y/enter key instead.
func TestUpdateConfirmDeleteSwallowsCollectionKey(t *testing.T) {
	m := testCollectionModel(t)
	m.mode = modeConfirmDelete

	gotModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	got := gotModel.(Model)

	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal (%v) — \"e\" must not leak through modeConfirmDelete into the collection view's inline-edit dispatch (got modeInlineEdit = %v)", got.mode, modeNormal, modeInlineEdit)
	}
}

// TestUpdateTextInputConsumesKeystroke proves that while the model is in
// modeInput, a keystroke that would otherwise be a global key (here "q",
// which quits when mode == modeNormal) is consumed by the text-input
// handler instead of falling through to the global quit handler.
func TestUpdateTextInputConsumesKeystroke(t *testing.T) {
	m := Model{
		mode:       modeInput,
		input:      newTextInput("Add:"),
		activeView: 1,
		viewNames:  []string{"timeline", "songs", "cache", "tools"},
	}

	gotModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	got := gotModel.(Model)

	if cmd != nil {
		t.Fatal("cmd != nil, want nil — \"q\" must be consumed by the text-input handler, not fall through to the global quit handler (which returns tea.Quit)")
	}
	if got.input.value != "q" {
		t.Fatalf("input.value = %q, want %q — the keystroke must be appended into the text buffer", got.input.value, "q")
	}
	if got.mode != modeInput {
		t.Fatalf("mode = %v, want modeInput (%v) — the model must stay in text-input mode after a plain keystroke", got.mode, modeInput)
	}
}
