package project

import (
	"path/filepath"
	"strings"

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
