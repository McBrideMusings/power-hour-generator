package csvplan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadCSVValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "powerhour.csv")
	data := "title,artist,start_time,duration,name,link\n" +
		"Song Title,Artist Name,1:23,60,Friend,https://example.com\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rows, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	if row.Index != 1 {
		t.Errorf("expected index 1, got %d", row.Index)
	}
	if row.Title != "Song Title" {
		t.Errorf("unexpected title: %q", row.Title)
	}
	if row.Artist != "Artist Name" {
		t.Errorf("unexpected artist: %q", row.Artist)
	}
	if row.StartRaw != "1:23" {
		t.Errorf("unexpected raw start: %q", row.StartRaw)
	}
	wantStart := time.Minute + 23*time.Second
	if row.Start != wantStart {
		t.Errorf("unexpected start duration: got %v want %v", row.Start, wantStart)
	}
	if row.DurationSeconds != 60 {
		t.Errorf("unexpected duration: got %d", row.DurationSeconds)
	}
	if row.Name != "Friend" {
		t.Errorf("unexpected name: %q", row.Name)
	}
	if row.Link != "https://example.com" {
		t.Errorf("unexpected link: %q", row.Link)
	}
}

func TestLoadTSVUnicode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "powerhour.tsv")
	data := "title\tartist\tstart_time\tduration\tname\tlink\n" +
		"Señorita\tBeyoncé✨\t0:05.250\t45\t\thttps://example.com/video\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rows, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	if row.Title != "Señorita" {
		t.Errorf("expected unicode title preserved, got %q", row.Title)
	}
	if row.Artist != "Beyoncé✨" {
		t.Errorf("expected unicode artist preserved, got %q", row.Artist)
	}
	if row.Start != 5*time.Second+250*time.Millisecond {
		t.Errorf("unexpected start duration: %v", row.Start)
	}
}

func TestLoadAggregatesErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "powerhour.csv")
	data := "title,artist,start_time,duration,name,link\n" +
		"\tArtist,1:70,0,,https://example.com\n" +
		"Valid Title,Valid Artist,0:10,abc,,\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rows, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}

	vErrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows despite validation issues, got %d", len(rows))
	}

	if len(vErrs) < 3 {
		t.Fatalf("expected multiple validation errors, got %d", len(vErrs))
	}

	// Ensure row numbers are reported.
	for _, issue := range vErrs {
		if issue.Line < 2 {
			t.Fatalf("expected data row line number >= 2, got %d", issue.Line)
		}
		if issue.Message == "" {
			t.Fatalf("missing error message for field %s", issue.Field)
		}
	}
}
