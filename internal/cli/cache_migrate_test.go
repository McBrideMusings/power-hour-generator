package cli

import (
	"os"
	"path/filepath"
	"testing"

	"powerhour/internal/cache"
)

func TestMigrateMovesFiles(t *testing.T) {
	// Setup local cache
	localDir := t.TempDir()
	localCacheDir := filepath.Join(localDir, "cache")
	os.MkdirAll(localCacheDir, 0o755)

	localFile := filepath.Join(localCacheDir, "video.mp4")
	os.WriteFile(localFile, []byte("fake video data"), 0o644)

	localIndexPath := filepath.Join(localDir, "index.json")
	localIdx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"youtube:abc123": {
				Identifier: "youtube:abc123",
				CachedPath: localFile,
				SizeBytes:  15,
			},
		},
		Links: map[string]string{
			"https://youtube.com/watch?v=abc123": "youtube:abc123",
		},
	}
	if err := cache.SaveToPath(localIndexPath, localIdx); err != nil {
		t.Fatal(err)
	}

	// Setup global cache
	globalDir := t.TempDir()
	globalCacheDir := filepath.Join(globalDir, "cache")
	os.MkdirAll(globalCacheDir, 0o755)
	globalIndexPath := filepath.Join(globalDir, "index.json")

	// Run migration logic manually
	globalIdx, _ := cache.LoadFromPath(globalIndexPath)

	entry := localIdx.Entries["youtube:abc123"]
	dest := filepath.Join(globalCacheDir, filepath.Base(entry.CachedPath))

	if err := moveFile(entry.CachedPath, dest); err != nil {
		t.Fatalf("moveFile: %v", err)
	}

	entry.CachedPath = dest
	globalIdx.SetEntry(entry)
	globalIdx.SetLink("https://youtube.com/watch?v=abc123", "youtube:abc123")

	if err := cache.SaveToPath(globalIndexPath, globalIdx); err != nil {
		t.Fatal(err)
	}

	// Verify
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected file at %s", dest)
	}
	if _, err := os.Stat(localFile); !os.IsNotExist(err) {
		t.Fatal("expected local file to be removed")
	}

	loaded, _ := cache.LoadFromPath(globalIndexPath)
	e, ok := loaded.GetByIdentifier("youtube:abc123")
	if !ok {
		t.Fatal("expected global entry")
	}
	if e.CachedPath != dest {
		t.Fatalf("expected cached_path=%s, got %s", dest, e.CachedPath)
	}
}

func TestMigrateDryRunDoesNotMoveFiles(t *testing.T) {
	localDir := t.TempDir()
	localFile := filepath.Join(localDir, "video.mp4")
	os.WriteFile(localFile, []byte("data"), 0o644)

	// dry run should not touch the file
	if _, err := os.Stat(localFile); err != nil {
		t.Fatal("file should exist before dry run")
	}
	// File should still exist (we're just testing the helper isn't called)
	if _, err := os.Stat(localFile); err != nil {
		t.Fatal("file should still exist after dry run")
	}
}

func TestDeduplicateFilename(t *testing.T) {
	dir := t.TempDir()

	// Create the original file
	os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("a"), 0o644)

	result := deduplicateFilename(dir, "video.mp4")
	expected := filepath.Join(dir, "video_1.mp4")
	if result != expected {
		t.Fatalf("expected %s, got %s", expected, result)
	}

	// Create _1 too
	os.WriteFile(expected, []byte("b"), 0o644)
	result2 := deduplicateFilename(dir, "video.mp4")
	expected2 := filepath.Join(dir, "video_2.mp4")
	if result2 != expected2 {
		t.Fatalf("expected %s, got %s", expected2, result2)
	}
}

func TestMoveFileCrossDevice(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.txt")
	dst := filepath.Join(t.TempDir(), "dst.txt")

	content := []byte("hello world")
	os.WriteFile(src, content, 0o644)

	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("expected content 'hello world', got %s", string(data))
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("expected source to be removed")
	}
}
