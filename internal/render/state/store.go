package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// SegmentState tracks the render inputs and output for a single segment.
type SegmentState struct {
	InputHash  string    `json:"input_hash"`
	RenderedAt time.Time `json:"rendered_at"`
	SourcePath string    `json:"source_path"`
	DurationS  float64   `json:"duration_s"`
}

// RenderState tracks render state across all segments for change detection.
type RenderState struct {
	GlobalConfigHash string                  `json:"global_config_hash"`
	Segments         map[string]SegmentState `json:"segments"`
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
