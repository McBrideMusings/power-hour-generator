package csvplan

import "testing"

func idRow(id, label string) CollectionRow {
	r := CollectionRow{CustomFields: map[string]string{"label": label}}
	if id != "" {
		r.RowID = id
		r.CustomFields["id"] = id
	}
	return r
}

// TestAssignRowIDsRepairsDuplicates is the fix for rows that were unreachable
// in the playback order: (collection, id) is a slot's identity, so two rows
// sharing an id collapse into one slot and the extras can never be selected.
// The first occurrence keeps the id, so existing slots and locks stay put.
func TestAssignRowIDsRepairsDuplicates(t *testing.T) {
	rows := []CollectionRow{
		idRow("e314d7", "lasso"),
		idRow("e314d7", "lasso2"),
		idRow("e314d7", "lasso3"),
		idRow("b4edba", "vice1"),
	}

	changed := assignRowIDs(rows)
	if changed != 2 {
		t.Fatalf("changed = %d, want 2 (the two later duplicates)", changed)
	}
	if rows[0].RowID != "e314d7" {
		t.Fatalf("first occurrence = %q, want e314d7 kept", rows[0].RowID)
	}
	if rows[3].RowID != "b4edba" {
		t.Fatalf("untouched row = %q, want b4edba", rows[3].RowID)
	}

	seen := map[string]bool{}
	for i, r := range rows {
		if r.RowID == "" {
			t.Fatalf("row %d has no id", i)
		}
		if seen[r.RowID] {
			t.Fatalf("row %d still shares id %q", i, r.RowID)
		}
		seen[r.RowID] = true
		if r.CustomFields["id"] != r.RowID {
			t.Fatalf("row %d: CustomFields id = %q, want %q — the writers persist this field", i, r.CustomFields["id"], r.RowID)
		}
	}
}

// TestAssignRowIDsLeavesCleanRowsAlone verifies the repair costs nothing when
// there is nothing to repair — a load must not rewrite a healthy plan.
func TestAssignRowIDsLeavesCleanRowsAlone(t *testing.T) {
	rows := []CollectionRow{idRow("aaa111", "a"), idRow("bbb222", "b")}
	if changed := assignRowIDs(rows); changed != 0 {
		t.Fatalf("changed = %d, want 0", changed)
	}
	if rows[0].RowID != "aaa111" || rows[1].RowID != "bbb222" {
		t.Fatal("ids were rewritten on a clean plan")
	}
}

// TestAssignRowIDsFillsBlanksAroundDuplicates verifies both jobs at once: a
// blank id and a duplicate id in the same plan.
func TestAssignRowIDsFillsBlanksAroundDuplicates(t *testing.T) {
	rows := []CollectionRow{idRow("dup", "a"), idRow("", "b"), idRow("dup", "c")}
	if changed := assignRowIDs(rows); changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	seen := map[string]bool{}
	for i, r := range rows {
		if r.RowID == "" || seen[r.RowID] {
			t.Fatalf("row %d id = %q is empty or repeated", i, r.RowID)
		}
		seen[r.RowID] = true
	}
}
