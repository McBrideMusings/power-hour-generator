package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"powerhour/internal/paths"
)

const indexVersion = 1

// SourceType enumerates the origin of a cached asset.
type SourceType string

const (
	SourceTypeUnknown SourceType = ""
	SourceTypeURL     SourceType = "url"
	SourceTypeLocal   SourceType = "local"
)

// Index captures per-row cache state persisted to .powerhour/index.json.
type Index struct {
	Version int                        `json:"version"`
	Entries map[string]Entry           `json:"entries"`
	Meta    map[string]json.RawMessage `json:"meta,omitempty"`
}

// Entry keeps metadata about a cached media artifact.
type Entry struct {
	RowIndex    int            `json:"row_index"`
	Key         string         `json:"key"`
	Source      string         `json:"source"`
	SourceType  SourceType     `json:"source_type"`
	CachedPath  string         `json:"cached_path"`
	RetrievedAt time.Time      `json:"retrieved_at"`
	LastProbeAt time.Time      `json:"last_probe_at"`
	SizeBytes   int64          `json:"size_bytes,omitempty"`
	ETag        string         `json:"etag,omitempty"`
	Probe       *ProbeMetadata `json:"probe,omitempty"`
	Notes       []string       `json:"notes,omitempty"`
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

// Load reads the index.json file from the provided project paths, returning an
// empty structure when the file is missing.
func Load(pp paths.ProjectPaths) (*Index, error) {
	data, err := os.ReadFile(pp.IndexFile)
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

// Save writes the index.json file to disk, creating the containing directory if
// needed. The write is performed atomically.
func Save(pp paths.ProjectPaths, idx *Index) error {
	if err := os.MkdirAll(filepath.Dir(pp.IndexFile), 0o755); err != nil {
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

	tmp := pp.IndexFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp index: %w", err)
	}

	if err := os.Rename(tmp, pp.IndexFile); err != nil {
		return fmt.Errorf("replace index: %w", err)
	}

	return nil
}

// Get returns an entry for the provided row index when present.
func (idx *Index) Get(rowIndex int) (Entry, bool) {
	if idx == nil || idx.Entries == nil {
		return Entry{}, false
	}
	entry, ok := idx.Entries[rowKey(rowIndex)]
	return entry, ok
}

// Set stores an entry for the provided row index.
func (idx *Index) Set(entry Entry) {
	if idx == nil {
		return
	}
	if idx.Entries == nil {
		idx.Entries = map[string]Entry{}
	}
	idx.Entries[rowKey(entry.RowIndex)] = entry
}

// Delete removes an entry for the provided row index.
func (idx *Index) Delete(rowIndex int) {
	if idx == nil || idx.Entries == nil {
		return
	}
	delete(idx.Entries, rowKey(rowIndex))
}

func (idx *Index) normalize() {
	if idx.Version == 0 {
		idx.Version = indexVersion
	}
	if idx.Entries == nil {
		idx.Entries = map[string]Entry{}
	}
}

func rowKey(rowIndex int) string {
	return strconv.Itoa(rowIndex)
}

func newIndex() *Index {
	return &Index{
		Version: indexVersion,
		Entries: map[string]Entry{},
	}
}
