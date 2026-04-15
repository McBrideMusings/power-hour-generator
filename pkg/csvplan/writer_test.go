package csvplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteCSV_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "songs.csv")

	// Write initial CSV.
	headers := []string{"title", "artist", "link", "start_time", "duration"}
	rows := []CollectionRow{
		{
			Index: 1, Link: "https://youtube.com/watch?v=abc",
			StartRaw: "1:30", Start: 90 * time.Second, DurationSeconds: 60,
			CustomFields: map[string]string{
				"title": "Song One", "artist": "Artist A",
				"link": "https://youtube.com/watch?v=abc", "start_time": "1:30", "duration": "60",
			},
		},
		{
			Index: 2, Link: "https://youtube.com/watch?v=def",
			StartRaw: "0:45", Start: 45 * time.Second, DurationSeconds: 60,
			CustomFields: map[string]string{
				"title": "Song Two", "artist": "Artist B",
				"link": "https://youtube.com/watch?v=def", "start_time": "0:45", "duration": "60",
			},
		},
	}

	if err := WriteCSV(path, headers, rows, ','); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	// Read back and verify.
	loaded, err := LoadCollection(path, CollectionOptions{DefaultDuration: 60})
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(loaded))
	}

	if loaded[0].CustomFields["title"] != "Song One" {
		t.Errorf("row 1 title = %q, want %q", loaded[0].CustomFields["title"], "Song One")
	}
	if loaded[1].CustomFields["artist"] != "Artist B" {
		t.Errorf("row 2 artist = %q, want %q", loaded[1].CustomFields["artist"], "Artist B")
	}
	if loaded[0].Link != "https://youtube.com/watch?v=abc" {
		t.Errorf("row 1 link = %q", loaded[0].Link)
	}
}

func TestWriteCSV_TabDelimiter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "songs.tsv")

	headers := []string{"title", "artist", "link", "start_time"}
	rows := []CollectionRow{
		{
			Index: 1, Link: "https://example.com/v1",
			StartRaw: "0:00", DurationSeconds: 60,
			CustomFields: map[string]string{
				"title": "Tab Song", "artist": "Tab Artist",
				"link": "https://example.com/v1", "start_time": "0:00",
			},
		},
	}

	if err := WriteCSV(path, headers, rows, '\t'); err != nil {
		t.Fatalf("WriteCSV tab: %v", err)
	}

	// Verify delimiter preserved by reading raw file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Should contain tabs, not commas (in header line).
	content := string(data)
	if content[0:5] != "title" {
		t.Fatalf("unexpected start: %q", content[:20])
	}

	// Verify round-trip.
	loaded, err := LoadCollection(path, CollectionOptions{DefaultDuration: 60})
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 row, got %d", len(loaded))
	}
	if loaded[0].CustomFields["title"] != "Tab Song" {
		t.Errorf("title = %q", loaded[0].CustomFields["title"])
	}

	// Verify ReadHeaders returns tab delimiter.
	hdrs, delim, err := ReadHeaders(path)
	if err != nil {
		t.Fatalf("ReadHeaders: %v", err)
	}
	if delim != '\t' {
		t.Errorf("delimiter = %q, want tab", delim)
	}
	if len(hdrs) != 4 {
		t.Errorf("headers count = %d, want 4", len(hdrs))
	}
}

func TestLoadCollectionAllowsDotSeparatedStartTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "songs.csv")
	content := "title,artist,link,start_time,duration\nSong,Band,https://example.com,0.35,60\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rows, err := LoadCollection(path, CollectionOptions{DefaultDuration: 60})
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Start != 35*time.Second {
		t.Fatalf("unexpected start duration: got %v want %v", rows[0].Start, 35*time.Second)
	}
}

func TestWriteYAML_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "songs.yaml")

	columns := []string{"title", "artist", "link", "start_time", "duration"}
	rows := []CollectionRow{
		{
			Index: 1, Link: "https://youtube.com/watch?v=abc",
			StartRaw: "2:00", Start: 120 * time.Second, DurationSeconds: 60,
			CustomFields: map[string]string{
				"title": "YAML Song", "artist": "YAML Artist",
				"link": "https://youtube.com/watch?v=abc", "start_time": "2:00", "duration": "60",
			},
		},
		{
			Index: 2, Link: "https://youtube.com/watch?v=def",
			StartRaw: "0:30", Start: 30 * time.Second, DurationSeconds: 45,
			CustomFields: map[string]string{
				"title": "Another", "artist": "Band",
				"link": "https://youtube.com/watch?v=def", "start_time": "0:30", "duration": "45",
			},
		},
	}

	defaults := map[string]string{"start_time": "0:00", "duration": "60"}
	if err := WriteYAML(path, columns, defaults, rows); err != nil {
		t.Fatalf("WriteYAML: %v", err)
	}

	result, err := LoadCollectionYAML(path, CollectionOptions{DefaultDuration: 60})
	if err != nil {
		t.Fatalf("LoadCollectionYAML: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}

	if result.Rows[0].CustomFields["title"] != "YAML Song" {
		t.Errorf("row 1 title = %q, want %q", result.Rows[0].CustomFields["title"], "YAML Song")
	}
	if result.Rows[1].Link != "https://youtube.com/watch?v=def" {
		t.Errorf("row 2 link = %q", result.Rows[1].Link)
	}
	if result.Rows[1].DurationSeconds != 45 {
		t.Errorf("row 2 duration = %d, want 45", result.Rows[1].DurationSeconds)
	}

	// Verify columns are preserved.
	if len(result.Columns) != len(columns) {
		t.Fatalf("expected %d columns, got %d", len(columns), len(result.Columns))
	}
	for i, col := range result.Columns {
		if col != columns[i] {
			t.Errorf("column[%d] = %q, want %q", i, col, columns[i])
		}
	}
	if result.Defaults["duration"] != "60" {
		t.Fatalf("default duration = %q, want 60", result.Defaults["duration"])
	}
}

func TestWriteYAML_EmptyRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "songs.yaml")

	columns := []string{"title", "artist", "start_time", "duration", "link"}

	if err := WriteYAML(path, columns, map[string]string{"start_time": "0:00"}, nil); err != nil {
		t.Fatalf("WriteYAML: %v", err)
	}

	result, err := LoadCollectionYAML(path, CollectionOptions{DefaultDuration: 60})
	if err != nil {
		t.Fatalf("LoadCollectionYAML: %v", err)
	}

	if len(result.Rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(result.Rows))
	}

	if len(result.Columns) != len(columns) {
		t.Fatalf("expected %d columns, got %d", len(columns), len(result.Columns))
	}
	for i, col := range result.Columns {
		if col != columns[i] {
			t.Errorf("column[%d] = %q, want %q", i, col, columns[i])
		}
	}
	if result.Defaults["start_time"] != "0:00" {
		t.Fatalf("default start_time = %q, want 0:00", result.Defaults["start_time"])
	}
}

func TestLoadCollectionYAML_AppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interstitials.yaml")

	raw := `columns: [link, start_time, duration]
defaults:
  start_time: "0:00"
  duration: "5"
rows:
  - link: https://example.com/interstitial
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := LoadCollectionYAML(path, CollectionOptions{DefaultDuration: 60})
	if err != nil {
		t.Fatalf("LoadCollectionYAML: %v", err)
	}

	if got := result.Rows[0].StartRaw; got != "0:00" {
		t.Fatalf("start_time = %q, want 0:00", got)
	}
	if got := result.Rows[0].DurationSeconds; got != 5 {
		t.Fatalf("duration = %d, want 5", got)
	}
	if got := result.Rows[0].CustomFields["duration"]; got != "5" {
		t.Fatalf("duration field = %q, want 5", got)
	}
}

func TestWriteYAML_OmitsValuesMatchingDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interstitials.yaml")

	rows := []CollectionRow{{
		Index:           1,
		Link:            "https://example.com/interstitial",
		StartRaw:        "0:00",
		DurationSeconds: 5,
		CustomFields: map[string]string{
			"link":       "https://example.com/interstitial",
			"start_time": "0:00",
			"duration":   "5",
		},
	}}

	if err := WriteYAML(path, []string{"link", "start_time", "duration"}, map[string]string{"start_time": "0:00", "duration": "5"}, rows); err != nil {
		t.Fatalf("WriteYAML: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	if count := strings.Count(text, "start_time: \"0:00\""); count != 1 {
		t.Fatalf("expected 1 start_time default entry, got %d\n%s", count, text)
	}
	if count := strings.Count(text, "duration: \"5\""); count != 1 {
		t.Fatalf("expected 1 duration default entry, got %d\n%s", count, text)
	}
	if !strings.Contains(text, "- link: https://example.com/interstitial") {
		t.Fatalf("expected row link in yaml, got:\n%s", text)
	}
	if strings.Contains(text, "rows:\n    - duration") || strings.Contains(text, "rows:\n    - start_time") {
		t.Fatalf("expected row values matching defaults to be omitted, got:\n%s", text)
	}
}

func TestWriteCSV_SpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "special.csv")

	headers := []string{"title", "artist", "link", "start_time"}
	rows := []CollectionRow{
		{
			Index: 1, Link: "https://example.com",
			StartRaw: "0:00", DurationSeconds: 60,
			CustomFields: map[string]string{
				"title":  `Song with "quotes" and, commas`,
				"artist": "O'Brien",
				"link":   "https://example.com", "start_time": "0:00",
			},
		},
	}

	if err := WriteCSV(path, headers, rows, ','); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	loaded, err := LoadCollection(path, CollectionOptions{DefaultDuration: 60})
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}

	if loaded[0].CustomFields["title"] != `Song with "quotes" and, commas` {
		t.Errorf("title = %q", loaded[0].CustomFields["title"])
	}
	if loaded[0].CustomFields["artist"] != "O'Brien" {
		t.Errorf("artist = %q", loaded[0].CustomFields["artist"])
	}
}

func TestReadHeaders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.csv")

	content := "Title,Artist,Link,Start_Time,Duration\nSong,Band,url,0:00,60\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	headers, delim, err := ReadHeaders(path)
	if err != nil {
		t.Fatalf("ReadHeaders: %v", err)
	}

	if delim != ',' {
		t.Errorf("delimiter = %q, want ','", delim)
	}

	expected := []string{"title", "artist", "link", "start_time", "duration"}
	if len(headers) != len(expected) {
		t.Fatalf("headers count = %d, want %d", len(headers), len(expected))
	}
	for i, h := range headers {
		if h != expected[i] {
			t.Errorf("header[%d] = %q, want %q", i, h, expected[i])
		}
	}
}
