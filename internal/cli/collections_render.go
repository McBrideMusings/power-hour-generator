package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/playback"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/job"
	"powerhour/internal/render/state"
	"powerhour/internal/tui"
	"powerhour/pkg/csvplan"
)

var (
	renderCollection string
)

// addCollectionRenderFlags adds collection-specific flags to the render command.
func addCollectionRenderFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&renderCollection, "collection", "", "Render only the specified collection (omit to render all collections)")
}

// cacheResolverFor converts svc to job.CacheResolver, returning a true nil
// interface when svc is nil rather than a non-nil interface wrapping a nil
// *cache.Service. Assigning a nil *cache.Service directly into a
// job.CacheResolver-typed field triggers Go's typed-nil gotcha: the
// resulting interface value is non-nil (it carries type information), so a
// "!= nil" check on the interface passes even though the underlying pointer
// is nil. RunCollectionJob relies on "nil CacheService disables auto-fetch",
// so every call site must route through this helper instead of assigning
// *cache.Service directly.
func cacheResolverFor(svc *cache.Service) job.CacheResolver {
	if svc == nil {
		return nil
	}
	return svc
}

// runCollectionRender handles rendering for collections-based configuration.
func runCollectionRender(ctx context.Context, cmd *cobra.Command, pp paths.ProjectPaths, cfg config.Config) error {
	if len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	if err := ensureProjectDirs(pp); err != nil {
		return err
	}

	if err := pp.EnsureCollectionDirs(cfg); err != nil {
		return err
	}

	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		return err
	}

	// Resolve the playback order before any --collection/--index filtering:
	// a slot's position is a fact about the whole order, not about whichever
	// subset this invocation happens to render.
	order, _, err := playback.ResolveOrder(pp.Root, cfg, collections)
	if err != nil {
		return err
	}

	if renderCollection != "" {
		coll, ok := collections[renderCollection]
		if !ok {
			return fmt.Errorf("collection %q not found in configuration", renderCollection)
		}
		collections = map[string]project.Collection{renderCollection: coll}
	}

	if len(renderIndexArg) > 0 {
		for collName, coll := range collections {
			rows := make([]csvplan.Row, len(coll.Rows))
			for i, collRow := range coll.Rows {
				rows[i] = collRow.ToRow()
			}

			filtered, err := filterRowsByIndexArgs(rows, renderIndexArg)
			if err != nil {
				return fmt.Errorf("filter collection %q by index: %w", collName, err)
			}

			filteredCollRows := make([]csvplan.CollectionRow, len(filtered))
			for i, row := range filtered {
				for _, collRow := range coll.Rows {
					if collRow.ToRow().Index == row.Index {
						filteredCollRows[i] = collRow
						break
					}
				}
			}

			coll.Rows = filteredCollRows
			collections[collName] = coll
		}
	}

	// Render state is project-wide, so say what this invocation is entitled to
	// prune from it. Rendering a collection in full makes this pass the
	// authority on that collection's entries; an --index filter means it saw
	// only some of its rows and is the authority on nothing.
	pruneScope := state.PruneScope{}
	if len(renderIndexArg) == 0 {
		names := make([]string, 0, len(collections))
		for name := range collections {
			names = append(names, name)
		}
		pruneScope = state.PruneCollections(names...)
	}

	collectionClips, err := resolver.BuildCollectionClips(collections)
	if err != nil {
		return err
	}

	if len(collectionClips) == 0 {
		return fmt.Errorf("no clips to render in collections")
	}

	// Apply per-sequence-entry fade overrides to the specific clip ranges
	// consumed by each timeline entry. Uses the same cursor logic as
	// ResolveTimeline so that a collection appearing twice with different
	// fades affects only its own portion of clips.
	project.ApplySequenceEntryFades(cfg, collectionClips)
	collectionClips = playback.AnnotateClips(order, collections, collectionClips)

	// Cheap pre-scan (local file stat / index lookup only, no network) to
	// build a display-only segments slice for the initial TUI table and to
	// decide whether auto-fetch is needed at all. job.RunCollectionJob
	// redoes this preflight itself once the real job runs; recomputing it
	// is inexpensive since it does no network I/O.
	segments := make([]render.Segment, len(collectionClips))
	needsFetch := false
	for i, collClip := range collectionClips {
		segment, buildErr := job.BuildCollectionRenderSegment(pp, cfg, idx, collClip)
		segments[i] = segment
		if buildErr != nil && errors.Is(buildErr, job.ErrMissingCachedSource) {
			link := collClip.Clip.Row.Link
			if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "youtu") {
				needsFetch = true
			}
		}
	}

	// Create cache service if we need to auto-fetch (before TUI starts, since tool
	// detection is slow and we don't want it to happen inside the render callback).
	var cacheSvc *cache.Service
	if needsFetch {
		fetchLogger, fetchLogCloser, logErr := logx.New(pp)
		if logErr != nil {
			return logErr
		}
		defer fetchLogCloser.Close()

		var cacheErr error
		cacheSvc, cacheErr = newCacheServiceWithStatus(ctx, pp, fetchLogger, nil, nil)
		if cacheErr != nil {
			return fmt.Errorf("auto-fetch: %w", cacheErr)
		}
	}
	// cacheResolverFor converts cacheSvc to job.CacheResolver, preserving a
	// true nil interface when cacheSvc is nil. Assigning a nil *cache.Service
	// directly into the job.CacheResolver-typed CollectionJobParams.CacheService
	// field would produce a non-nil interface wrapping a nil pointer (Go's
	// typed-nil gotcha), which would defeat the "nil disables auto-fetch"
	// check in RunCollectionJob.
	cacheResolver := cacheResolverFor(cacheSvc)

	svc, err := render.NewService(ctx, pp, cfg, nil)
	if err != nil {
		return err
	}

	outWriter := cmd.OutOrStdout()
	mode := tui.DetectMode(outWriter, renderNoProgress, outputJSON)

	// In TUI mode, suppress render service stdout to avoid corrupting the display.
	if mode != tui.ModeTUI {
		svc.SetWriters(cmd.OutOrStdout(), nil)
	}

	// seqToKey is built from the full clip set (not just the render-order
	// subset) so the reporter can resolve keys for fetch-phase events, which
	// fire before the render order is known.
	seqToKey := make(map[int]string, len(collectionClips))
	for _, cc := range collectionClips {
		seqToKey[cc.Clip.Sequence] = collectionRenderKey(cc)
	}

	if renderDryRun {
		jobResult, jobErr := job.RunCollectionJob(ctx, job.CollectionJobParams{
			Paths:         pp,
			Config:        cfg,
			Index:         idx,
			CacheService:  cacheResolver,
			RenderService: svc,
			Clips:         collectionClips,
			Force:         renderForce,
			Concurrency:   renderConcurrency,
			DryRun:        true,
			PruneScope:    pruneScope,
		})
		if jobErr != nil {
			return jobErr
		}
		printDryRun(cmd, jobResult.Actions, outputJSON)
		return nil
	}

	var fullResults []render.Result
	var renderErr error

	if mode == tui.ModeTUI {
		fmt.Fprintf(outWriter, "Project: %s\n", pp.Root)
		model := buildCollectionRenderProgressModel(pp.Root, collectionClips, segments)

		err := tui.RunWithWork(outWriter, model, func(send func(tea.Msg)) {
			reporter := tui.NewRenderReporter(
				send,
				func(seg render.Segment) string {
					if key, ok := seqToKey[seg.Clip.Sequence]; ok {
						return key
					}
					return fmt.Sprintf("unknown:%d", seg.Clip.Sequence)
				},
				func(res render.Result) string {
					if key, ok := seqToKey[res.Index]; ok {
						return key
					}
					return fmt.Sprintf("unknown:%d", res.Index)
				},
				collectionRenderKey,
				func(seg render.Segment) map[string]string {
					return map[string]string{"STATUS": "queued"}
				},
				func(res render.Result) map[string]string {
					return collectionRenderResultFields(pp.Root, res)
				},
				func(cc project.CollectionClip) map[string]string {
					return map[string]string{"STATUS": "fetching"}
				},
				func(cc project.CollectionClip, seg render.Segment) map[string]string {
					source := "-"
					if seg.SourcePath != "" {
						source = filepath.Base(seg.SourcePath)
					}
					return map[string]string{"STATUS": "fetched", "SOURCE": source}
				},
				func(cc project.CollectionClip, err error) map[string]string {
					return map[string]string{"STATUS": "error", "SOURCE": "UNAVAILABLE"}
				},
			)

			jobResult, jobErr := job.RunCollectionJob(ctx, job.CollectionJobParams{
				Paths:         pp,
				Config:        cfg,
				Index:         idx,
				CacheService:  cacheResolver,
				RenderService: svc,
				Clips:         collectionClips,
				Reporter:      reporter,
				Force:         renderForce,
				Concurrency:   renderConcurrency,
				PruneScope:    pruneScope,
			})
			if jobErr != nil {
				renderErr = jobErr
				return
			}
			fullResults = jobResult.Results
			segments = jobResult.Segments
		})
		if err != nil {
			return err
		}
		if renderErr != nil {
			return renderErr
		}

		printCollectionRenderSummary(outWriter, fullResults)
	} else {
		// Non-TUI: fetch then render sequentially.
		jobResult, jobErr := job.RunCollectionJob(ctx, job.CollectionJobParams{
			Paths:         pp,
			Config:        cfg,
			Index:         idx,
			CacheService:  cacheResolver,
			RenderService: svc,
			Clips:         collectionClips,
			Force:         renderForce,
			Concurrency:   renderConcurrency,
			PruneScope:    pruneScope,
		})
		if jobErr != nil {
			return jobErr
		}
		fullResults = jobResult.Results
		segments = jobResult.Segments

		if mode == tui.ModeJSON {
			return writeCollectionRenderJSON(cmd, pp.Root, collectionClips, fullResults)
		}

		writeCollectionRenderTable(cmd, pp.Root, collectionClips, segments, fullResults)
	}

	if err := renderInlineFiles(ctx, pp, cfg, svc, renderForce); err != nil {
		return err
	}

	return printCollectionRenderErrors(cmd.ErrOrStderr(), collectionClips, fullResults)
}

// renderInlineFiles re-encodes inline file entries (SequenceEntry.File) to
// normalized MP4 segments under segments/__inline__/. Raw source files such as
// .webm cannot be stream-copied into an MP4 concat list; re-encoding ensures
// correct timestamps and container compatibility. Uses hash-based change
// detection: skips segments whose stored hash matches the computed hash and
// output file exists. Render state is persisted for inline segments.
func renderInlineFiles(ctx context.Context, pp paths.ProjectPaths, cfg config.Config, svc *render.Service, force bool) error {
	rs, _ := state.Load(pp.RenderStateFile)
	filenameTemplate := cfg.SegmentFilenameTemplate()
	var segments []render.Segment

	for seqIdx, entry := range cfg.Timeline.Sequence {
		if entry.File == "" {
			continue
		}

		sourcePath := entry.File
		if !filepath.IsAbs(sourcePath) {
			sourcePath = filepath.Join(pp.Root, sourcePath)
		}
		if _, err := os.Stat(sourcePath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("timeline sequence[%d] file %q: not found", seqIdx, entry.File)
			}
			return fmt.Errorf("timeline sequence[%d] file %q: %w", seqIdx, entry.File, err)
		}

		outPath := render.InlineSegmentPath(pp.SegmentsDir, seqIdx, sourcePath)

		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("create inline segments dir: %w", err)
		}

		fadeIn, fadeOut := config.ResolveFade(entry.Fade, entry.FadeIn, entry.FadeOut)
		clip := project.Clip{
			Sequence:       seqIdx + 1,
			ClipType:       project.ClipType("__inline__"),
			TypeIndex:      seqIdx,
			SourceKind:     project.SourceKindPlan,
			FadeInSeconds:  fadeIn,
			FadeOutSeconds: fadeOut,
			Row: csvplan.Row{
				Index: seqIdx + 1,
				Link:  sourcePath,
			},
		}
		seg := render.Segment{
			Clip:       clip,
			Overlays:   nil,
			SourcePath: sourcePath,
			CachedPath: sourcePath,
			OutputPath: outPath,
		}
		if prior, ok := rs.Segments[state.SegmentKey(seg)]; ok {
			seg.StoredHash = prior.InputHash
		}
		segments = append(segments, seg)
	}

	if len(segments) == 0 {
		return nil
	}

	results := svc.Render(ctx, segments, render.Options{Force: force})
	var errs []string
	for _, res := range results {
		if res.Err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(res.OutputPath), res.Err))
		}
		if !res.Skipped && res.Err == nil && res.OutputPath != "" {
			for _, seg := range segments {
				if seg.OutputPath == res.OutputPath {
					rs.Segments[state.SegmentKey(seg)] = state.SegmentState{
						InputHash:  state.SegmentInputHash(seg, filenameTemplate),
						RenderedAt: time.Now(),
						SourcePath: seg.CachedPath,
						OutputPath: seg.OutputPath,
					}
					break
				}
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("inline file render failed:\n  %s", strings.Join(errs, "\n  "))
	}
	_ = rs.Save(pp.RenderStateFile)
	return nil
}

func writeCollectionRenderJSON(cmd *cobra.Command, projectRoot string, clips []project.CollectionClip, results []render.Result) error {
	type clipResult struct {
		Collection string        `json:"collection"`
		Index      int           `json:"index"`
		Status     string        `json:"status"`
		OutputPath string        `json:"output_path"`
		Error      string        `json:"error,omitempty"`
		Result     render.Result `json:"result"`
	}

	output := struct {
		Project string       `json:"project"`
		Clips   []clipResult `json:"clips"`
	}{
		Project: projectRoot,
		Clips:   make([]clipResult, len(clips)),
	}

	for i, collClip := range clips {
		res := results[i]
		status := "success"
		errMsg := ""
		if res.Err != nil {
			status = "error"
			errMsg = res.Err.Error()
		}

		output.Clips[i] = clipResult{
			Collection: collClip.CollectionName,
			Index:      collClip.Clip.Row.Index,
			Status:     status,
			OutputPath: res.OutputPath,
			Error:      errMsg,
			Result:     res,
		}
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal render json: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func writeCollectionRenderTable(cmd *cobra.Command, projectRoot string, clips []project.CollectionClip, segments []render.Segment, results []render.Result) {
	fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\n", projectRoot)

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "COLLECTION\tINDEX\tSTATUS\tSOURCE\tOUTPUT")
	for i, collClip := range clips {
		res := results[i]
		status := "rendered"
		source := "-"
		outputPath := "-"

		if i < len(segments) {
			seg := segments[i]
			if seg.SourcePath != "" {
				source = filepath.Base(seg.SourcePath)
			}
			if seg.OutputPath != "" {
				relPath, err := filepath.Rel(projectRoot, seg.OutputPath)
				if err == nil && !strings.HasPrefix(relPath, "..") {
					outputPath = relPath
				} else {
					outputPath = seg.OutputPath
				}
			}
		}

		if res.Err != nil {
			status = "error"
			errMsg := res.Err.Error()
			if strings.Contains(errMsg, "not downloaded") ||
				strings.Contains(errMsg, "not found") {
				source = "MISSING"
			}
		} else if res.Skipped {
			status = "cached"
		}

		if res.OutputPath != "" {
			relPath, err := filepath.Rel(projectRoot, res.OutputPath)
			if err == nil && !strings.HasPrefix(relPath, "..") {
				outputPath = relPath
			} else {
				outputPath = res.OutputPath
			}
		}

		fmt.Fprintf(w, "%s\t%03d\t%s\t%s\t%s\n",
			collClip.CollectionName,
			collClip.Clip.Row.Index,
			status,
			source,
			outputPath,
		)
	}
	w.Flush()

	printCollectionRenderSummary(cmd.OutOrStdout(), results)
}

func printCollectionRenderSummary(w io.Writer, results []render.Result) {
	var rendered, skipped, failed int
	for _, res := range results {
		if res.Err != nil {
			failed++
		} else if res.Skipped {
			skipped++
		} else {
			rendered++
		}
	}
	fmt.Fprintf(w, "\nRendered: %d, Skipped: %d, Failed: %d\n", rendered, skipped, failed)
}

var collectionRenderColumns = []tui.Column{
	{Header: "COLLECTION", Width: 12},
	{Header: "INDEX", Width: 5},
	{Header: "STATUS", Width: 10},
	{Header: "SOURCE", Width: 20, Flex: true},
	{Header: "OUTPUT", Width: 30, Flex: true},
}

func buildCollectionRenderProgressModel(projectRoot string, clips []project.CollectionClip, segments []render.Segment) tui.ProgressModel {
	model := tui.NewProgressModel("render", collectionRenderColumns)
	for i, cc := range clips {
		key := collectionRenderKey(cc)
		source := "-"
		output := "-"
		if i < len(segments) {
			seg := segments[i]
			if seg.SourcePath != "" {
				source = filepath.Base(seg.SourcePath)
			}
			if seg.OutputPath != "" {
				relPath, err := filepath.Rel(projectRoot, seg.OutputPath)
				if err == nil && !strings.HasPrefix(relPath, "..") {
					output = relPath
				} else {
					output = filepath.Base(seg.OutputPath)
				}
			}
		}
		model.AddRow(key, []string{
			cc.CollectionName,
			fmt.Sprintf("%03d", cc.Clip.Row.Index),
			"pending",
			source,
			output,
		})
	}
	return model
}

func collectionRenderResultFields(projectRoot string, res render.Result) map[string]string {
	fields := make(map[string]string)

	if res.Err != nil {
		fields["STATUS"] = "error"
		errMsg := res.Err.Error()
		if strings.Contains(errMsg, "not downloaded") ||
			strings.Contains(errMsg, "not found") {
			fields["SOURCE"] = "MISSING"
		}
	} else if res.Skipped {
		fields["STATUS"] = "cached"
	} else {
		fields["STATUS"] = "rendered"
	}

	if res.OutputPath != "" {
		relPath, err := filepath.Rel(projectRoot, res.OutputPath)
		if err == nil && !strings.HasPrefix(relPath, "..") {
			fields["OUTPUT"] = relPath
		} else {
			fields["OUTPUT"] = filepath.Base(res.OutputPath)
		}
	}

	return fields
}

func printDryRun(cmd *cobra.Command, actions []state.SegmentAction, jsonOutput bool) {
	if jsonOutput {
		type jsonAction struct {
			Index  int    `json:"index"`
			Title  string `json:"title"`
			Action string `json:"action"`
			Reason string `json:"reason"`
			Output string `json:"output"`
		}
		var out []jsonAction
		for _, a := range actions {
			out = append(out, jsonAction{
				Index:  a.Segment.Clip.Sequence,
				Title:  job.ClipDisplayTitle(a.Segment.Clip),
				Action: a.Action,
				Reason: a.Reason,
				Output: a.Segment.OutputPath,
			})
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return
	}

	var renderCount, skipCount, renameCount int
	for _, a := range actions {
		switch a.Action {
		case state.ActionRender:
			renderCount++
		case state.ActionRename:
			renameCount++
		default:
			skipCount++
		}
	}

	// A rename is called out separately from a skip because it is the whole
	// difference between reordering a project and re-encoding it.
	fmt.Fprintf(cmd.OutOrStdout(), "DRY RUN: %d segments would be rendered, %d renamed, %d skipped\n\n", renderCount, renameCount, skipCount)
	for _, a := range actions {
		tag := "SKIP  "
		switch a.Action {
		case state.ActionRender:
			tag = "RENDER"
		case state.ActionRename:
			tag = "RENAME"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  %03d  %-20s  (%s)\n",
			tag, a.Segment.Clip.Sequence, job.ClipDisplayTitle(a.Segment.Clip), a.Reason)
	}
}

func collectionRenderKey(cc project.CollectionClip) string {
	return fmt.Sprintf("%s:%03d", cc.CollectionName, cc.Clip.Row.Index)
}

// printCollectionRenderErrors prints a concise error summary after the results,
// then returns a non-nil error so the process exits with a failure code.
func printCollectionRenderErrors(w io.Writer, clips []project.CollectionClip, results []render.Result) error {
	var lines []string
	for i, res := range results {
		if res.Err == nil {
			continue
		}
		cc := clips[i]
		lines = append(lines, fmt.Sprintf("  %03d - %s", cc.Clip.Row.Index, res.Err))
	}
	if len(lines) > 0 {
		fmt.Fprintln(w)
		for _, line := range lines {
			fmt.Fprintln(w, line)
		}
		return fmt.Errorf("%d segment(s) failed to render", len(lines))
	}
	return nil
}
