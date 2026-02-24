package state

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"powerhour/internal/config"
	"powerhour/internal/render"
)

// globalConfigInput is the canonical structure hashed for global config changes.
type globalConfigInput struct {
	Video    config.VideoConfig    `json:"video"`
	Audio    config.AudioConfig    `json:"audio"`
	Encoding config.EncodingConfig `json:"encoding"`
}

// fieldEntry captures a single custom field for deterministic ordering.
type fieldEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// segmentInput is the canonical structure hashed for per-segment changes.
type segmentInput struct {
	Link            string                  `json:"link"`
	StartRaw        string                  `json:"start_raw"`
	DurationSeconds int                     `json:"duration_seconds"`
	Title           string                  `json:"title"`
	Artist          string                  `json:"artist"`
	Name            string                  `json:"name"`
	CustomFields    []fieldEntry            `json:"custom_fields"`
	FadeInSeconds   float64                 `json:"fade_in_seconds"`
	FadeOutSeconds  float64                 `json:"fade_out_seconds"`
	ProfileName     string                  `json:"profile_name"`
	DefaultStyle    config.TextStyle        `json:"default_style"`
	Segments        []config.OverlaySegment `json:"segments"`
	Template        string                  `json:"template"`
}

// GlobalConfigHash returns a deterministic hash of the video, audio, and
// encoding configuration sections.
func GlobalConfigHash(cfg config.Config) string {
	input := globalConfigInput{
		Video:    cfg.Video,
		Audio:    cfg.Audio,
		Encoding: cfg.Encoding,
	}
	return hashJSON(input)
}

// SegmentInputHash returns a deterministic hash of all render-relevant inputs
// for a single segment.
func SegmentInputHash(seg render.Segment, filenameTemplate string) string {
	// Sort custom fields for deterministic ordering
	var fields []fieldEntry
	for k, v := range seg.Clip.Row.CustomFields {
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
		ProfileName:     seg.Profile.Name,
		DefaultStyle:    seg.Profile.DefaultStyle,
		Segments:        seg.Segments,
		Template:        filenameTemplate,
	}
	return hashJSON(input)
}

func hashJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		// Should never happen with known struct types.
		return fmt.Sprintf("sha256:error-%v", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum)
}
