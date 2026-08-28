package render

import (
	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

// EffectiveRow returns the row as the overlay and filename-template layer
// sees it: its Index is the clip's playback position when the clip has one.
//
// The substitution happens here rather than in project.RenderRowTemplate
// because that function also renders on-screen labels, where "index" still
// means the plan row's own line number.
func EffectiveRow(clip project.Clip) csvplan.Row {
	row := clip.Row
	if clip.PlaybackPosition > 0 {
		row.Index = clip.PlaybackPosition
	}
	return row
}

// OverlaysDependOnIndex reports whether an overlay set's rendered output
// actually changes when the index token changes.
//
// It answers the question by probing rather than by consulting a table of
// which preset reads which token: the overlay set is expanded twice against
// the same row with two different index values, and the results compared. A
// preset that never renders {index} — or one whose options turned its number
// badge off, or a custom filter that happens to interpolate it — is
// classified correctly without any of them being named here (ADR 0002).
//
// The caller uses this to decide whether a segment's hash may depend on its
// playback position. A segment whose pixels do not change when it moves must
// not have its hash change either, or the rename path in
// internal/render/state can never fire.
func OverlaysDependOnIndex(overlays []config.OverlayEntry, row csvplan.Row, clipDuration float64) bool {
	if len(overlays) == 0 {
		return false
	}

	probeA := row
	probeA.Index = indexProbeA
	probeB := row
	probeB.Index = indexProbeB

	a := ExpandOverlays(overlays, probeA, clipDuration)
	b := ExpandOverlays(overlays, probeB, clipDuration)

	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

// Two index values that share no digits, so a probe cannot come out equal by
// a preset zero-padding or truncating them into the same string.
const (
	indexProbeA = 1
	indexProbeB = 27
)
