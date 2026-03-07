package render

import (
	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

func newTestSegment(cfg config.Config, row csvplan.Row) Segment {
	clip := project.Clip{
		Sequence:        row.Index,
		ClipType:        project.ClipTypeSong,
		TypeIndex:       row.Index,
		Row:             row,
		SourceKind:      project.SourceKindPlan,
		DurationSeconds: row.DurationSeconds,
		FadeInSeconds:   0.5,
		FadeOutSeconds:  0.5,
	}

	return Segment{
		Clip:       clip,
		Overlays:   []config.OverlayEntry{{Type: "song-info"}},
		SourcePath: "/tmp/source.mp4",
	}
}
