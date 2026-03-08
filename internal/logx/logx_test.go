package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPruneGlobalLogs_RemovesOldest(t *testing.T) {
	dir := t.TempDir()

	// Create 60 log files with sortable names
	for i := 0; i < 60; i++ {
		name := filepath.Join(dir, fmt.Sprintf("cmd-%04d.log", i))
		if err := os.WriteFile(name, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pruneGlobalLogs(dir, 50)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 50 {
		t.Fatalf("expected 50 files, got %d", len(entries))
	}

	// Verify the oldest files (0000-0009) were removed and newest remain
	if entries[0].Name() != "cmd-0010.log" {
		t.Errorf("expected first remaining file to be cmd-0010.log, got %s", entries[0].Name())
	}
	if entries[49].Name() != "cmd-0059.log" {
		t.Errorf("expected last remaining file to be cmd-0059.log, got %s", entries[49].Name())
	}
}

func TestPruneGlobalLogs_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	// Should not panic or error
	pruneGlobalLogs(dir, 50)
}

func TestPruneGlobalLogs_UnderLimit(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 10; i++ {
		name := filepath.Join(dir, fmt.Sprintf("cmd-%04d.log", i))
		if err := os.WriteFile(name, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pruneGlobalLogs(dir, 50)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 10 {
		t.Fatalf("expected 10 files unchanged, got %d", len(entries))
	}
}

func TestPruneGlobalLogs_IgnoresNonLogFiles(t *testing.T) {
	dir := t.TempDir()

	// Create 5 log files and 5 non-log files
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("cmd-%04d.log", i)), []byte("test"), 0o644)
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("cmd-%04d.txt", i)), []byte("test"), 0o644)
	}

	pruneGlobalLogs(dir, 3)

	// Count remaining log files
	entries, _ := os.ReadDir(dir)
	logCount := 0
	txtCount := 0
	for _, e := range entries {
		switch filepath.Ext(e.Name()) {
		case ".log":
			logCount++
		case ".txt":
			txtCount++
		}
	}

	if logCount != 3 {
		t.Errorf("expected 3 log files, got %d", logCount)
	}
	if txtCount != 5 {
		t.Errorf("expected 5 txt files unchanged, got %d", txtCount)
	}
}

func TestPruneGlobalLogs_NonexistentDir(t *testing.T) {
	// Should not panic
	pruneGlobalLogs("/nonexistent/path/that/doesnt/exist", 50)
}
