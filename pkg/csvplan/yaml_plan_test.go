package csvplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadCollectionYAML_RowsWithoutColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rows-no-columns.yaml")

	// Write a YAML plan file with rows key but no columns key.
	raw := `rows:
  - link: https://example.com/video1
    start_time: "0:00"
    duration: "60"
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := LoadCollectionYAML(path, CollectionOptions{DefaultDuration: 60})
	if err == nil {
		t.Fatalf("expected error for rows without columns, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "columns") {
		t.Errorf("error message = %q, want message containing 'columns'", errMsg)
	}
	if errMsg != "YAML plan has rows but is missing required columns key" {
		t.Errorf("error message = %q, want %q", errMsg, "YAML plan has rows but is missing required columns key")
	}

	// result should be empty when there's an error
	if len(result.Rows) != 0 {
		t.Errorf("expected 0 rows in result, got %d", len(result.Rows))
	}
}

func TestLoadCollectionYAML_BareListBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bare-list.yaml")

	// Write a bare YAML list of maps (backward compat format).
	raw := `- link: https://example.com/video1
  start_time: "1:30"
  duration: "60"
- link: https://example.com/video2
  start_time: "0:45"
  duration: "45"
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := LoadCollectionYAML(path, CollectionOptions{DefaultDuration: 60})
	if err != nil {
		t.Fatalf("LoadCollectionYAML: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}

	// Verify first row
	if result.Rows[0].Link != "https://example.com/video1" {
		t.Errorf("row 1 link = %q, want https://example.com/video1", result.Rows[0].Link)
	}
	if result.Rows[0].Start != 90*time.Second {
		t.Errorf("row 1 start = %v, want 90s", result.Rows[0].Start)
	}
	if result.Rows[0].DurationSeconds != 60 {
		t.Errorf("row 1 duration = %d, want 60", result.Rows[0].DurationSeconds)
	}

	// Verify second row
	if result.Rows[1].Link != "https://example.com/video2" {
		t.Errorf("row 2 link = %q, want https://example.com/video2", result.Rows[1].Link)
	}
	if result.Rows[1].Start != 45*time.Second {
		t.Errorf("row 2 start = %v, want 45s", result.Rows[1].Start)
	}
	if result.Rows[1].DurationSeconds != 45 {
		t.Errorf("row 2 duration = %d, want 45", result.Rows[1].DurationSeconds)
	}

	// Columns should be empty for bare-list format (no schema declared).
	if len(result.Columns) != 0 {
		t.Errorf("expected 0 columns, got %d", len(result.Columns))
	}
}
