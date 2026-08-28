package render

import (
	"testing"

	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

// TestSegmentInputHashIgnoresLabel guards the ph-25-back-to-club migration:
// adding a presentational "label" column to an existing collection's plan
// must not change SegmentInputHash for any row, or every already-rendered
// segment would re-encode.
func TestSegmentInputHashIgnoresLabel(t *testing.T) {
	base := func(label string) Segment {
		return Segment{
			Clip: project.Clip{
				Row: csvplan.Row{
					Link:  "https://youtu.be/example",
					Title: "Song",
					CustomFields: map[string]string{
						"title": "Song",
						"label": label,
					},
				},
				DurationSeconds: 30,
			},
		}
	}

	withoutLabel := SegmentInputHash(base(""), "$INDEX_$SAFE_TITLE")
	withLabel := SegmentInputHash(base("Some Label"), "$INDEX_$SAFE_TITLE")
	changedLabel := SegmentInputHash(base("A Different Label"), "$INDEX_$SAFE_TITLE")

	if withoutLabel != withLabel {
		t.Fatalf("expected hash to ignore label field: %q != %q", withoutLabel, withLabel)
	}
	if withLabel != changedLabel {
		t.Fatalf("expected hash to ignore label field changes: %q != %q", withLabel, changedLabel)
	}

	other := base("Some Label")
	other.Clip.Row.CustomFields["title"] = "Different Song"
	changedOther := SegmentInputHash(other, "$INDEX_$SAFE_TITLE")
	if changedOther == withLabel {
		t.Fatalf("expected hash to change when a non-label custom field changes")
	}
}
