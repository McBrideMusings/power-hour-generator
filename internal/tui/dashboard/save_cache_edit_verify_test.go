package dashboard

import (
	"os"
	"testing"

	"powerhour/internal/cache"
)

// TestSaveCacheEditUsesInMemoryIndexNotDiskReload proves saveCacheEdit no
// longer reloads the cache index from disk on every call (e.g. every Tab
// during inline edit). It corrupts the on-disk index file after the model
// is built, then performs a save; if saveCacheEdit still called cache.Load,
// this would either fail (corrupt JSON) or silently lose the in-memory
// edit. With the fix, the save must succeed and use m.cacheIdx as the
// source of truth, then persist that corrected state back to disk.
func TestSaveCacheEditUsesInMemoryIndexNotDiskReload(t *testing.T) {
	root := t.TempDir()
	cachedPath := writeTestCacheFile(t, root, "song1.mp4")

	idx := &cache.Index{
		Entries: map[string]cache.Entry{
			"song1": {
				Key:        "song1",
				Identifier: "song1",
				Source:     "https://example.com/watch?v=song1",
				SourceType: cache.SourceTypeURL,
				CachedPath: cachedPath,
				Title:      "Old Title",
			},
		},
	}

	m := newTestCacheModel(t, root, idx, []string{"https://example.com/watch?v=song1"})

	// Corrupt the on-disk index file. If saveCacheEdit calls cache.Load,
	// this call must now fail.
	indexPath := m.pp.IndexFile
	if indexPath == "" {
		t.Fatalf("expected non-empty index file path")
	}
	if err := os.WriteFile(indexPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("corrupt index file: %v", err)
	}

	v := m.cacheView
	v.showAll = true
	v.cursor = 0
	v.editing = true
	v.editFieldIdx = indexOfColumn(v.columns, "title")
	if v.editFieldIdx < 0 {
		t.Fatalf("title column not found in %v", v.columns)
	}
	v.editValue = "New Title"

	gotModel, _ := m.saveCacheEdit(v, true)
	got := gotModel.(Model)

	if got.statusMsg != "" {
		t.Fatalf("unexpected statusMsg (indicates disk reload path ran): %q", got.statusMsg)
	}

	entry, ok := got.cacheIdx.GetByIdentifier("song1")
	if !ok {
		t.Fatalf("entry not found after save")
	}
	if entry.Title != "New Title" {
		t.Fatalf("Title = %q, want %q", entry.Title, "New Title")
	}

	// Confirm it was actually persisted to disk (cache.Save succeeded,
	// overwriting our corrupted file).
	reloaded, err := cache.Load(m.pp)
	if err != nil {
		t.Fatalf("reload persisted index: %v", err)
	}
	persisted, ok := reloaded.GetByIdentifier("song1")
	if !ok || persisted.Title != "New Title" {
		t.Fatalf("persisted entry = %+v, ok=%v, want Title=New Title", persisted, ok)
	}
}

func indexOfColumn(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}
