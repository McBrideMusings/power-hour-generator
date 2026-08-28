package project

import (
	"testing"
	"time"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

func TestDuplicateCollectionRowAppendsDeepCopy(t *testing.T) {
	coll := Collection{
		Headers: []string{"title", "artist", "link", "start_time", "duration"},
		Rows: []csvplan.CollectionRow{
			{
				Index:           1,
				Link:            "https://example.com/1",
				StartRaw:        "0:15",
				DurationSeconds: 60,
				CustomFields: map[string]string{
					"title":      "First",
					"artist":     "Artist",
					"link":       "https://example.com/1",
					"start_time": "0:15",
					"duration":   "60",
				},
			},
		},
	}

	dup := DuplicateCollectionRow(coll, 0)

	if len(dup.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(dup.Rows))
	}
	if dup.Rows[1].Index != 2 {
		t.Fatalf("duplicated row index = %d, want 2", dup.Rows[1].Index)
	}
	if dup.Rows[1].CustomFields["title"] != "First" {
		t.Fatalf("duplicated title = %q, want %q", dup.Rows[1].CustomFields["title"], "First")
	}

	dup.Rows[1].CustomFields["title"] = "Changed"
	if coll.Rows[0].CustomFields["title"] != "First" {
		t.Fatalf("original row title changed to %q", coll.Rows[0].CustomFields["title"])
	}
}

func TestDedupeImportedRowsSkipsExistingYouTubeLinks(t *testing.T) {
	coll := Collection{
		Rows: []csvplan.CollectionRow{
			{Link: "https://www.youtube.com/watch?v=abc123&si=tracking"},
			{Link: "https://youtu.be/def456"},
		},
	}
	imported := []csvplan.CollectionRow{
		{Link: "https://youtu.be/abc123?si=other-tracking"}, // same video as row 1, different URL shape
		{Link: "https://www.youtube.com/watch?v=def456"},    // same video as row 2
		{Link: "https://www.youtube.com/watch?v=new789"},    // genuinely new
		{Link: "https://www.youtube.com/watch?v=new789"},    // duplicated within the pasted batch itself
	}

	deduped, skipped := DedupeImportedRows(coll, imported)

	if len(deduped) != 1 {
		t.Fatalf("deduped rows = %d, want 1", len(deduped))
	}
	if deduped[0].Link != "https://www.youtube.com/watch?v=new789" {
		t.Fatalf("deduped row link = %q, want new789", deduped[0].Link)
	}
	if skipped != 3 {
		t.Fatalf("skipped = %d, want 3", skipped)
	}
}

func TestDedupeImportedRowsFallsBackToExactLinkForNonYouTube(t *testing.T) {
	coll := Collection{
		Rows: []csvplan.CollectionRow{
			{Link: "/local/path/song.mp4"},
		},
	}
	imported := []csvplan.CollectionRow{
		{Link: "/local/path/song.mp4"},       // exact duplicate
		{Link: "/local/path/other-song.mp4"}, // new
	}

	deduped, skipped := DedupeImportedRows(coll, imported)

	if len(deduped) != 1 || deduped[0].Link != "/local/path/other-song.mp4" {
		t.Fatalf("deduped = %+v, want only other-song.mp4", deduped)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
}

func TestBuildCollectionRowUsesSchemaDefaults(t *testing.T) {
	coll := Collection{
		Config: config.CollectionConfig{
			LinkHeader:     "link",
			StartHeader:    "start_time",
			DurationHeader: "duration",
		},
		Defaults: map[string]string{
			"start_time": "0:00",
			"duration":   "5",
		},
	}

	row := BuildCollectionRow(coll, "https://example.com/clip")

	if row.StartRaw != "0:00" {
		t.Fatalf("StartRaw = %q, want 0:00", row.StartRaw)
	}
	if row.DurationSeconds != 5 {
		t.Fatalf("DurationSeconds = %d, want 5", row.DurationSeconds)
	}
	if row.CustomFields["duration"] != "5" {
		t.Fatalf("duration field = %q, want 5", row.CustomFields["duration"])
	}
}

func TestBuildCollectionRowParsesDefaultStartTime(t *testing.T) {
	coll := Collection{
		Config: config.CollectionConfig{
			LinkHeader:     "link",
			StartHeader:    "start_time",
			DurationHeader: "duration",
		},
		Defaults: map[string]string{
			"start_time": "1:15",
			"duration":   "5",
		},
	}

	row := BuildCollectionRow(coll, "https://example.com/clip")

	if row.Start != 75*time.Second {
		t.Fatalf("Start = %v, want %v", row.Start, 75*time.Second)
	}
}
