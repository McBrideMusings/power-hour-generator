package state

import (
	"os"
	"path/filepath"
	"strings"

	"powerhour/internal/config"
	"powerhour/internal/render"
)

const (
	ActionRender = "render"
	ActionSkip   = "skip"
	// ActionRename means the segment's inputs are unchanged but its output
	// filename is not where it was last written — the row moved in the
	// playback order and the filename template embeds the position. Moving
	// the existing file into place is correct and costs no ffmpeg run.
	ActionRename = "rename"

	ReasonForced        = "forced"
	ReasonNew           = "new segment"
	ReasonConfigChanged = "config changed"
	ReasonInputChanged  = "input changed"
	ReasonOutputMissing = "output missing"
	ReasonUpToDate      = "up to date"
	ReasonMoved         = "moved"
)

// SegmentAction describes the action to take for a single segment.
type SegmentAction struct {
	Segment render.Segment
	Action  string
	Reason  string
	// RenameFrom is the existing file to move to Segment.OutputPath. Set
	// only when Action is ActionRename.
	RenameFrom string
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
		prior, exists := rs.Segments[SegmentKey(seg)]
		if !exists {
			actions[i] = SegmentAction{Segment: seg, Action: ActionRender, Reason: ReasonNew}
			continue
		}

		currentHash := SegmentInputHash(seg, filenameTemplate)
		if currentHash != prior.InputHash {
			actions[i] = SegmentAction{Segment: seg, Action: ActionRender, Reason: ReasonInputChanged}
			continue
		}

		if fileExists(seg.OutputPath) {
			actions[i] = SegmentAction{Segment: seg, Action: ActionSkip, Reason: ReasonUpToDate}
			continue
		}

		// The inputs are unchanged and the output is not where it is
		// expected — but it may be sitting under the name the last render
		// gave it. Moving it is the whole point of keying state by row id.
		if prior.OutputPath != "" && prior.OutputPath != seg.OutputPath && fileExists(prior.OutputPath) {
			actions[i] = SegmentAction{
				Segment:    seg,
				Action:     ActionRename,
				Reason:     ReasonMoved,
				RenameFrom: prior.OutputPath,
			}
			continue
		}

		actions[i] = SegmentAction{Segment: seg, Action: ActionRender, Reason: ReasonOutputMissing}
	}

	return actions
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// ApplyRenames moves each ActionRename segment's file into its new output
// path, rewriting in place any action it could not carry out as a render.
//
// The moves run in two phases — every source to a temp name, then every temp
// name to its destination — because a reorder routinely produces a cycle: two
// songs swapping positions each want the filename the other currently holds,
// and a single-phase move would destroy one of them.
//
// A rename that fails is never fatal: that segment simply gets encoded.
func ApplyRenames(actions []SegmentAction) {
	type pending struct {
		idx int
		tmp string
		dst string
	}

	var staged []pending
	for i := range actions {
		if actions[i].Action != ActionRename {
			continue
		}
		dst := actions[i].Segment.OutputPath
		tmp := actions[i].RenameFrom + ".rename.tmp"
		if err := os.Rename(actions[i].RenameFrom, tmp); err != nil {
			actions[i] = SegmentAction{Segment: actions[i].Segment, Action: ActionRender, Reason: ReasonOutputMissing}
			continue
		}
		staged = append(staged, pending{idx: i, tmp: tmp, dst: dst})
	}

	for _, p := range staged {
		if err := os.MkdirAll(filepath.Dir(p.dst), 0o755); err != nil {
			os.Rename(p.tmp, actions[p.idx].RenameFrom)
			actions[p.idx] = SegmentAction{Segment: actions[p.idx].Segment, Action: ActionRender, Reason: ReasonOutputMissing}
			continue
		}
		if err := os.Rename(p.tmp, p.dst); err != nil {
			os.Rename(p.tmp, actions[p.idx].RenameFrom)
			actions[p.idx] = SegmentAction{Segment: actions[p.idx].Segment, Action: ActionRender, Reason: ReasonOutputMissing}
		}
	}
}

// PruneScope names the part of the render state a pass is authoritative for.
//
// Render state is project-wide; a render pass usually is not. The keep-set
// alone cannot express the difference — "this key was not in the pass" and
// "this key is not the pass's business" both look like absence — so the
// caller, which knows its own scope, states it here. The zero value is
// authoritative for nothing and prunes nothing, which is the right answer
// for any pass that saw a filtered subset of its input.
type PruneScope struct {
	// All marks a pass authoritative for the whole project: every key it did
	// not keep is stale and may be deleted.
	All bool
	// ClipTypes are the collections the pass saw in full. Only keys naming
	// one of them may be pruned; every other collection's entries, and every
	// path-keyed inline entry, survive untouched.
	ClipTypes map[string]bool
}

// PruneAll returns the scope of a pass that resolved the entire project.
func PruneAll() PruneScope {
	return PruneScope{All: true}
}

// PruneCollections returns the scope of a pass that rendered each named
// collection in full. Pass no names for a pass authoritative for nothing.
func PruneCollections(names ...string) PruneScope {
	if len(names) == 0 {
		return PruneScope{}
	}
	types := make(map[string]bool, len(names))
	for _, name := range names {
		types[name] = true
	}
	return PruneScope{ClipTypes: types}
}

// owns reports whether this scope is entitled to delete the given key.
func (s PruneScope) owns(key string) bool {
	if s.All {
		return true
	}
	clipType, ok := clipTypeOfKey(key)
	return ok && s.ClipTypes[clipType]
}

// clipTypeOfKey extracts the collection name from a row-keyed state entry.
// A path-keyed entry (an inline file:, which belongs to no collection)
// yields no clip type and so is owned only by a whole-project scope.
func clipTypeOfKey(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, "row:")
	if !ok {
		return "", false
	}
	clipType, _, ok := strings.Cut(rest, "/")
	if !ok || clipType == "" {
		return "", false
	}
	return clipType, true
}

// Prune removes stale entries from the render state: those absent from keep
// AND inside the caller's authority per scope.
//
// An entry whose file was renamed rather than removed is still current — its
// key is a row identity, which the rename did not change — so it survives
// here exactly as an untouched entry does.
func Prune(rs *RenderState, keep map[string]bool, scope PruneScope) {
	if rs == nil {
		return
	}
	for key := range rs.Segments {
		if keep[key] || !scope.owns(key) {
			continue
		}
		delete(rs.Segments, key)
	}
}
