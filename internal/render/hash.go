package render

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"powerhour/internal/config"
)

// fieldEntry captures a single custom field for deterministic ordering.
type fieldEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// segmentInput is the canonical structure hashed for per-segment changes.
type segmentInput struct {
	Link            string                `json:"link"`
	StartRaw        string                `json:"start_raw"`
	DurationSeconds int                   `json:"duration_seconds"`
	Title           string                `json:"title"`
	Artist          string                `json:"artist"`
	Name            string                `json:"name"`
	CustomFields    []fieldEntry          `json:"custom_fields"`
	FadeInSeconds   float64               `json:"fade_in_seconds"`
	FadeOutSeconds  float64               `json:"fade_out_seconds"`
	Overlays        []config.OverlayEntry `json:"overlays"`
	Template        string                `json:"template"`
	// PlaybackPosition is set only when the segment's overlay set actually
	// renders the index token (OverlaysDependOnIndex). A clip whose pixels do
	// not change when it moves must not change hash when it moves, or the
	// rename path in internal/render/state can never fire and every reorder
	// re-encodes the whole project.
	PlaybackPosition int `json:"playback_position,omitempty"`
}

// SegmentInputHash returns a deterministic hash of all render-relevant inputs
// for a single segment.
func SegmentInputHash(seg Segment, filenameTemplate string) string {
	var fields []fieldEntry
	for k, v := range seg.Clip.Row.CustomFields {
		if k == "label" {
			// Presentational only (display templates) — never invalidates a segment.
			continue
		}
		fields = append(fields, fieldEntry{Key: k, Value: v})
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Key < fields[j].Key
	})

	input := segmentInput{
		Link:            seg.Clip.Row.Link,
		StartRaw:        seg.Clip.Row.StartRaw,
		DurationSeconds: seg.Clip.DurationSeconds,
		Title:           seg.Clip.Row.Title,
		Artist:          seg.Clip.Row.Artist,
		Name:            seg.Clip.Row.Name,
		CustomFields:    fields,
		FadeInSeconds:   seg.Clip.FadeInSeconds,
		FadeOutSeconds:  seg.Clip.FadeOutSeconds,
		Overlays:        seg.Overlays,
		Template:        filenameTemplate,
	}
	if seg.Clip.PlaybackPosition > 0 &&
		OverlaysDependOnIndex(seg.Overlays, seg.Clip.Row, float64(seg.Clip.DurationSeconds)) {
		input.PlaybackPosition = seg.Clip.PlaybackPosition
	}
	return HashJSON(input)
}

// HashJSON returns a deterministic SHA256 hash of the JSON encoding of v.
func HashJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("sha256:error-%v", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum)
}
