package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"powerhour/internal/render"
)

// SegmentState tracks the render inputs and output for a single segment.
type SegmentState struct {
	InputHash  string    `json:"input_hash"`
	RenderedAt time.Time `json:"rendered_at"`
	SourcePath string    `json:"source_path"`
	DurationS  float64   `json:"duration_s"`
	// OutputPath is where this segment's file was last written. It is the
	// rename source when a segment keeps its hash but its filename changes,
	// which is what a reorder does to any segment whose name embeds its
	// playback position.
	OutputPath string `json:"output_path,omitempty"`
}

// RenderState tracks render state across all segments for change detection.
type RenderState struct {
	GlobalConfigHash string                  `json:"global_config_hash"`
	Segments         map[string]SegmentState `json:"segments"`
}

// SegmentKey is the render-state key for a segment.
//
// A segment backed by a plan row keys on its stable row identity, so the
// entry survives the output filename changing when the row moves in the
// playback order. A segment with no row id — an inline file: entry, which
// has no plan row at all — has no stable identity to key on and falls back
// to its output path. That path never moves, because a file slot
// structurally never participates in a swap or a shuffle.
//
// The two key spaces are prefixed so they can never collide.
func SegmentKey(seg render.Segment) string {
	if rowID := strings.TrimSpace(seg.Clip.Row.RowID); rowID != "" {
		return "row:" + string(seg.Clip.ClipType) + "/" + rowID
	}
	return "path:" + seg.OutputPath
}

// SegmentKeys returns the render-state key for each segment, in order.
func SegmentKeys(segments []render.Segment) []string {
	keys := make([]string, len(segments))
	for i, seg := range segments {
		keys[i] = SegmentKey(seg)
	}
	return keys
}

// MigrateKeys rewrites any state entry still keyed by a bare output path
// into the current scheme, carrying its hash and its real on-disk location
// across so an upgrade does not re-encode the whole project once.
//
// Two joins, in order. An entry keyed by a segment's current output path is
// that segment's — it is the same file under the same name. Whatever is
// left is matched on source path, which is what identifies a segment's file
// when the filename itself has changed (exactly what this rekey does to
// every segment whose template embeds its position). A source path claimed
// by more than one leftover entry is ambiguous and is not migrated; those
// segments get re-encoded once.
//
// This is a one-shot conversion, not a compatibility layer: the old shape is
// gone from disk after the next Save, and nothing reads it again.
func MigrateKeys(rs *RenderState, segments []render.Segment) {
	if rs == nil || len(rs.Segments) == 0 {
		return
	}

	legacy := make(map[string]SegmentState, len(rs.Segments))
	for key, entry := range rs.Segments {
		if strings.HasPrefix(key, "row:") || strings.HasPrefix(key, "path:") {
			continue
		}
		legacy[key] = entry
	}
	if len(legacy) == 0 {
		return
	}

	claim := func(seg render.Segment, oldPath string) {
		entry := legacy[oldPath]
		// The old key is where the file actually sits, which is what makes
		// it a usable rename source.
		entry.OutputPath = oldPath
		rs.Segments[SegmentKey(seg)] = entry
		delete(rs.Segments, oldPath)
		delete(legacy, oldPath)
	}

	pending := make([]render.Segment, 0, len(segments))
	for _, seg := range segments {
		if _, taken := rs.Segments[SegmentKey(seg)]; taken {
			continue
		}
		if _, ok := legacy[seg.OutputPath]; ok {
			claim(seg, seg.OutputPath)
			continue
		}
		pending = append(pending, seg)
	}

	bySource := make(map[string][]string, len(legacy))
	for key, entry := range legacy {
		if src := strings.TrimSpace(entry.SourcePath); src != "" {
			bySource[src] = append(bySource[src], key)
		}
	}
	for _, seg := range pending {
		keys := bySource[strings.TrimSpace(seg.CachedPath)]
		if len(keys) != 1 {
			continue
		}
		if _, still := legacy[keys[0]]; !still {
			continue
		}
		claim(seg, keys[0])
	}
}

// Load reads render state from the given path. A missing or corrupt file
// returns an empty state without error.
func Load(path string) (*RenderState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return emptyState(), nil
	}

	var rs RenderState
	if err := json.Unmarshal(data, &rs); err != nil {
		return emptyState(), nil
	}

	if rs.Segments == nil {
		rs.Segments = map[string]SegmentState{}
	}
	return &rs, nil
}

// Save writes the render state atomically to the given path.
func (rs *RenderState) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

func emptyState() *RenderState {
	return &RenderState{
		Segments: map[string]SegmentState{},
	}
}
