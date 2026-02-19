package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"powerhour/internal/paths"
)

const indexVersion = 2

// SourceType enumerates the origin of a cached asset.
type SourceType string

const (
	SourceTypeUnknown SourceType = ""
	SourceTypeURL     SourceType = "url"
	SourceTypeLocal   SourceType = "local"
)

// Index captures persistent cache state for fetched media artifacts.
type Index struct {
	Version int                        `json:"version"`
	Entries map[string]Entry           `json:"entries"`
	Links   map[string]string          `json:"links,omitempty"`
	Meta    map[string]json.RawMessage `json:"meta,omitempty"`
}

// Entry keeps metadata about a cached media artifact.
type Entry struct {
	Key         string         `json:"key"`
	Identifier  string         `json:"identifier"`
	ID          string         `json:"id,omitempty"`
	Extractor   string         `json:"extractor,omitempty"`
	Source      string         `json:"source"`
	SourceType  SourceType     `json:"source_type"`
	CachedPath  string         `json:"cached_path"`
	RetrievedAt time.Time      `json:"retrieved_at"`
	LastProbeAt time.Time      `json:"last_probe_at"`
	SizeBytes   int64          `json:"size_bytes,omitempty"`
	ETag        string         `json:"etag,omitempty"`
	Probe       *ProbeMetadata `json:"probe,omitempty"`
	Notes       []string       `json:"notes,omitempty"`
	Links       []string       `json:"links,omitempty"`
}

// ProbeMetadata includes the ffprobe results for the cached file.
type ProbeMetadata struct {
	FormatName      string          `json:"format_name,omitempty"`
	FormatLongName  string          `json:"format_long_name,omitempty"`
	DurationSeconds float64         `json:"duration_seconds,omitempty"`
	Streams         json.RawMessage `json:"streams,omitempty"`
	FormatRaw       json.RawMessage `json:"format_raw,omitempty"`
	Raw             json.RawMessage `json:"raw,omitempty"`
}

// LoadFromPath reads an index from the given file path, returning an empty
// structure when the file is missing.
func LoadFromPath(indexPath string) (*Index, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newIndex(), nil
		}
		return nil, fmt.Errorf("read index: %w", err)
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}

	idx.normalize()
	return &idx, nil
}

// SaveToPath writes an index to the given file path, creating the containing
// directory if needed. The write is performed atomically.
func SaveToPath(indexPath string, idx *Index) error {
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return fmt.Errorf("ensure index dir: %w", err)
	}

	if idx == nil {
		idx = newIndex()
	}
	idx.normalize()

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("encode index: %w", err)
	}

	tmp := indexPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp index: %w", err)
	}

	if err := os.Rename(tmp, indexPath); err != nil {
		return fmt.Errorf("replace index: %w", err)
	}

	return nil
}

// Load reads the index.json file from the provided project paths, returning an
// empty structure when the file is missing.
func Load(pp paths.ProjectPaths) (*Index, error) {
	return LoadFromPath(pp.IndexFile)
}

// Save writes the index.json file to disk, creating the containing directory if
// needed. The write is performed atomically.
func Save(pp paths.ProjectPaths, idx *Index) error {
	return SaveToPath(pp.IndexFile, idx)
}

// GetByIdentifier returns an entry for the provided canonical identifier when present.
func (idx *Index) GetByIdentifier(identifier string) (Entry, bool) {
	if idx == nil || idx.Entries == nil {
		return Entry{}, false
	}
	key := strings.TrimSpace(identifier)
	if key == "" {
		return Entry{}, false
	}
	entry, ok := idx.Entries[key]
	return entry, ok
}

// SetEntry stores an entry keyed by its canonical identifier.
func (idx *Index) SetEntry(entry Entry) {
	if idx == nil {
		return
	}
	key := strings.TrimSpace(entry.Identifier)
	if key == "" {
		return
	}
	if idx.Entries == nil {
		idx.Entries = map[string]Entry{}
	}
	idx.Entries[key] = entry
}

// DeleteEntry removes an entry for the provided canonical identifier.
func (idx *Index) DeleteEntry(identifier string) {
	if idx == nil || idx.Entries == nil {
		return
	}
	key := strings.TrimSpace(identifier)
	if key == "" {
		return
	}
	delete(idx.Entries, key)
}

// LookupLink returns the canonical identifier associated with a link, if recorded.
func (idx *Index) LookupLink(link string) (string, bool) {
	if idx == nil || idx.Links == nil {
		return "", false
	}
	key, ok := idx.Links[normalizeLink(link)]
	return key, ok
}

// SetLink records the canonical identifier for a given link string.
func (idx *Index) SetLink(link, identifier string) {
	if idx == nil {
		return
	}
	linkKey := normalizeLink(link)
	idKey := strings.TrimSpace(identifier)
	if linkKey == "" || idKey == "" {
		return
	}
	if idx.Links == nil {
		idx.Links = map[string]string{}
	}
	idx.Links[linkKey] = idKey
}

// DeleteLink removes any recorded mapping for the supplied link.
func (idx *Index) DeleteLink(link string) {
	if idx == nil || idx.Links == nil {
		return
	}
	delete(idx.Links, normalizeLink(link))
}

func (idx *Index) normalize() {
	if idx.Version == 0 {
		idx.Version = indexVersion
	}
	if idx.Entries == nil {
		idx.Entries = map[string]Entry{}
	}
	if idx.Links == nil {
		idx.Links = map[string]string{}
	}
}

func newIndex() *Index {
	return &Index{
		Version: indexVersion,
		Entries: map[string]Entry{},
		Links:   map[string]string{},
	}
}

func normalizeLink(link string) string {
	return strings.TrimSpace(link)
}
