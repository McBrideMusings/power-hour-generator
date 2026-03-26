package render

import (
	"strings"
	"testing"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

func TestPresetSongInfoDefaults(t *testing.T) {
	row := csvplan.Row{
		Index:  1,
		Title:  "Test Song",
		Artist: "Test Artist",
	}
	filters := presetSongInfo(nil, row, 60)

	if len(filters) != 4 {
		t.Fatalf("expected 4 filters (title, artist, number outline, number fill), got %d", len(filters))
	}

	// Title filter
	if !strings.Contains(filters[0], "text='Test Song'") {
		t.Errorf("title filter missing text: %s", filters[0])
	}
	if !strings.Contains(filters[0], "fontfile=") {
		t.Errorf("title filter missing fontfile: %s", filters[0])
	}
	if !strings.Contains(filters[0], "fontsize=64") {
		t.Errorf("title filter missing fontsize: %s", filters[0])
	}

	// Artist filter should be uppercased
	if !strings.Contains(filters[1], "text='TEST ARTIST'") {
		t.Errorf("artist filter missing uppercased text: %s", filters[1])
	}
	if !strings.Contains(filters[1], "fontsize=32") {
		t.Errorf("artist filter missing fontsize: %s", filters[1])
	}

	// Number outline layer
	if !strings.Contains(filters[2], "text='1'") {
		t.Errorf("number outline filter missing text: %s", filters[2])
	}
	if !strings.Contains(filters[2], "fontfile=") {
		t.Errorf("number outline filter missing fontfile: %s", filters[2])
	}

	// Number fill layer
	if !strings.Contains(filters[3], "text='1'") {
		t.Errorf("number fill filter missing text: %s", filters[3])
	}
	if !strings.Contains(filters[3], "fontsize=140") {
		t.Errorf("number fill filter missing fontsize: %s", filters[3])
	}
}

func TestPresetSongInfoOverrides(t *testing.T) {
	row := csvplan.Row{
		Index:  5,
		Title:  "Custom",
		Artist: "Band",
	}
	opts := map[string]string{
		"font":        "Arial",
		"title_size":  "48",
		"artist_size": "28",
		"show_number": "false",
	}
	filters := presetSongInfo(opts, row, 60)

	if len(filters) != 2 {
		t.Fatalf("expected 2 filters (no number), got %d", len(filters))
	}
	if !strings.Contains(filters[0], "fontfile=") {
		t.Errorf("expected fontfile in filter: %s", filters[0])
	}
	if !strings.Contains(filters[0], "fontsize=48") {
		t.Errorf("expected custom title size: %s", filters[0])
	}
	if !strings.Contains(filters[1], "fontsize=28") {
		t.Errorf("expected custom artist size: %s", filters[1])
	}
}

func TestPresetDrinkDefaults(t *testing.T) {
	row := csvplan.Row{Index: 1}
	filters := presetDrink(nil, row, 60)

	if len(filters) != 2 {
		t.Fatalf("expected 2 filters (shadow + text), got %d", len(filters))
	}

	// Shadow layer should be yellow
	if !strings.Contains(filters[0], "fontcolor=yellow") {
		t.Errorf("shadow filter missing yellow color: %s", filters[0])
	}
	if !strings.Contains(filters[0], "text='Drink!'") {
		t.Errorf("shadow filter missing text: %s", filters[0])
	}

	// Text layer should be white with black outline
	if !strings.Contains(filters[1], "fontcolor=white") {
		t.Errorf("text filter missing white color: %s", filters[1])
	}
	if !strings.Contains(filters[1], "fontfile=") {
		t.Errorf("text filter missing fontfile: %s", filters[1])
	}
}

func TestPresetDrinkCustomText(t *testing.T) {
	row := csvplan.Row{Index: 1}
	opts := map[string]string{
		"text": "Sip!",
		"size": "80",
	}
	filters := presetDrink(opts, row, 60)

	if len(filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(filters))
	}
	if !strings.Contains(filters[0], "text='Sip!'") {
		t.Errorf("expected custom text: %s", filters[0])
	}
	if !strings.Contains(filters[0], "fontsize=80") {
		t.Errorf("expected custom size: %s", filters[0])
	}
}

func TestExpandOverlaysNone(t *testing.T) {
	overlays := []config.OverlayEntry{{Type: "none"}}
	row := csvplan.Row{Index: 1}
	filters := ExpandOverlays(overlays, row, 60)

	if len(filters) != 0 {
		t.Fatalf("expected 0 filters for 'none', got %d", len(filters))
	}
}

func TestExpandOverlaysCustom(t *testing.T) {
	overlays := []config.OverlayEntry{{
		Type:    "custom",
		Filters: []string{"drawtext=text='{title}':fontsize=64"},
	}}
	row := csvplan.Row{
		Index: 1,
		Title: "Hello",
	}
	filters := ExpandOverlays(overlays, row, 60)

	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if !strings.Contains(filters[0], "text='Hello'") {
		t.Errorf("expected token expansion: %s", filters[0])
	}
}

func TestExpandOverlaysMultiple(t *testing.T) {
	overlays := []config.OverlayEntry{
		{Type: "song-info"},
		{Type: "drink"},
	}
	row := csvplan.Row{
		Index:  1,
		Title:  "Song",
		Artist: "Artist",
	}
	filters := ExpandOverlays(overlays, row, 60)

	// song-info: 4 (title + artist + number outline + number fill, no name field) + drink: 2 (shadow + text) = 6
	if len(filters) != 6 {
		t.Fatalf("expected 6 filters, got %d", len(filters))
	}
}

func TestLookupPreset(t *testing.T) {
	for _, name := range []string{"song-info", "drink", "custom", "none"} {
		_, ok := LookupPreset(name)
		if !ok {
			t.Errorf("expected preset %q to be registered", name)
		}
	}

	_, ok := LookupPreset("unknown")
	if ok {
		t.Error("expected unknown preset to not be found")
	}
}

func TestOptHelpers(t *testing.T) {
	opts := map[string]string{
		"str":   "hello",
		"num":   "42",
		"float": "3.14",
		"bool":  "true",
	}

	if v := optStr(opts, "str", "default"); v != "hello" {
		t.Errorf("optStr = %q, want hello", v)
	}
	if v := optStr(opts, "missing", "default"); v != "default" {
		t.Errorf("optStr fallback = %q, want default", v)
	}
	if v := optInt(opts, "num", 0); v != 42 {
		t.Errorf("optInt = %d, want 42", v)
	}
	if v := optInt(opts, "missing", 10); v != 10 {
		t.Errorf("optInt fallback = %d, want 10", v)
	}
	if v := optFloat(opts, "float", 0); v != 3.14 {
		t.Errorf("optFloat = %f, want 3.14", v)
	}
	if v := optBool(opts, "bool", false); !v {
		t.Error("optBool = false, want true")
	}
	if v := optBool(opts, "missing", true); !v {
		t.Error("optBool fallback = false, want true")
	}
}
