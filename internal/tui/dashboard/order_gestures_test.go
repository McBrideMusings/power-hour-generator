package dashboard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/playback"
	"powerhour/internal/project"
	"powerhour/internal/render/state"
	"powerhour/pkg/csvplan"
)

// testOrderGestureModel builds a real dashboard.Model (via NewModel, so
// m.order is resolved through playback.ResolveOrder like every other entry
// point) over a project with a `once` collection (songs, 3 rows) and a
// `repeat` collection (ads, 2 rows), both placed directly in the timeline
// sequence with no interleave — so BuildTimelinePlacements places every row
// of each once, in row order, giving 5 playback-order slots: songs A, B, C
// then ads A, B.
func testOrderGestureModel(t *testing.T) Model {
	t.Helper()

	root := t.TempDir()
	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}

	songs := project.Collection{
		Name:   "songs",
		Config: config.CollectionConfig{Selection: "once", Display: "{title}"},
		Rows: []csvplan.CollectionRow{
			{Index: 1, RowID: "aaa111", CustomFields: map[string]string{"title": "Song A"}},
			{Index: 2, RowID: "bbb222", CustomFields: map[string]string{"title": "Song B"}},
			{Index: 3, RowID: "ccc333", CustomFields: map[string]string{"title": "Song C"}},
		},
	}
	ads := project.Collection{
		Name:   "ads",
		Config: config.CollectionConfig{Selection: "repeat", Display: "{title}"},
		Rows: []csvplan.CollectionRow{
			{Index: 1, RowID: "ddd444", CustomFields: map[string]string{"title": "Ad A"}},
			{Index: 2, RowID: "eee555", CustomFields: map[string]string{"title": "Ad B"}},
		},
	}

	cfg := config.Config{
		Collections: map[string]config.CollectionConfig{
			"songs": songs.Config,
			"ads":   ads.Config,
		},
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{Collection: "songs"},
				{Collection: "ads"},
			},
		},
	}

	collections := map[string]project.Collection{"songs": songs, "ads": ads}

	idx, _ := cache.Load(pp)
	rs, _ := state.Load(pp.RenderStateFile)

	m := NewModel(cfg, pp, collections, nil, idx, rs, "", nil)
	m.termWidth = 120
	m.termHeight = 40
	m.timelineView.termWidth = 120
	m.timelineView.termHeight = 40
	m.timelineView.focusPanel = 1

	if len(m.order.Slots) != 5 {
		t.Fatalf("setup: order has %d slots, want 5 (order: %+v)", len(m.order.Slots), m.order.Slots)
	}
	return m
}

// TestOrderGestureSwap verifies the mark-and-swap gesture on a `once`
// collection: s marks the cursor slot, s on a slot of the same collection
// exchanges the two occupants and clears the mark, and the result is
// persisted to playback-order.yaml.
func TestOrderGestureSwap(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 0 // songs A

	marked, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = marked.(Model)
	if m.timelineView.markedSlot != 0 {
		t.Fatalf("markedSlot = %d, want 0", m.timelineView.markedSlot)
	}

	m.timelineView.resCursor = 2 // songs C
	swapped, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	got := swapped.(Model)

	if got.timelineView.markedSlot != -1 {
		t.Fatalf("markedSlot = %d, want -1 after swap", got.timelineView.markedSlot)
	}
	if got.order.Slots[0].RowID != "ccc333" || got.order.Slots[2].RowID != "aaa111" {
		t.Fatalf("order not swapped: slot0=%q slot2=%q", got.order.Slots[0].RowID, got.order.Slots[2].RowID)
	}

	onDisk, found, err := playback.Load(got.pp.Root)
	if err != nil || !found {
		t.Fatalf("playback.Load: found=%v err=%v", found, err)
	}
	if onDisk.Slots[0].RowID != "ccc333" {
		t.Fatalf("persisted order slot0 = %q, want ccc333", onDisk.Slots[0].RowID)
	}
}

// TestOrderGesturePickerOnRepeat verifies that s on a `repeat` collection's
// slot opens the picker overlay (rather than marking) with the collection's
// pool as candidates.
func TestOrderGesturePickerOnRepeat(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 3 // ads A

	got, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	gm := got.(Model)

	if gm.overlay != overlayPicker || gm.pickerOverlay == nil {
		t.Fatalf("overlay = %v, pickerOverlay = %v; want overlayPicker with a picker", gm.overlay, gm.pickerOverlay)
	}
	if gm.pickerOverlay.slot != 3 {
		t.Fatalf("picker slot = %d, want 3", gm.pickerOverlay.slot)
	}
	if len(gm.pickerOverlay.items) != 2 {
		t.Fatalf("picker items = %d, want 2 (ads pool)", len(gm.pickerOverlay.items))
	}
	if gm.timelineView.markedSlot != -1 {
		t.Fatalf("markedSlot = %d, want -1 (repeat never marks)", gm.timelineView.markedSlot)
	}

	// Enter applies the highlighted item via playback.Set.
	applied, _ := gm.pickerOverlay.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !applied {
		t.Fatal("handleKey(Enter) apply = false, want true")
	}
	final := gm.applyOrderPicker()
	if final.overlay != overlayNone || final.pickerOverlay != nil {
		t.Fatalf("overlay = %v, pickerOverlay = %v after apply, want cleared", final.overlay, final.pickerOverlay)
	}
	if final.order.Slots[3].RowID != "ddd444" {
		t.Fatalf("slot 3 RowID = %q, want ddd444 (first pool item)", final.order.Slots[3].RowID)
	}
}

// TestOrderGestureFileSlotHasNoPool verifies s on a file slot only notes —
// it neither marks nor mutates, matching playback.mutableSlot's rejection.
func TestOrderGestureFileSlotHasNoPool(t *testing.T) {
	m := testOrderGestureModel(t)
	m.order.Slots = append(m.order.Slots, playback.Slot{File: "/media/bumper.mp4"})
	m.timelineView.resCursor = len(m.order.Slots) - 1

	got, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	gm := got.(Model)

	if gm.timelineView.orderNote != "file entries have no pool" {
		t.Fatalf("orderNote = %q, want the file-slot note", gm.timelineView.orderNote)
	}
	if gm.timelineView.markedSlot != -1 {
		t.Fatalf("markedSlot = %d, want -1", gm.timelineView.markedSlot)
	}
}

// TestOrderGestureLock verifies l toggles Locked on the cursor slot and
// persists it to playback-order.yaml.
func TestOrderGestureLock(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 1 // songs B

	locked, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	got := locked.(Model)

	if !got.order.Slots[1].Locked {
		t.Fatal("slot 1 Locked = false, want true after l")
	}
	if got.timelineView.orderNote != "locked" {
		t.Fatalf("orderNote = %q, want %q", got.timelineView.orderNote, "locked")
	}

	onDisk, found, err := playback.Load(got.pp.Root)
	if err != nil || !found {
		t.Fatalf("playback.Load: found=%v err=%v", found, err)
	}
	if !onDisk.Slots[1].Locked {
		t.Fatal("persisted slot 1 Locked = false, want true")
	}

	unlocked, _ := got.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	got2 := unlocked.(Model)
	if got2.order.Slots[1].Locked {
		t.Fatal("slot 1 Locked = true after second l, want false")
	}
	if got2.timelineView.orderNote != "unlocked" {
		t.Fatalf("orderNote = %q, want %q", got2.timelineView.orderNote, "unlocked")
	}
}

// TestOrderGestureLockThroughGlobalKeyHandler drives l through handleKey, the
// real entry point, rather than straight into the timeline handler. l used to
// be a global next-view binding, so the key switched tabs and never reached
// the lock gesture; the direct-call tests above could not see that.
func TestOrderGestureLockThroughGlobalKeyHandler(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 1 // songs B

	locked, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	got := locked.(Model)

	if got.activeView != 0 {
		t.Fatalf("activeView = %d, want 0 — l must not switch views", got.activeView)
	}
	if !got.order.Slots[1].Locked {
		t.Fatal("slot 1 Locked = false, want true after l")
	}
}

// TestOrderGestureShuffleSkipsLockedSlots verifies S shuffles a collection's
// unlocked slots and leaves a locked one untouched in position.
func TestOrderGestureShuffleSkipsLockedSlots(t *testing.T) {
	m := testOrderGestureModel(t)

	// Lock slot 0 (songs A) first.
	m.timelineView.resCursor = 0
	locked, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = locked.(Model)
	lockedRowID := m.order.Slots[0].RowID

	shuffled, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	got := shuffled.(Model)

	if got.order.Slots[0].RowID != lockedRowID {
		t.Fatalf("locked slot 0 RowID changed: got %q, want %q", got.order.Slots[0].RowID, lockedRowID)
	}
	if !strings.Contains(got.timelineView.orderNote, "shuffled") {
		t.Fatalf("orderNote = %q, want a shuffled-count note", got.timelineView.orderNote)
	}

	// The two unlocked songs slots must still both be present (once
	// selection permutes occupants, never redraws them).
	seen := map[string]bool{got.order.Slots[1].RowID: true, got.order.Slots[2].RowID: true}
	if !seen["bbb222"] || !seen["ccc333"] {
		t.Fatalf("unlocked slots lost an occupant: slot1=%q slot2=%q", got.order.Slots[1].RowID, got.order.Slots[2].RowID)
	}
}

// TestOrderGestureEscClearsMark verifies Esc clears a pending mark without
// quitting the dashboard, and without touching the order.
func TestOrderGestureEscClearsMark(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 0

	marked, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = marked.(Model)
	if m.timelineView.markedSlot != 0 {
		t.Fatalf("markedSlot = %d, want 0", m.timelineView.markedSlot)
	}

	before := append([]playback.Slot(nil), m.order.Slots...)

	result, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	got := result.(Model)
	if cmd != nil {
		t.Fatal("Esc with a pending mark returned a quit cmd, want nil (no quit)")
	}
	if got.timelineView.markedSlot != -1 {
		t.Fatalf("markedSlot = %d, want -1 after Esc", got.timelineView.markedSlot)
	}
	for i, s := range got.order.Slots {
		if s.RowID != before[i].RowID {
			t.Fatalf("order mutated by Esc at slot %d: got %q, want %q", i, s.RowID, before[i].RowID)
		}
	}
}

// TestFooterHidesMutationKeysInSequencePanel verifies the sequence panel's
// footer advertises no mutation key, while the playback order panel's does.
func TestFooterHidesMutationKeysInSequencePanel(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.focusPanel = 0

	seqFooter := renderFooter(m)
	for _, key := range []string{"a add", "x del", "J/K reorder", "s swap", "l lock", "S shuffle"} {
		if strings.Contains(seqFooter, key) {
			t.Fatalf("sequence-panel footer = %q, should not advertise %q", seqFooter, key)
		}
	}

	m.timelineView.focusPanel = 1
	orderFooter := renderFooter(m)
	if !strings.Contains(orderFooter, "s swap/pick") || !strings.Contains(orderFooter, "l lock") || !strings.Contains(orderFooter, "S shuffle") {
		t.Fatalf("playback-order footer = %q, want the s/l/S gesture keys", orderFooter)
	}
}
