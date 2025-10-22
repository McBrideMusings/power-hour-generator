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
		OverlayProfile:  "default",
	}

	profileDef, ok := cfg.Profiles.Overlays["default"]
	if !ok {
		for name, def := range cfg.Profiles.Overlays {
			profileDef = def
			clip.OverlayProfile = name
			break
		}
	}

	segments := make([]config.OverlaySegment, len(profileDef.Segments))
	copy(segments, profileDef.Segments)

	profile := project.ResolvedProfile{
		Name:         clip.OverlayProfile,
		DefaultStyle: profileDef.DefaultStyle,
		Segments:     segments,
	}

	return Segment{
		Clip:       clip,
		Profile:    profile,
		Segments:   segments,
		SourcePath: "/tmp/source.mp4",
	}
}
