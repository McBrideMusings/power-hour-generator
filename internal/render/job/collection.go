// Package job owns render orchestration shared by the CLI and the TUI
// dashboard: preflight (build the segment), auto-fetch of missing
// URL-backed sources, hash-based change detection, the actual render, and
// render-state persistence. It cannot live in package render itself because
// internal/render/state already imports internal/render (for
// render.Segment) — a shared orchestrator inside render would create a
// render -> state -> render cycle. job sits beside both and imports each.
package job

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/state"
	"powerhour/pkg/csvplan"
)

// ErrMissingCachedSource sentinel-wraps a segment whose source has not been
// fetched yet but could be, if it is a URL. Use errors.Is against this value.
var ErrMissingCachedSource = errors.New("missing cached source")

// MissingCachedSourceError carries a row-specific message while still
// satisfying errors.Is(err, ErrMissingCachedSource).
type MissingCachedSourceError struct {
	Msg string
}

func (e MissingCachedSourceError) Error() string { return e.Msg }

func (e MissingCachedSourceError) Is(target error) bool {
	return target == ErrMissingCachedSource
}

// ResolveEntryForRow looks up the cache entry backing a plan row, whether the
// row's link is a remote URL or a local file path. It returns (entry, false,
// nil) when the row is understood but not yet cached — the caller decides
// whether that's fetchable.
func ResolveEntryForRow(pp paths.ProjectPaths, idx *cache.Index, row csvplan.Row) (cache.Entry, bool, error) {
	if idx == nil {
		return cache.Entry{}, false, fmt.Errorf("row %03d %q: cache index is nil", row.Index, row.Title)
	}

	link := strings.TrimSpace(row.Link)
	if link == "" {
		return cache.Entry{}, false, fmt.Errorf("row %03d missing link; update the plan and re-run", row.Index)
	}

	if parsed, err := url.Parse(link); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		key, exists := idx.LookupLink(link)
		if !exists {
			return cache.Entry{}, false, nil
		}
		entry, ok := idx.GetByIdentifier(key)
		if !ok || strings.TrimSpace(entry.CachedPath) == "" {
			return cache.Entry{}, false, nil
		}
		return entry, true, nil
	}

	path := link
	if !filepath.IsAbs(path) {
		path = filepath.Join(pp.Root, link)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return cache.Entry{}, false, fmt.Errorf("row %03d %q: resolve source path: %w", row.Index, row.Title, err)
	}

	entry, ok := idx.GetByIdentifier(abs)
	if !ok || strings.TrimSpace(entry.CachedPath) == "" {
		return cache.Entry{}, false, nil
	}

	return entry, true, nil
}

// BuildCollectionRenderSegment resolves a CollectionClip into a render.Segment,
// locating its source (local file or cached download). It returns a
// MissingCachedSourceError (matching ErrMissingCachedSource via errors.Is)
// when the source is understood but not yet available locally.
func BuildCollectionRenderSegment(pp paths.ProjectPaths, cfg config.Config, idx *cache.Index, collClip project.CollectionClip) (render.Segment, error) {
	clip := collClip.Clip

	clip.Row.DurationSeconds = clip.DurationSeconds
	if clip.Row.Index <= 0 {
		clip.Row.Index = clip.TypeIndex
		if clip.Row.Index <= 0 {
			clip.Row.Index = clip.Sequence
		}
	}

	segment := render.Segment{
		Clip:     clip,
		Overlays: collClip.Overlays,
	}

	outputDir := collClip.OutputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(pp.SegmentsDir, outputDir)
	}
	baseName := render.SegmentBaseName(cfg.SegmentFilenameTemplate(), segment)
	segment.OutputPath = filepath.Join(outputDir, baseName+".mp4")

	link := clip.Row.Link
	if !isRemoteLink(link) {
		trimmed := strings.Trim(link, "'\"")

		var sourcePath string
		if filepath.IsAbs(trimmed) {
			if _, err := os.Stat(trimmed); err == nil {
				sourcePath = trimmed
			} else {
				sourcePath = filepath.Join(pp.Root, strings.TrimPrefix(trimmed, string(filepath.Separator)))
			}
		} else {
			sourcePath = filepath.Join(pp.Root, trimmed)
		}

		if _, err := os.Stat(sourcePath); err != nil {
			if os.IsNotExist(err) {
				return segment, MissingCachedSourceError{
					Msg: fmt.Sprintf("local file not found: %s", sourcePath),
				}
			}
			return segment, fmt.Errorf("collection %q row %03d: stat local file: %w", collClip.CollectionName, clip.Row.Index, err)
		}

		segment.SourcePath = sourcePath
		segment.CachedPath = sourcePath
		return segment, nil
	}

	entry, ok, err := ResolveEntryForRow(pp, idx, clip.Row)
	if err != nil {
		return segment, err
	}
	if !ok {
		return segment, MissingCachedSourceError{
			Msg: "video not downloaded; may be unavailable or region-locked",
		}
	}

	segment.Entry = entry
	segment.SourcePath = entry.CachedPath
	segment.CachedPath = entry.CachedPath

	return segment, nil
}

// isRemoteLink is a cheap prefix check for "is this worth attempting a
// network fetch" — distinct from ResolveEntryForRow's stricter url.Parse
// scheme check, which decides how to look the entry up once we already know
// it's a URL.
func isRemoteLink(link string) bool {
	return strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "youtu")
}

// ClipDisplayTitle picks the best human-readable label for a clip: title,
// then name, then the media filename, then the clip type.
func ClipDisplayTitle(clip project.Clip) string {
	if title := strings.TrimSpace(clip.Row.Title); title != "" {
		return title
	}
	if name := strings.TrimSpace(clip.Row.Name); name != "" {
		return name
	}
	if clip.SourceKind == project.SourceKindMedia && strings.TrimSpace(clip.MediaPath) != "" {
		return filepath.Base(clip.MediaPath)
	}
	return string(clip.ClipType)
}

func preflightResult(clip project.Clip, err error) render.Result {
	return render.Result{
		Index:     clip.Sequence,
		ClipType:  clip.ClipType,
		TypeIndex: clip.TypeIndex,
		Title:     ClipDisplayTitle(clip),
		Err:       err,
	}
}

// CollectionJobParams describes one render pass over a set of resolved
// collection clips.
type CollectionJobParams struct {
	Paths         paths.ProjectPaths
	Config        config.Config
	Index         *cache.Index
	CacheService  *cache.Service // nil disables auto-fetch of missing URL sources
	RenderService *render.Service
	Clips         []project.CollectionClip
	Reporter      render.ProgressReporter // may be nil
	Force         bool
	Concurrency   int
	DryRun        bool // when true, skip the actual render.Service.Render call
}

// CollectionJobResult is the outcome of one RunCollectionJob call, with every
// slice aligned 1:1 to the input Clips slice (except Actions, which is
// aligned to the subset of clips whose source was available).
type CollectionJobResult struct {
	Results  []render.Result
	Segments []render.Segment
	Actions  []state.SegmentAction
	State    *state.RenderState
}

// RunCollectionJob performs the full render pipeline for a set of collection
// clips: preflight (locate each clip's source), auto-fetch any missing
// URL-backed sources, hash-based change detection against stored render
// state, the render itself (unless DryRun), and a render-state save. Both
// the CLI and the TUI dashboard call this so they can never drift again.
func RunCollectionJob(ctx context.Context, p CollectionJobParams) (CollectionJobResult, error) {
	clips := p.Clips
	segments := make([]render.Segment, len(clips))
	preflight := make([]render.Result, len(clips))
	shouldRender := make([]bool, len(clips))
	renderOrder := make([]int, 0, len(clips))

	for i, cc := range clips {
		segment, err := BuildCollectionRenderSegment(p.Paths, p.Config, p.Index, cc)
		segments[i] = segment
		if err != nil {
			if errors.Is(err, ErrMissingCachedSource) {
				preflight[i] = preflightResult(cc.Clip, err)
				if segment.OutputPath != "" {
					preflight[i].OutputPath = segment.OutputPath
				}
				continue
			}
			return CollectionJobResult{}, err
		}
		renderOrder = append(renderOrder, i)
		shouldRender[i] = true
	}

	// Identify missing sources that can be auto-fetched (URLs only).
	var missingIndices []int
	for i, res := range preflight {
		if res.Err != nil && errors.Is(res.Err, ErrMissingCachedSource) {
			if isRemoteLink(clips[i].Clip.Row.Link) {
				missingIndices = append(missingIndices, i)
			}
		}
	}
	fetchable := make(map[int]bool, len(missingIndices))
	for _, i := range missingIndices {
		fetchable[i] = true
	}

	// Report non-fetchable preflight errors right away — they will not
	// change during the fetch phase below.
	if p.Reporter != nil {
		for i, res := range preflight {
			if res.Err != nil && !fetchable[i] {
				p.Reporter.Complete(res)
			}
		}
	}

	if len(missingIndices) > 0 && p.CacheService != nil {
		dirty := false
		for _, i := range missingIndices {
			cc := clips[i]
			row := cc.Clip.Row

			if p.Reporter != nil {
				p.Reporter.Fetching(cc)
			}

			result, fetchErr := p.CacheService.Resolve(ctx, p.Index, row, cache.ResolveOptions{})
			if fetchErr != nil {
				if p.Reporter != nil {
					p.Reporter.FetchError(cc, fetchErr)
				}
				continue
			}
			if result.Updated {
				dirty = true
			}

			segment, buildErr := BuildCollectionRenderSegment(p.Paths, p.Config, p.Index, cc)
			segments[i] = segment
			if buildErr != nil {
				if errors.Is(buildErr, ErrMissingCachedSource) {
					preflight[i] = preflightResult(cc.Clip, buildErr)
					if p.Reporter != nil {
						p.Reporter.FetchError(cc, buildErr)
					}
					continue
				}
				return CollectionJobResult{}, buildErr
			}
			preflight[i] = render.Result{}
			renderOrder = append(renderOrder, i)
			shouldRender[i] = true

			if p.Reporter != nil {
				p.Reporter.Fetched(cc, segment)
			}
		}
		if dirty {
			if err := cache.Save(p.Paths, p.Index); err != nil {
				return CollectionJobResult{}, fmt.Errorf("save cache index after auto-fetch: %w", err)
			}
		}
	}

	// Sort renderOrder so valid segments are in clip-index order — results
	// are consumed sequentially by index, so the order must match.
	sort.Ints(renderOrder)

	valid := make([]render.Segment, 0, len(renderOrder))
	for _, i := range renderOrder {
		valid = append(valid, segments[i])
	}

	rs, err := state.Load(p.Paths.RenderStateFile)
	if err != nil {
		return CollectionJobResult{}, fmt.Errorf("load render state: %w", err)
	}

	filenameTemplate := p.Config.SegmentFilenameTemplate()

	// Wire stored hashes into segments for change detection.
	for i := range valid {
		if prior, ok := rs.Segments[valid[i].OutputPath]; ok {
			valid[i].StoredHash = prior.InputHash
		}
	}

	actions := state.DetectChanges(rs, valid, p.Config, filenameTemplate, p.Force)

	var toRender []render.Segment
	skip := make(map[string]render.Result)
	for i, a := range actions {
		seg := valid[i]
		if a.Action == state.ActionSkip {
			res := render.Result{
				Index:      seg.Clip.Sequence,
				ClipType:   seg.Clip.ClipType,
				TypeIndex:  seg.Clip.TypeIndex,
				Title:      ClipDisplayTitle(seg.Clip),
				OutputPath: seg.OutputPath,
				Skipped:    true,
				Reason:     a.Reason,
			}
			skip[seg.OutputPath] = res
			if p.Reporter != nil {
				p.Reporter.Complete(res)
			}
		} else {
			toRender = append(toRender, seg)
		}
	}

	if p.DryRun {
		return CollectionJobResult{
			Segments: segments,
			Actions:  actions,
			State:    rs,
		}, nil
	}

	var renderResults []render.Result
	if len(toRender) > 0 && p.RenderService != nil {
		renderResults = p.RenderService.Render(ctx, toRender, render.Options{
			Concurrency: p.Concurrency,
			Force:       p.Force,
			Reporter:    p.Reporter,
		})
	}

	fullResults := mergeResults(clips, preflight, shouldRender, renderResults, skip)

	rs.GlobalConfigHash = state.GlobalConfigHash(p.Config)
	segByPath := make(map[string]render.Segment, len(valid))
	for _, seg := range valid {
		segByPath[seg.OutputPath] = seg
	}
	for _, res := range fullResults {
		if !res.Skipped && res.Err == nil && res.OutputPath != "" {
			if seg, ok := segByPath[res.OutputPath]; ok {
				rs.Segments[res.OutputPath] = state.SegmentState{
					InputHash:  state.SegmentInputHash(seg, filenameTemplate),
					RenderedAt: time.Now(),
					SourcePath: seg.CachedPath,
					DurationS:  float64(seg.Clip.DurationSeconds),
				}
			}
		}
	}
	currentKeys := make(map[string]bool, len(valid))
	for _, seg := range valid {
		currentKeys[seg.OutputPath] = true
	}
	state.Prune(rs, currentKeys)
	if err := rs.Save(p.Paths.RenderStateFile); err != nil {
		return CollectionJobResult{}, fmt.Errorf("save render state: %w", err)
	}

	return CollectionJobResult{
		Results:  fullResults,
		Segments: segments,
		Actions:  actions,
		State:    rs,
	}, nil
}

// mergeResults merges preflight errors, change-detection skips, and actual
// render results into one results slice aligned to clips.
func mergeResults(clips []project.CollectionClip, preflight []render.Result, shouldRender []bool, renderResults []render.Result, skipResults map[string]render.Result) []render.Result {
	fullResults := make([]render.Result, len(clips))
	renderIdx := 0
	for i := range clips {
		if !shouldRender[i] {
			fullResults[i] = preflight[i]
			continue
		}
		outputPath := preflight[i].OutputPath
		if outputPath == "" {
			for path, sr := range skipResults {
				if sr.Index == clips[i].Clip.Sequence {
					outputPath = path
					break
				}
			}
		}
		if sr, ok := skipResults[outputPath]; ok {
			fullResults[i] = sr
			continue
		}
		if renderIdx < len(renderResults) {
			fullResults[i] = renderResults[renderIdx]
			renderIdx++
		}
	}
	return fullResults
}
