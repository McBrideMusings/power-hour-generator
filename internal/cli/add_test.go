package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"powerhour/pkg/csvplan"
)

func writeTestProjectFiles(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "powerhour.yaml"), []byte(renderDefaultConfigYAML("yaml")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "songs.yaml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "interstitials.yaml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestProjectFilesWithStructuredYAML(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "powerhour.yaml"), []byte(renderDefaultConfigYAML("yaml")), 0o644); err != nil {
		t.Fatal(err)
	}
	// Structured YAML format with custom column "genre"
	structuredSongsYAML := `columns: [title, artist, name, start_time, duration, link, genre]
defaults:
    start_time: "0:00"
    duration: "60"
rows: []
`
	if err := os.WriteFile(filepath.Join(dir, "songs.yaml"), []byte(structuredSongsYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "interstitials.yaml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddCommandAppendsRowsToYAMLCollectionFromFile(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	outputJSON = false
	t.Cleanup(func() {
		projectDir = ""
		outputJSON = false
	})

	writeTestProjectFiles(t, dir)

	importPath := filepath.Join(dir, "import.csv")
	importBody := "title,artist,start_time,duration,link\nSong,Artist,0:10,62,https://example.com\n"
	if err := os.WriteFile(importPath, []byte(importBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--collection", "songs", "--file", importPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "songs.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	for _, want := range []string{
		"title: Song",
		"artist: Artist",
		"start_time: \"0:10\"",
		"duration: \"62\"",
		"link: https://example.com",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("songs.yaml missing %q\n%s", want, content)
		}
	}
}

func TestAddCommandAddsSingleURLArgument(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	outputJSON = false
	t.Cleanup(func() {
		projectDir = ""
		outputJSON = false
	})

	writeTestProjectFiles(t, dir)

	cmd := newAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--collection", "songs", "https://www.youtube.com/watch?v=abc123&list=playlist"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "songs.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "link: https://www.youtube.com/watch?v=abc123") {
		t.Fatalf("songs.yaml missing cleaned link\n%s", content)
	}
	if !strings.Contains(content, "start_time: \"0:00\"") {
		t.Fatalf("songs.yaml missing default start_time\n%s", content)
	}
}

func TestAddCommandReadsSingleURLFromStdinWithTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	outputJSON = false
	t.Cleanup(func() {
		projectDir = ""
		outputJSON = false
	})

	writeTestProjectFiles(t, dir)

	cmd := newAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("https://youtu.be/abc123?si=test\n"))
	cmd.SetArgs([]string{"--collection", "songs"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "songs.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "link: https://youtu.be/abc123") {
		t.Fatalf("songs.yaml missing cleaned stdin link\n%s", content)
	}
	if !strings.Contains(content, "start_time: \"0:00\"") {
		t.Fatalf("songs.yaml missing default start_time from stdin add\n%s", content)
	}
}

func TestAddCommandPreservesStructuredYAMLColumnsOnRoundTrip(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	outputJSON = false
	t.Cleanup(func() {
		projectDir = ""
		outputJSON = false
	})

	writeTestProjectFilesWithStructuredYAML(t, dir)

	// First add: append a row via CSV import
	importPath := filepath.Join(dir, "import.csv")
	importBody := "title,artist,start_time,duration,link,genre\nSong One,Artist One,0:10,62,https://example.com/1,Rock\n"
	if err := os.WriteFile(importPath, []byte(importBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--collection", "songs", "--file", importPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("First add Execute: %v", err)
	}

	// Reload the file with LoadCollectionYAML to verify structured format is preserved
	opts := csvplan.CollectionOptions{
		LinkHeader:      "link",
		StartHeader:     "start_time",
		DurationHeader:  "duration",
		DefaultDuration: 60,
	}
	result, err := csvplan.LoadCollectionYAML(filepath.Join(dir, "songs.yaml"), opts)
	if err != nil {
		t.Fatalf("LoadCollectionYAML after first add: %v", err)
	}

	// Verify columns include the custom "genre" column
	expectedColumns := []string{"title", "artist", "name", "start_time", "duration", "link", "genre"}
	if len(result.Columns) != len(expectedColumns) {
		t.Fatalf("Expected %d columns, got %d: %v", len(expectedColumns), len(result.Columns), result.Columns)
	}
	for i, col := range expectedColumns {
		if i >= len(result.Columns) || result.Columns[i] != col {
			t.Fatalf("Column mismatch at position %d: expected %q, got %v", i, col, result.Columns)
		}
	}

	// Verify the row was added correctly
	if len(result.Rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(result.Rows))
	}
	row := result.Rows[0]
	if row.Link != "https://example.com/1" {
		t.Fatalf("Expected link https://example.com/1, got %q", row.Link)
	}
	if row.CustomFields["title"] != "Song One" {
		t.Fatalf("Expected title 'Song One', got %q", row.CustomFields["title"])
	}
	if row.CustomFields["genre"] != "Rock" {
		t.Fatalf("Expected genre 'Rock', got %q", row.CustomFields["genre"])
	}

	// Second add: append another row via CSV import with different genre
	importPath2 := filepath.Join(dir, "import2.csv")
	importBody2 := "title,artist,start_time,duration,link,genre\nSong Two,Artist Two,0:20,45,https://example.com/2,Jazz\n"
	if err := os.WriteFile(importPath2, []byte(importBody2), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd2 := newAddCmd()
	cmd2.SetOut(&out)
	cmd2.SetErr(&out)
	cmd2.SetArgs([]string{"--collection", "songs", "--file", importPath2})

	if err := cmd2.Execute(); err != nil {
		t.Fatalf("Second add Execute: %v", err)
	}

	// Reload again and verify both rows and columns are preserved
	result2, err := csvplan.LoadCollectionYAML(filepath.Join(dir, "songs.yaml"), opts)
	if err != nil {
		t.Fatalf("LoadCollectionYAML after second add: %v", err)
	}

	// Verify columns still include the custom "genre" column
	if len(result2.Columns) != len(expectedColumns) {
		t.Fatalf("After second add: expected %d columns, got %d: %v", len(expectedColumns), len(result2.Columns), result2.Columns)
	}
	for i, col := range expectedColumns {
		if i >= len(result2.Columns) || result2.Columns[i] != col {
			t.Fatalf("After second add: column mismatch at position %d: expected %q, got %v", i, col, result2.Columns)
		}
	}

	// Verify both rows are present with correct genre values
	if len(result2.Rows) != 2 {
		t.Fatalf("After second add: expected 2 rows, got %d", len(result2.Rows))
	}
	if result2.Rows[0].CustomFields["genre"] != "Rock" {
		t.Fatalf("After second add: row 0 genre mismatch: expected 'Rock', got %q", result2.Rows[0].CustomFields["genre"])
	}
	if result2.Rows[1].CustomFields["genre"] != "Jazz" {
		t.Fatalf("After second add: row 1 genre mismatch: expected 'Jazz', got %q", result2.Rows[1].CustomFields["genre"])
	}

	// Verify the defaults are still present
	if result2.Defaults == nil {
		t.Fatalf("Expected defaults to be preserved, got nil")
	}
	if result2.Defaults["start_time"] != "0:00" {
		t.Fatalf("Expected default start_time '0:00', got %q", result2.Defaults["start_time"])
	}
	if result2.Defaults["duration"] != "60" {
		t.Fatalf("Expected default duration '60', got %q", result2.Defaults["duration"])
	}
}
