package state

import (
	"powerhour/internal/config"
	"powerhour/internal/render"
)

// globalConfigInput is the canonical structure hashed for global config changes.
type globalConfigInput struct {
	Video    config.VideoConfig    `json:"video"`
	Audio    config.AudioConfig    `json:"audio"`
	Encoding config.EncodingConfig `json:"encoding"`
}

// GlobalConfigHash returns a deterministic hash of the video, audio, and
// encoding configuration sections.
func GlobalConfigHash(cfg config.Config) string {
	input := globalConfigInput{
		Video:    cfg.Video,
		Audio:    cfg.Audio,
		Encoding: cfg.Encoding,
	}
	return render.HashJSON(input)
}

// SegmentInputHash delegates to render.SegmentInputHash.
func SegmentInputHash(seg render.Segment, filenameTemplate string) string {
	return render.SegmentInputHash(seg, filenameTemplate)
}
