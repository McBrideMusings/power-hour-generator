package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"powerhour/internal/cache"
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

func setUpCleanStaleLocalCopiesProject(t *testing.T, idx *cache.Index) (dir, indexPath string) {
	t.Helper()
	dir = t.TempDir()
	indexPath = filepath.Join(dir, ".powerhour", "index.json")
	if err := cache.SaveToPath(indexPath, idx); err != nil {
		t.Fatalf("SaveToPath: %v", err)
	}

	projectDir = dir
	cleanDryRun = false
	t.Cleanup(func() {
		projectDir = ""
		cleanDryRun = false
	})

	return dir, indexPath
}

func TestCleanStaleLocalCopiesDryRunLeavesFileAndIndexUntouched(t *testing.T) {
	dir := t.TempDir()
	stalePath := filepath.Join(dir, "cache", "stale-local.mp4")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"local:stale": {
				Identifier: "local:stale",
				Title:      "Stale Local Copy",
				SourceType: cache.SourceTypeLocal,
				CachedPath: stalePath,
			},
		},
	}
	indexPath := filepath.Join(dir, ".powerhour", "index.json")
	if err := cache.SaveToPath(indexPath, idx); err != nil {
		t.Fatal(err)
	}
	projectDir = dir
	cleanDryRun = true
	t.Cleanup(func() {
		projectDir = ""
		cleanDryRun = false
	})

	cmd := newCleanStaleLocalCopiesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "would remove") {
		t.Fatalf("expected dry-run output to list stale entry, got %q", out.String())
	}

	if _, err := os.Stat(stalePath); err != nil {
		t.Fatalf("expected stale file to survive dry run, got stat error: %v", err)
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	entry, ok := loaded.GetByIdentifier("local:stale")
	if !ok {
		t.Fatal("expected entry to still exist after dry run")
	}
	if entry.CachedPath != stalePath {
		t.Fatalf("expected CachedPath to be unchanged after dry run, got %q", entry.CachedPath)
	}
}

func TestCleanStaleLocalCopiesRemovesFileAndClearsCachedPath(t *testing.T) {
	dir := t.TempDir()
	stalePath := filepath.Join(dir, "cache", "stale-local.mp4")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"local:stale": {
				Identifier: "local:stale",
				Title:      "Stale Local Copy",
				SourceType: cache.SourceTypeLocal,
				CachedPath: stalePath,
			},
		},
	}
	indexPath := filepath.Join(dir, ".powerhour", "index.json")
	if err := cache.SaveToPath(indexPath, idx); err != nil {
		t.Fatal(err)
	}
	projectDir = dir
	cleanDryRun = false
	t.Cleanup(func() {
		projectDir = ""
		cleanDryRun = false
	})

	cmd := newCleanStaleLocalCopiesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatal("expected stale cache file to be deleted")
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	entry, ok := loaded.GetByIdentifier("local:stale")
	if !ok {
		t.Fatal("expected index entry to still exist (not deleted)")
	}
	if entry.CachedPath != "" {
		t.Fatalf("expected CachedPath to be cleared, got %q", entry.CachedPath)
	}
	if entry.SourceType != cache.SourceTypeLocal {
		t.Fatalf("expected entry to remain SourceTypeLocal, got %q", entry.SourceType)
	}
}

func TestCleanStaleLocalCopiesKeepsCachedPathWhenRemovalFails(t *testing.T) {
	dir := t.TempDir()

	// This entry's CachedPath resolves successfully and should be cleared.
	removablePath := filepath.Join(dir, "cache", "removable.mp4")
	if err := os.MkdirAll(filepath.Dir(removablePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(removablePath, []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}

	// This entry's CachedPath points at a non-empty directory, so os.Remove
	// fails on it (ENOTEMPTY/EISDIR) and the index reference must survive.
	stuckPath := filepath.Join(dir, "cache", "stuck.mp4")
	if err := os.MkdirAll(stuckPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuckPath, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"local:removable": {
				Identifier: "local:removable",
				Title:      "Removable Stale Copy",
				SourceType: cache.SourceTypeLocal,
				CachedPath: removablePath,
			},
			"local:stuck": {
				Identifier: "local:stuck",
				Title:      "Stuck Stale Copy",
				SourceType: cache.SourceTypeLocal,
				CachedPath: stuckPath,
			},
		},
	}
	indexPath := filepath.Join(dir, ".powerhour", "index.json")
	if err := cache.SaveToPath(indexPath, idx); err != nil {
		t.Fatal(err)
	}
	projectDir = dir
	cleanDryRun = false
	t.Cleanup(func() {
		projectDir = ""
		cleanDryRun = false
	})

	cmd := newCleanStaleLocalCopiesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(removablePath); !os.IsNotExist(err) {
		t.Fatal("expected removable stale cache file to be deleted")
	}
	if _, err := os.Stat(stuckPath); err != nil {
		t.Fatalf("expected stuck path to survive the failed removal, got stat error: %v", err)
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}

	removableEntry, ok := loaded.GetByIdentifier("local:removable")
	if !ok {
		t.Fatal("expected removable entry to still exist")
	}
	if removableEntry.CachedPath != "" {
		t.Fatalf("expected removable entry's CachedPath to be cleared, got %q", removableEntry.CachedPath)
	}

	stuckEntry, ok := loaded.GetByIdentifier("local:stuck")
	if !ok {
		t.Fatal("expected stuck entry to still exist")
	}
	if stuckEntry.CachedPath != stuckPath {
		t.Fatalf("expected stuck entry's CachedPath to remain unchanged after failed removal, got %q", stuckEntry.CachedPath)
	}
}

func TestCleanStaleLocalCopiesLeavesExternalLocalFileUntouched(t *testing.T) {
	externalDir := t.TempDir()
	externalPath := filepath.Join(externalDir, "my-song.mp4")
	if err := os.WriteFile(externalPath, []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"local:external": {
				Identifier: "local:external",
				Title:      "External Local File",
				SourceType: cache.SourceTypeLocal,
				CachedPath: externalPath,
			},
		},
	}
	_, indexPath := setUpCleanStaleLocalCopiesProject(t, idx)

	t.Run("dry run", func(t *testing.T) {
		cleanDryRun = true
		cmd := newCleanStaleLocalCopiesCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if strings.Contains(out.String(), externalPath) {
			t.Fatalf("expected external entry to be left out of dry-run output, got %q", out.String())
		}
	})

	t.Run("real run", func(t *testing.T) {
		cleanDryRun = false
		cmd := newCleanStaleLocalCopiesCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})

	if _, err := os.Stat(externalPath); err != nil {
		t.Fatalf("expected external local file to survive both runs, got stat error: %v", err)
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	entry, ok := loaded.GetByIdentifier("local:external")
	if !ok {
		t.Fatal("expected entry to still exist")
	}
	if entry.CachedPath != externalPath {
		t.Fatalf("expected CachedPath to remain untouched, got %q", entry.CachedPath)
	}
}
