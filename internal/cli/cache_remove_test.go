package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"powerhour/internal/cache"
)

func setUpCacheRemoveProject(t *testing.T, idx *cache.Index) (dir, indexPath string) {
	t.Helper()
	dir = t.TempDir()
	indexPath = filepath.Join(dir, ".powerhour", "index.json")
	if err := cache.SaveToPath(indexPath, idx); err != nil {
		t.Fatalf("SaveToPath: %v", err)
	}

	projectDir = dir
	cacheRemoveDryRun = false
	cacheRemoveKeepFile = false
	t.Cleanup(func() {
		projectDir = ""
		cacheRemoveDryRun = false
		cacheRemoveKeepFile = false
	})

	return dir, indexPath
}

func TestCacheRemoveExactIdentifier(t *testing.T) {
	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"youtube:abc123": {
				Identifier: "youtube:abc123",
				ID:         "abc123",
				Title:      "Live Set",
				SourceType: cache.SourceTypeLocal,
			},
		},
	}
	_, indexPath := setUpCacheRemoveProject(t, idx)

	cmd := newCacheRemoveCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v (stderr=%s)", err, errBuf.String())
	}
	if !strings.Contains(out.String(), "Removed:") {
		t.Fatalf("expected stdout to contain %q, got %q", "Removed:", out.String())
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if _, ok := loaded.GetByIdentifier("youtube:abc123"); ok {
		t.Fatal("expected entry to be removed from index")
	}
}

func TestCacheRemoveExactIdentifierFullForm(t *testing.T) {
	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"youtube:abc123": {
				Identifier: "youtube:abc123",
				ID:         "abc123",
				Title:      "Live Set",
				SourceType: cache.SourceTypeLocal,
			},
		},
	}
	_, indexPath := setUpCacheRemoveProject(t, idx)

	cmd := newCacheRemoveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"youtube:abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if _, ok := loaded.GetByIdentifier("youtube:abc123"); ok {
		t.Fatal("expected entry to be removed from index")
	}
}

func TestCacheRemoveAmbiguousMatch(t *testing.T) {
	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"youtube:abc123": {
				Identifier: "youtube:abc123",
				ID:         "abc123",
				Title:      "Live Set",
				SourceType: cache.SourceTypeLocal,
			},
			"youtube:abc124": {
				Identifier: "youtube:abc124",
				ID:         "abc124",
				Title:      "Live Encore",
				SourceType: cache.SourceTypeLocal,
			},
		},
	}
	_, indexPath := setUpCacheRemoveProject(t, idx)

	cmd := newCacheRemoveCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"live"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for an ambiguous match")
	}
	if !strings.Contains(err.Error(), "more specific") {
		t.Fatalf("expected error to mention 'more specific', got %q", err.Error())
	}
	if !strings.Contains(errBuf.String(), "Ambiguous") {
		t.Fatalf("expected stderr to contain %q, got %q", "Ambiguous", errBuf.String())
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("expected both entries to survive an ambiguous match, got %d", len(loaded.Entries))
	}
	if _, ok := loaded.GetByIdentifier("youtube:abc123"); !ok {
		t.Fatal("expected youtube:abc123 to still be present")
	}
	if _, ok := loaded.GetByIdentifier("youtube:abc124"); !ok {
		t.Fatal("expected youtube:abc124 to still be present")
	}
}

func TestCacheRemoveClearsLinkMappings(t *testing.T) {
	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"youtube:abc123": {
				Identifier: "youtube:abc123",
				ID:         "abc123",
				Title:      "Live Set",
				SourceType: cache.SourceTypeLocal,
			},
			"youtube:xyz789": {
				Identifier: "youtube:xyz789",
				ID:         "xyz789",
				Title:      "Other Song",
				SourceType: cache.SourceTypeLocal,
			},
		},
		Links: map[string]string{
			"https://youtube.com/watch?v=abc123": "youtube:abc123",
			"https://youtu.be/abc123":            "youtube:abc123",
			"https://youtube.com/watch?v=xyz789": "youtube:xyz789",
		},
	}
	_, indexPath := setUpCacheRemoveProject(t, idx)

	cmd := newCacheRemoveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"youtube:abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if _, ok := loaded.Links["https://youtube.com/watch?v=abc123"]; ok {
		t.Fatal("expected link to removed entry to be cleared")
	}
	if _, ok := loaded.Links["https://youtu.be/abc123"]; ok {
		t.Fatal("expected second link to removed entry to be cleared")
	}
	if target, ok := loaded.Links["https://youtube.com/watch?v=xyz789"]; !ok || target != "youtube:xyz789" {
		t.Fatal("expected unrelated link to survive")
	}
}

func TestCacheRemoveDeletesURLSourcedFile(t *testing.T) {
	dir := t.TempDir()
	cachedFile := filepath.Join(dir, "cache", "abc123.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedFile, []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"youtube:abc123": {
				Identifier: "youtube:abc123",
				ID:         "abc123",
				Title:      "Live Set",
				SourceType: cache.SourceTypeURL,
				CachedPath: cachedFile,
			},
		},
	}
	origDir := dir
	indexPath := filepath.Join(origDir, ".powerhour", "index.json")
	if err := cache.SaveToPath(indexPath, idx); err != nil {
		t.Fatalf("SaveToPath: %v", err)
	}
	projectDir = origDir
	cacheRemoveDryRun = false
	cacheRemoveKeepFile = false
	t.Cleanup(func() {
		projectDir = ""
		cacheRemoveDryRun = false
		cacheRemoveKeepFile = false
	})

	cmd := newCacheRemoveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"youtube:abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "Deleted:") {
		t.Fatalf("expected stdout to contain %q, got %q", "Deleted:", out.String())
	}
	if _, err := os.Stat(cachedFile); !os.IsNotExist(err) {
		t.Fatal("expected cached file for URL-sourced entry to be deleted")
	}
}

func TestCacheRemoveKeepsLocalSourceFile(t *testing.T) {
	dir := t.TempDir()
	localFile := filepath.Join(dir, "song.mp4")
	if err := os.WriteFile(localFile, []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"local:song": {
				Identifier: "local:song",
				Title:      "My Local Song",
				SourceType: cache.SourceTypeLocal,
				CachedPath: localFile,
			},
		},
	}
	indexPath := filepath.Join(dir, ".powerhour", "index.json")
	if err := cache.SaveToPath(indexPath, idx); err != nil {
		t.Fatalf("SaveToPath: %v", err)
	}
	projectDir = dir
	cacheRemoveDryRun = false
	cacheRemoveKeepFile = false
	t.Cleanup(func() {
		projectDir = ""
		cacheRemoveDryRun = false
		cacheRemoveKeepFile = false
	})

	cmd := newCacheRemoveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"local:song"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out.String(), "Deleted:") {
		t.Fatalf("did not expect stdout to mention a deleted file for a local-sourced entry, got %q", out.String())
	}
	if _, err := os.Stat(localFile); err != nil {
		t.Fatalf("expected local file to survive removal, got stat error: %v", err)
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if _, ok := loaded.GetByIdentifier("local:song"); ok {
		t.Fatal("expected index record to be removed even though the file survives")
	}
}

func TestCacheRemoveKeepFileFlagPreservesURLSourcedFile(t *testing.T) {
	dir := t.TempDir()
	cachedFile := filepath.Join(dir, "cache", "keepme.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedFile, []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &cache.Index{
		Version: 2,
		Entries: map[string]cache.Entry{
			"youtube:keepme": {
				Identifier: "youtube:keepme",
				ID:         "keepme",
				Title:      "Keep Me",
				SourceType: cache.SourceTypeURL,
				CachedPath: cachedFile,
			},
		},
	}
	indexPath := filepath.Join(dir, ".powerhour", "index.json")
	if err := cache.SaveToPath(indexPath, idx); err != nil {
		t.Fatalf("SaveToPath: %v", err)
	}
	projectDir = dir
	cacheRemoveDryRun = false
	cacheRemoveKeepFile = false
	t.Cleanup(func() {
		projectDir = ""
		cacheRemoveDryRun = false
		cacheRemoveKeepFile = false
	})

	cmd := newCacheRemoveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--keep-file", "youtube:keepme"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(cachedFile); err != nil {
		t.Fatalf("expected cached file to survive with --keep-file, got stat error: %v", err)
	}

	loaded, err := cache.LoadFromPath(indexPath)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if _, ok := loaded.GetByIdentifier("youtube:keepme"); ok {
		t.Fatal("expected index record to be removed even with --keep-file")
	}
}
