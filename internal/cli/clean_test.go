package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlobFiles(t *testing.T) {
	dir := t.TempDir()

	// Create nested segment files
	subdir := filepath.Join(dir, "songs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.mp4", "b.mp4", "c.txt"} {
		if err := os.WriteFile(filepath.Join(subdir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "top.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("mp4 glob", func(t *testing.T) {
		files, err := globFiles(dir, "**/*.mp4")
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 3 {
			t.Fatalf("got %d files, want 3: %v", len(files), files)
		}
	})

	t.Run("all glob", func(t *testing.T) {
		files, err := globFiles(subdir, "*")
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 3 {
			t.Fatalf("got %d files, want 3: %v", len(files), files)
		}
	})

	t.Run("nonexistent dir", func(t *testing.T) {
		files, err := globFiles(filepath.Join(dir, "nope"), "**/*.mp4")
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 0 {
			t.Fatalf("got %d files, want 0", len(files))
		}
	})
}

func TestDiffPaths(t *testing.T) {
	actual := []string{"/a/1.mp4", "/a/2.mp4", "/a/3.mp4"}
	expected := map[string]bool{
		"/a/1.mp4": true,
		"/a/3.mp4": true,
	}

	orphans := diffPaths(actual, expected)
	if len(orphans) != 1 {
		t.Fatalf("got %d orphans, want 1", len(orphans))
	}
	if orphans[0] != "/a/2.mp4" {
		t.Fatalf("got %s, want /a/2.mp4", orphans[0])
	}
}

func TestRemoveFileEntry(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.mp4")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("dry run does not delete", func(t *testing.T) {
		cleanDryRun = true
		defer func() { cleanDryRun = false }()

		result := cleanResult{DryRun: true}
		removeFileEntry(file, os.Stdout, &result)

		if result.Removed != 1 {
			t.Fatalf("got removed=%d, want 1", result.Removed)
		}
		if result.FreedBytes != 5 {
			t.Fatalf("got freed=%d, want 5", result.FreedBytes)
		}
		// File should still exist
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("file should still exist after dry run: %v", err)
		}
	})

	t.Run("actual remove deletes file", func(t *testing.T) {
		cleanDryRun = false
		result := cleanResult{}
		removeFileEntry(file, os.Stdout, &result)

		if result.Removed != 1 {
			t.Fatalf("got removed=%d, want 1", result.Removed)
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatal("file should have been removed")
		}
	})

	t.Run("nonexistent file is skipped", func(t *testing.T) {
		result := cleanResult{}
		removeFileEntry(filepath.Join(dir, "nope.mp4"), os.Stdout, &result)
		if result.Skipped != 1 {
			t.Fatalf("got skipped=%d, want 1", result.Skipped)
		}
	})
}
