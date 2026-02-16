package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	releaseCacheFile = "release_cache.json"
	releaseCacheTTL  = 1 * time.Hour
)

type releaseCacheEntry struct {
	Tool      string    `json:"tool"`
	Version   string    `json:"version"`
	URL       string    `json:"url"`
	Checksum  string    `json:"checksum"`
	Archive   string    `json:"archive"`
	FetchedAt time.Time `json:"fetched_at"`
}

type releaseCache struct {
	Entries map[string]releaseCacheEntry `json:"entries"`
}

func releaseCachePath() (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, releaseCacheFile), nil
}

func loadReleaseCache() releaseCache {
	path, err := releaseCachePath()
	if err != nil {
		return releaseCache{Entries: map[string]releaseCacheEntry{}}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseCache{Entries: map[string]releaseCacheEntry{}}
	}
	var rc releaseCache
	if err := json.Unmarshal(data, &rc); err != nil {
		return releaseCache{Entries: map[string]releaseCacheEntry{}}
	}
	if rc.Entries == nil {
		rc.Entries = map[string]releaseCacheEntry{}
	}
	return rc
}

func saveReleaseCache(rc releaseCache) {
	path, err := releaseCachePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// cachedLatestRelease returns a cached release spec if available and not expired.
func cachedLatestRelease(tool string) (releaseSpec, bool) {
	rc := loadReleaseCache()
	entry, ok := rc.Entries[tool]
	if !ok {
		return releaseSpec{}, false
	}
	if time.Since(entry.FetchedAt) > releaseCacheTTL {
		return releaseSpec{}, false
	}
	return releaseSpec{
		Version:  entry.Version,
		URL:      entry.URL,
		Checksum: entry.Checksum,
		Archive:  archiveFormat(entry.Archive),
	}, true
}

// cacheLatestRelease stores a release spec in the cache.
func cacheLatestRelease(tool string, spec releaseSpec) {
	rc := loadReleaseCache()
	rc.Entries[tool] = releaseCacheEntry{
		Tool:      tool,
		Version:   spec.Version,
		URL:       spec.URL,
		Checksum:  spec.Checksum,
		Archive:   string(spec.Archive),
		FetchedAt: time.Now(),
	}
	saveReleaseCache(rc)
}
