package project

import (
	"path/filepath"
	"strings"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

// ClipType identifies a configured clip category such as song or interstitial.
type ClipType string

const (
	ClipTypeSong         ClipType = "song"
	ClipTypeInterstitial ClipType = "interstitial"
	ClipTypeIntro        ClipType = "intro"
	ClipTypeOutro        ClipType = "outro"
)

// ClipSourceKind conveys how a clip type is sourced.
type ClipSourceKind string

const (
	SourceKindUnknown ClipSourceKind = ""
	SourceKindPlan    ClipSourceKind = "plan"
	SourceKindMedia   ClipSourceKind = "media"
)

// ResolvedProfile represents an overlay profile ready for use.
type ResolvedProfile struct {
	Name               string
	DefaultStyle       config.TextStyle
	Segments           []config.OverlaySegment
	DefaultDurationSec *int
	FadeInSec          *float64
	FadeOutSec         *float64
}

// ResolveSegments returns a clone of the profile's overlay segments.
func (rp ResolvedProfile) ResolveSegments() []config.OverlaySegment {
	return cloneSegments(rp.Segments)
}

// Clip models a single entry in the resolved render timeline.
type Clip struct {
	Sequence        int
	ClipType        ClipType
	TypeIndex       int
	Row             csvplan.Row
	SourceKind      ClipSourceKind
	MediaPath       string
	DurationSeconds int
	FadeInSeconds   float64
	FadeOutSeconds  float64
	OverlayProfile  string
}

func cloneProfile(name string, profile config.OverlayProfile) ResolvedProfile {
	return ResolvedProfile{
		Name:               name,
		DefaultStyle:       cloneTextStyle(profile.DefaultStyle),
		Segments:           cloneSegments(profile.Segments),
		DefaultDurationSec: copyIntPtr(profile.DefaultDurationSec),
		FadeInSec:          copyFloatPtr(profile.FadeInSec),
		FadeOutSec:         copyFloatPtr(profile.FadeOutSec),
	}
}

func profileExists(profiles map[string]ResolvedProfile, name string) bool {
	_, ok := profiles[strings.TrimSpace(name)]
	return ok
}

func resolveProjectPath(root, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, value)
}

func cloneTextStyle(style config.TextStyle) config.TextStyle {
	clone := style
	if style.FontSize != nil {
		value := *style.FontSize
		clone.FontSize = &value
	}
	if style.OutlineWidth != nil {
		value := *style.OutlineWidth
		clone.OutlineWidth = &value
	}
	if style.LineSpacing != nil {
		value := *style.LineSpacing
		clone.LineSpacing = &value
	}
	if style.LetterSpacing != nil {
		value := *style.LetterSpacing
		clone.LetterSpacing = &value
	}
	return clone
}

func cloneSegments(segments []config.OverlaySegment) []config.OverlaySegment {
	if len(segments) == 0 {
		return nil
	}
	clones := make([]config.OverlaySegment, len(segments))
	for i, segment := range segments {
		clones[i] = segment
		clones[i].Style = cloneTextStyle(segment.Style)
	}
	return clones
}

func copyIntPtr(src *int) *int {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func copyFloatPtr(src *float64) *float64 {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

