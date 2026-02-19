package cache

import (
	"path/filepath"
	"testing"
)

func TestLoadFromPathMissing(t *testing.T) {
	idx, err := LoadFromPath(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
	if len(idx.Entries) != 0 {
		t.Fatalf("expected empty entries, got %d", len(idx.Entries))
	}
}

func TestSaveToPathRoundtrip(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "sub", "index.json")

	idx := newIndex()
	idx.SetEntry(Entry{
		Identifier: "youtube:abc123",
		CachedPath: "/some/path/video.mp4",
		SizeBytes:  1234,
	})
	idx.SetLink("https://youtube.com/watch?v=abc123", "youtube:abc123")

	if err := SaveToPath(indexPath, idx); err != nil {
		t.Fatalf("SaveToPath: %v", err)
	}

	loaded, err := LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}

	entry, ok := loaded.GetByIdentifier("youtube:abc123")
	if !ok {
		t.Fatal("expected entry for youtube:abc123")
	}
	if entry.CachedPath != "/some/path/video.mp4" {
		t.Fatalf("expected cached_path, got %s", entry.CachedPath)
	}

	linkID, ok := loaded.LookupLink("https://youtube.com/watch?v=abc123")
	if !ok {
		t.Fatal("expected link mapping")
	}
	if linkID != "youtube:abc123" {
		t.Fatalf("expected link to youtube:abc123, got %s", linkID)
	}
}
