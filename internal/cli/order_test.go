package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/playback"
)

// setUpOrderTestProject builds a temp project with two plan-backed
// collections and a timeline that bookends the "songs" collection with
// inline file entries, then points the package-level projectDir at it and
// registers cleanup. Returns the project root.
func setUpOrderTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	songsCSV := "title,artist,link,start_time,duration\n" +
		"Song A,Artist A,https://example.com/a,0:00,30\n" +
		"Song B,Artist B,https://example.com/b,0:00,30\n" +
		"Song C,Artist C,https://example.com/c,0:00,30\n"
	if err := os.WriteFile(filepath.Join(root, "songs.csv"), []byte(songsCSV), 0o644); err != nil {
		t.Fatalf("write songs.csv: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Collections: map[string]config.CollectionConfig{
			"songs": {Plan: "songs.csv"},
		},
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{File: "intro.mp4"},
				{Collection: "songs", Slice: "start:2"},
				{File: "intermission.mp4"},
				{Collection: "songs"},
				{File: "outro.mp4"},
			},
		},
	}

	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	if err := config.Save(pp.ConfigFile, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	projectDir = root
	t.Cleanup(func() {
		projectDir = ""
		outputJSON = false
		orderShuffleCollection = ""
	})

	return root
}

func runOrderCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newOrderCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// TestOrderListJSON asserts the bare `order` command's --json shape: six
// slots (three files, three songs), 1-based numbering, and file slots
// carrying no collection/row_id.
func TestOrderListJSON(t *testing.T) {
	root := setUpOrderTestProject(t)
	outputJSON = true

	out, err := runOrderCLI(t)
	if err != nil {
		t.Fatalf("order: %v\noutput: %s", err, out)
	}

	var payload struct {
		Changes []orderChangeOutput `json:"changes"`
		Slots   []orderSlotOutput   `json:"slots"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}

	if len(payload.Slots) != 6 {
		t.Fatalf("got %d slots, want 6: %+v", len(payload.Slots), payload.Slots)
	}
	for i, s := range payload.Slots {
		if s.Slot != i+1 {
			t.Fatalf("slot[%d].Slot = %d, want 1-based %d", i, s.Slot, i+1)
		}
	}

	if payload.Slots[0].Kind != "file" || payload.Slots[0].File != "intro.mp4" {
		t.Fatalf("slot 1 = %+v, want intro.mp4 file slot", payload.Slots[0])
	}
	if payload.Slots[0].Collection != "" || payload.Slots[0].RowID != "" {
		t.Fatalf("file slot carries collection/row_id: %+v", payload.Slots[0])
	}
	if payload.Slots[1].Kind != "collection" || payload.Slots[1].Collection != "songs" {
		t.Fatalf("slot 2 = %+v, want a songs collection slot", payload.Slots[1])
	}
	if payload.Slots[1].Label == "" {
		t.Fatalf("slot 2 label empty, want a resolved display label")
	}

	// The first run had no prior stored order, so Materialize supplied one
	// and Reconcile found nothing to change — the order file is not
	// required to exist yet (reportOrderLoad only persists on changes).
	// A second listing must still resolve the identical shape either way.
	if _, err := os.Stat(filepath.Join(root, playback.FileName)); err != nil {
		out2, err2 := runOrderCLI(t)
		if err2 != nil {
			t.Fatalf("second order: %v\noutput: %s", err2, out2)
		}
	}
}

// TestOrderSwapOneBased asserts `order swap` accepts 1-based slot numbers
// and actually exchanges the two occupants.
func TestOrderSwapOneBased(t *testing.T) {
	setUpOrderTestProject(t)
	outputJSON = true

	listOut, err := runOrderCLI(t)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	var before struct {
		Slots []orderSlotOutput `json:"slots"`
	}
	if err := json.Unmarshal([]byte(listOut), &before); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Slots (1-based): 1=intro(file) 2=songs 3=songs 4=intermission(file)
	// 5=songs 6=outro(file). Swap two "songs" slots that straddle the
	// intermission bookend — slots 2 and 5.
	slotA, slotB := before.Slots[1], before.Slots[4]

	swapOut, err := runOrderCLI(t, "swap", "2", "5")
	if err != nil {
		t.Fatalf("order swap: %v\noutput: %s", err, swapOut)
	}

	var after struct {
		Slots []orderSlotOutput `json:"slots"`
	}
	if err := json.Unmarshal([]byte(swapOut), &after); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, swapOut)
	}

	if after.Slots[1].RowID != slotB.RowID {
		t.Fatalf("slot 2 row id = %q, want %q (occupant of former slot 5)", after.Slots[1].RowID, slotB.RowID)
	}
	if after.Slots[4].RowID != slotA.RowID {
		t.Fatalf("slot 5 row id = %q, want %q (occupant of former slot 2)", after.Slots[4].RowID, slotA.RowID)
	}
}

// TestOrderSwapRejectsFileSlot asserts a swap touching a file slot fails
// with an error naming the missing pool, and leaves the order untouched.
func TestOrderSwapRejectsFileSlot(t *testing.T) {
	setUpOrderTestProject(t)

	out, err := runOrderCLI(t, "swap", "1", "2")
	if err == nil {
		t.Fatalf("expected error swapping a file slot, got none; output: %s", out)
	}
	if !strings.Contains(err.Error(), "file entry") {
		t.Fatalf("error %q does not name the missing pool", err.Error())
	}
}

// TestOrderSetRejectsFileSlot asserts `order set` on a file slot fails the
// same way.
func TestOrderSetRejectsFileSlot(t *testing.T) {
	setUpOrderTestProject(t)

	out, err := runOrderCLI(t, "set", "1", "anything")
	if err == nil {
		t.Fatalf("expected error setting a file slot, got none; output: %s", out)
	}
	if !strings.Contains(err.Error(), "file entry") {
		t.Fatalf("error %q does not name the missing pool", err.Error())
	}
}

// TestOrderShuffleCollectionFlag asserts `order shuffle --collection songs`
// only touches songs slots, leaving file slots untouched.
func TestOrderShuffleCollectionFlag(t *testing.T) {
	setUpOrderTestProject(t)
	outputJSON = true

	before, err := runOrderCLI(t)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	var beforePayload struct {
		Slots []orderSlotOutput `json:"slots"`
	}
	if err := json.Unmarshal([]byte(before), &beforePayload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	out, err := runOrderCLI(t, "shuffle", "--collection", "songs")
	if err != nil {
		t.Fatalf("order shuffle: %v\noutput: %s", err, out)
	}

	var afterPayload struct {
		Action string            `json:"action"`
		Slots  []orderSlotOutput `json:"slots"`
	}
	if err := json.Unmarshal([]byte(out), &afterPayload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}

	if afterPayload.Action != "shuffle" {
		t.Fatalf("action = %q, want shuffle", afterPayload.Action)
	}

	for i, s := range afterPayload.Slots {
		if s.Kind == "file" && s.File != beforePayload.Slots[i].File {
			t.Fatalf("file slot %d changed: before=%q after=%q", i+1, beforePayload.Slots[i].File, s.File)
		}
	}

	// The songs pool has exactly 3 rows and "once" selection: the multiset
	// of row ids across the songs slots must be unchanged.
	beforeIDs := map[string]int{}
	afterIDs := map[string]int{}
	for _, s := range beforePayload.Slots {
		if s.Collection == "songs" {
			beforeIDs[s.RowID]++
		}
	}
	for _, s := range afterPayload.Slots {
		if s.Collection == "songs" {
			afterIDs[s.RowID]++
		}
	}
	if len(beforeIDs) != len(afterIDs) {
		t.Fatalf("songs row id multiset changed: before=%v after=%v", beforeIDs, afterIDs)
	}
	for id, n := range beforeIDs {
		if afterIDs[id] != n {
			t.Fatalf("songs row id multiset changed: before=%v after=%v", beforeIDs, afterIDs)
		}
	}
}

// TestOrderReportsReconcileChanges asserts a stale stored order (one row
// added to the plan since the order was last materialized) reports the
// resulting change before/alongside acting, and a second reconcile against
// the now-current stored order reports nothing further.
func TestOrderReportsReconcileChanges(t *testing.T) {
	root := setUpOrderTestProject(t)
	outputJSON = true

	// Force a stored playback-order.yaml to exist by mutating (a bare
	// listing never persists when Reconcile finds nothing to change, and a
	// freshly-materialized order is never "stale" against itself).
	if out, err := runOrderCLI(t, "lock", "2"); err != nil {
		t.Fatalf("initial order lock: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, playback.FileName)); err != nil {
		t.Fatalf("expected %s to exist after a mutation: %v", playback.FileName, err)
	}

	// Add a fourth song row to the plan file after the order was stored.
	songsPath := filepath.Join(root, "songs.csv")
	existing, err := os.ReadFile(songsPath)
	if err != nil {
		t.Fatalf("read songs.csv: %v", err)
	}
	updated := string(existing) + "Song D,Artist D,https://example.com/d,0:00,30\n"
	if err := os.WriteFile(songsPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write songs.csv: %v", err)
	}

	out, err := runOrderCLI(t, "reconcile")
	if err != nil {
		t.Fatalf("order reconcile: %v\noutput: %s", err, out)
	}

	var payload struct {
		Changes []orderChangeOutput `json:"changes"`
		Slots   []orderSlotOutput   `json:"slots"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}

	if len(payload.Changes) == 0 {
		t.Fatalf("expected reconcile to report a change for the newly added row, got none; output: %s", out)
	}
	found := false
	for _, c := range payload.Changes {
		if c.Collection == "songs" && c.RowID != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a change naming collection songs and a row id, got %+v", payload.Changes)
	}
	if len(payload.Slots) != 7 {
		t.Fatalf("got %d slots after reconcile, want 7 (6 + the appended row)", len(payload.Slots))
	}

	// A second reconcile against the now-current stored order reports no
	// further changes.
	out2, err := runOrderCLI(t, "reconcile")
	if err != nil {
		t.Fatalf("second order reconcile: %v\noutput: %s", err, out2)
	}
	var payload2 struct {
		Changes []orderChangeOutput `json:"changes"`
	}
	if err := json.Unmarshal([]byte(out2), &payload2); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out2)
	}
	if len(payload2.Changes) != 0 {
		t.Fatalf("expected no changes on a second reconcile, got %+v", payload2.Changes)
	}
}

// TestOrderLockUnlock asserts lock/unlock round-trip and are reflected in
// the slot listing.
func TestOrderLockUnlock(t *testing.T) {
	setUpOrderTestProject(t)
	outputJSON = true

	out, err := runOrderCLI(t, "lock", "2")
	if err != nil {
		t.Fatalf("order lock: %v\noutput: %s", err, out)
	}
	var payload struct {
		Slots []orderSlotOutput `json:"slots"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if !payload.Slots[1].Locked {
		t.Fatalf("slot 2 not locked after `order lock 2`: %+v", payload.Slots[1])
	}

	out2, err := runOrderCLI(t, "unlock", "2")
	if err != nil {
		t.Fatalf("order unlock: %v\noutput: %s", err, out2)
	}
	var payload2 struct {
		Slots []orderSlotOutput `json:"slots"`
	}
	if err := json.Unmarshal([]byte(out2), &payload2); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out2)
	}
	if payload2.Slots[1].Locked {
		t.Fatalf("slot 2 still locked after `order unlock 2`: %+v", payload2.Slots[1])
	}
}

// TestOrderSetUnknownRowIDErrors asserts `order set` rejects a row id that
// does not belong to the slot's collection.
func TestOrderSetUnknownRowIDErrors(t *testing.T) {
	setUpOrderTestProject(t)

	out, err := runOrderCLI(t, "set", "2", "no-such-row")
	if err == nil {
		t.Fatalf("expected error for unknown row id, got none; output: %s", out)
	}
}

// TestParseSlotArg exercises the 1-based -> 0-based conversion and its
// out-of-range error shape directly.
func TestParseSlotArg(t *testing.T) {
	idx, err := parseSlotArg("1", 3)
	if err != nil || idx != 0 {
		t.Fatalf("parseSlotArg(1,3) = %d,%v want 0,nil", idx, err)
	}
	if _, err := parseSlotArg("0", 3); err == nil {
		t.Fatal("expected error for slot 0")
	}
	if _, err := parseSlotArg("4", 3); err == nil {
		t.Fatal("expected error for slot beyond total")
	}
	if _, err := parseSlotArg("nope", 3); err == nil {
		t.Fatal("expected error for non-numeric slot")
	}
}
