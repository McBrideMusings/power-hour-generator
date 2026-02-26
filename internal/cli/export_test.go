package cli

import (
	"encoding/json"
	"testing"
)

func TestExportOutputStructure(t *testing.T) {
	output := exportOutput{
		Project: "/test/project",
		Config:  exportConfig{},
		Collections: map[string]exportCollection{
			"songs": {
				Rows: []exportRow{
					{Index: 1, Title: "Song A", Artist: "Artist A", Link: "https://example.com"},
				},
			},
		},
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed["project"] != "/test/project" {
		t.Errorf("project = %v, want /test/project", parsed["project"])
	}

	collections, ok := parsed["collections"].(map[string]interface{})
	if !ok {
		t.Fatal("expected collections map")
	}
	if _, ok := collections["songs"]; !ok {
		t.Fatal("expected songs collection")
	}
}

func TestExportWithTimeline(t *testing.T) {
	output := exportOutput{
		Project:     "/test/project",
		Config:      exportConfig{},
		Collections: map[string]exportCollection{},
		Timeline: []exportTimelineEntry{
			{Sequence: 1, Collection: "songs", Index: 1, Title: "Song A", Artist: "Artist A"},
			{Sequence: 2, Collection: "songs", Index: 2, Title: "Song B"},
		},
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	timeline, ok := parsed["timeline"].([]interface{})
	if !ok {
		t.Fatal("expected timeline array")
	}
	if len(timeline) != 2 {
		t.Fatalf("got %d timeline entries, want 2", len(timeline))
	}
}

func TestExportOmitsEmptyTimeline(t *testing.T) {
	output := exportOutput{
		Project:     "/test",
		Config:      exportConfig{},
		Collections: map[string]exportCollection{},
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if _, ok := parsed["timeline"]; ok {
		t.Error("timeline should be omitted when empty")
	}
}
