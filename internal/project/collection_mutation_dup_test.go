package project

import (
	"testing"

	"powerhour/pkg/csvplan"
)

// TestDuplicateCollectionRowDropsTheID verifies a duplicated row does not
// inherit its source's identity. RowID was already left empty, but the id
// also lives in CustomFields, which the writers persist — so carrying it over
// put the duplicate back on the source's slot on the next load.
func TestDuplicateCollectionRowDropsTheID(t *testing.T) {
	coll := Collection{Rows: []csvplan.CollectionRow{{
		Link:  "/media/lasso.mkv",
		RowID: "e314d7",
		CustomFields: map[string]string{
			"id":    "e314d7",
			"label": "lasso",
		},
	}}}

	got := DuplicateCollectionRow(coll, 0)
	if len(got.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(got.Rows))
	}
	dup := got.Rows[1]
	if dup.RowID != "" {
		t.Fatalf("duplicate RowID = %q, want empty so the loader assigns a fresh one", dup.RowID)
	}
	if id, ok := dup.CustomFields["id"]; ok {
		t.Fatalf("duplicate carries CustomFields[id] = %q; the writers would persist the source's identity", id)
	}
	if dup.CustomFields["label"] != "lasso" {
		t.Fatal("duplicate lost its content fields")
	}
	if got.Rows[0].CustomFields["id"] != "e314d7" {
		t.Fatal("the source row's id was disturbed")
	}
}
