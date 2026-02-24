package state

import (
	"os"

	"powerhour/internal/config"
	"powerhour/internal/render"
)

const (
	ActionRender = "render"
	ActionSkip   = "skip"

	ReasonForced        = "forced"
	ReasonNew           = "new segment"
	ReasonConfigChanged = "config changed"
	ReasonInputChanged  = "input changed"
	ReasonOutputMissing = "output missing"
	ReasonUpToDate      = "up to date"
)

// SegmentAction describes the action to take for a single segment.
type SegmentAction struct {
	Segment render.Segment
	Action  string
	Reason  string
}

// DetectChanges determines which segments need re-rendering by comparing
// current inputs against the stored render state.
func DetectChanges(rs *RenderState, segments []render.Segment, cfg config.Config, filenameTemplate string, force bool) []SegmentAction {
	actions := make([]SegmentAction, len(segments))

	if force {
		for i, seg := range segments {
			actions[i] = SegmentAction{Segment: seg, Action: ActionRender, Reason: ReasonForced}
		}
		return actions
	}

	currentGlobalHash := GlobalConfigHash(cfg)
	if currentGlobalHash != rs.GlobalConfigHash {
		for i, seg := range segments {
			actions[i] = SegmentAction{Segment: seg, Action: ActionRender, Reason: ReasonConfigChanged}
		}
		return actions
	}

	for i, seg := range segments {
		key := seg.OutputPath
		prior, exists := rs.Segments[key]
		if !exists {
			actions[i] = SegmentAction{Segment: seg, Action: ActionRender, Reason: ReasonNew}
			continue
		}

		currentHash := SegmentInputHash(seg, filenameTemplate)
		if currentHash != prior.InputHash {
			actions[i] = SegmentAction{Segment: seg, Action: ActionRender, Reason: ReasonInputChanged}
			continue
		}

		if _, err := os.Stat(key); os.IsNotExist(err) {
			actions[i] = SegmentAction{Segment: seg, Action: ActionRender, Reason: ReasonOutputMissing}
			continue
		}

		actions[i] = SegmentAction{Segment: seg, Action: ActionSkip, Reason: ReasonUpToDate}
	}

	return actions
}

// Prune removes entries from the render state that are not in the current
// set of segment keys.
func Prune(rs *RenderState, currentKeys map[string]bool) {
	for key := range rs.Segments {
		if !currentKeys[key] {
			delete(rs.Segments, key)
		}
	}
}
