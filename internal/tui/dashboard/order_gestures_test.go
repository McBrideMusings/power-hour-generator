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

// TestOrderGestureCycleOnce verifies cycle mode on a `once` collection: s
// arms the cursor slot, → steps its occupant to the next pool row, and
// because that row already holds another slot the two exchange — so every
// row still occupies exactly one slot. The step stays in memory until Enter.
func TestOrderGestureCycleOnce(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 0 // songs A

	armed, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = armed.(Model)
	if !m.timelineView.cycling || m.timelineView.cycleSlot != 0 {
		t.Fatalf("cycling=%v cycleSlot=%d, want true/0", m.timelineView.cycling, m.timelineView.cycleSlot)
	}

	stepped, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	got := stepped.(Model)

	if got.activeView != 0 {
		t.Fatalf("activeView = %d, want 0 — cycle mode owns the arrows", got.activeView)
	}
	if got.order.Slots[0].RowID != "bbb222" || got.order.Slots[1].RowID != "aaa111" {
		t.Fatalf("once cycle did not swap: slot0=%q slot1=%q", got.order.Slots[0].RowID, got.order.Slots[1].RowID)
	}
	if !got.timelineView.cycling || got.timelineView.cycleSlot != 0 {
		t.Fatalf("cycling=%v cycleSlot=%d — a step must not leave cycle mode", got.timelineView.cycling, got.timelineView.cycleSlot)
	}

	// Nothing is on disk yet: the step is pending until Enter.
	if _, found, _ := playback.Load(got.pp.Root); found {
		t.Fatal("a pending cycle step wrote playback-order.yaml, want nothing written until Enter")
	}

	done, _ := got.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	final := done.(Model)
	if final.timelineView.cycling {
		t.Fatal("still cycling after Enter, want cycle mode left")
	}
	onDisk, found, err := playback.Load(final.pp.Root)
	if err != nil || !found {
		t.Fatalf("playback.Load after Enter: found=%v err=%v", found, err)
	}
	if onDisk.Slots[0].RowID != "bbb222" || onDisk.Slots[1].RowID != "aaa111" {
		t.Fatalf("persisted slots = %q/%q, want bbb222/aaa111", onDisk.Slots[0].RowID, onDisk.Slots[1].RowID)
	}
}

// TestOrderGestureCycleEscUndoesEverySlot is the guarantee the gesture is
// built around: Esc restores the order as it stood when s was pressed —
// including the far side of a swap, which is not the row under the cursor —
// and leaves playback-order.yaml untouched.
func TestOrderGestureCycleEscUndoesEverySlot(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 0
	before := []string{m.order.Slots[0].RowID, m.order.Slots[1].RowID, m.order.Slots[2].RowID}

	armed, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	stepped, _ := armed.(Model).handleKey(tea.KeyMsg{Type: tea.KeyRight})
	stepped2, _ := stepped.(Model).handleKey(tea.KeyMsg{Type: tea.KeyRight})
	moved := stepped2.(Model)
	if moved.order.Slots[0].RowID == before[0] {
		t.Fatal("setup: two right presses did not move slot 0")
	}

	undone, _ := moved.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	got := undone.(Model)
	for i, want := range before {
		if got.order.Slots[i].RowID != want {
			t.Fatalf("slot %d = %q after Esc, want %q restored", i, got.order.Slots[i].RowID, want)
		}
	}
	if got.timelineView.cycling {
		t.Fatal("still cycling after Esc, want cycle mode left")
	}
	if _, found, _ := playback.Load(got.pp.Root); found {
		t.Fatal("Esc left a playback-order.yaml behind, want nothing ever written")
	}
}

// TestOrderGestureCycleSwallowsOtherKeys verifies cycle mode is modal: a key
// that is neither a step nor a decision cannot leave the mode, so it can
// never silently commit or discard the pending edit.
func TestOrderGestureCycleSwallowsOtherKeys(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 0

	armed, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	stepped, _ := armed.(Model).handleKey(tea.KeyMsg{Type: tea.KeyRight})
	pending := stepped.(Model)

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyDown},
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyRunes, Runes: []rune("l")},
		{Type: tea.KeyRunes, Runes: []rune("2")},
	} {
		next, cmd := pending.handleKey(key)
		got := next.(Model)
		if cmd != nil {
			t.Fatalf("key %v returned a cmd while cycling, want nil", key)
		}
		if !got.timelineView.cycling {
			t.Fatalf("key %v left cycle mode, want it swallowed", key)
		}
		if got.activeView != 0 {
			t.Fatalf("key %v switched view while cycling", key)
		}
		if _, found, _ := playback.Load(got.pp.Root); found {
			t.Fatalf("key %v caused a write while cycling", key)
		}
	}
}

// TestOrderGestureCycleRepeat verifies cycle mode on a `repeat` collection:
// the step assigns from the pool (playback.Cycle's repeat branch, covered
// directly in internal/playback). ← wraps backwards off the first row.
func TestOrderGestureCycleRepeat(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 3 // ads A

	armed, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = armed.(Model)
	stepped, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	got := stepped.(Model)

	if got.order.Slots[3].RowID != "eee555" {
		t.Fatalf("slot 3 RowID = %q, want eee555 (next in the ads pool)", got.order.Slots[3].RowID)
	}
	if !got.timelineView.cyclePending(3) {
		t.Fatal("slot 3 not marked pending, want the panel to show it as unsaved")
	}
	// ← from the first pool row wraps to the last.
	m2 := testOrderGestureModel(t)
	m2.timelineView.resCursor = 3
	armed2, _ := m2.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	back, _ := armed2.(Model).handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if got := back.(Model).order.Slots[3].RowID; got != "eee555" {
		t.Fatalf("slot 3 RowID = %q after left-wrap, want eee555", got)
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
	if gm.timelineView.cycling {
		t.Fatal("a file slot armed cycle mode, want it refused")
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

// TestOrderGestureEscWithNoStepIsHarmless verifies Esc with no pending step
// leaves cycle mode without quitting the dashboard or touching the order.
func TestOrderGestureEscWithNoStepIsHarmless(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.resCursor = 0

	armed, _ := m.handleTimelineKeyWithMutations(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = armed.(Model)
	if !m.timelineView.cycling || m.timelineView.cycleSlot != 0 {
		t.Fatalf("cycling=%v cycleSlot=%d, want true/0", m.timelineView.cycling, m.timelineView.cycleSlot)
	}

	before := append([]playback.Slot(nil), m.order.Slots...)

	result, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	got := result.(Model)
	if cmd != nil {
		t.Fatal("Esc while cycling returned a quit cmd, want nil (no quit)")
	}
	if got.timelineView.cycling {
		t.Fatal("still cycling after Esc, want cycle mode left")
	}
	for i, sl := range got.order.Slots {
		if sl.RowID != before[i].RowID {
			t.Fatalf("order mutated by Esc at slot %d: got %q, want %q", i, sl.RowID, before[i].RowID)
		}
	}
}

// TestFooterHidesMutationKeysInSequencePanel verifies the sequence panel's
// footer advertises no mutation key, while the playback order panel's does.
func TestFooterHidesMutationKeysInSequencePanel(t *testing.T) {
	m := testOrderGestureModel(t)
	m.timelineView.focusPanel = 0

	seqFooter := renderFooter(m)
	for _, key := range []string{"a add", "x del", "J/K reorder", "s change", "l lock", "S shuffle"} {
		if strings.Contains(seqFooter, key) {
			t.Fatalf("sequence-panel footer = %q, should not advertise %q", seqFooter, key)
		}
	}

	m.timelineView.focusPanel = 1
	orderFooter := renderFooter(m)
	if strings.Contains(orderFooter, "commit") {
		t.Fatalf("idle playback-order footer = %q, should not advertise the cycle-mode keys", orderFooter)
	}
	if !strings.Contains(orderFooter, "s change") || !strings.Contains(orderFooter, "l lock") || !strings.Contains(orderFooter, "S shuffle") {
		t.Fatalf("playback-order footer = %q, want the s/l/S gesture keys", orderFooter)
	}

	// While cycling, ←/→ belong to the slot, so the footer must stop calling
	// them view switching and must name both the commit and the undo key.
	m.timelineView.cycling = true
	cycleFooter := renderFooter(m)
	for _, want := range []string{"←/→ change this slot", "commit", "Esc undo"} {
		if !strings.Contains(cycleFooter, want) {
			t.Fatalf("cycling footer = %q, want it to name %q", cycleFooter, want)
		}
	}
	if strings.Contains(cycleFooter, "views") {
		t.Fatalf("cycling footer = %q, still advertises ←/→ as view switching", cycleFooter)
	}
}
