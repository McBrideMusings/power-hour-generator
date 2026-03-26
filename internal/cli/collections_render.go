package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
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

// runCollectionRender handles rendering for collections-based configuration.
func runCollectionRender(ctx context.Context, cmd *cobra.Command, pp paths.ProjectPaths, cfg config.Config) error {
	if cfg.Collections == nil || len(cfg.Collections) == 0 {
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

	collectionClips, err := resolver.BuildCollectionClips(collections)
	if err != nil {
		return err
	}

	if len(collectionClips) == 0 {
		return fmt.Errorf("no clips to render in collections")
	}

	segments := make([]render.Segment, len(collectionClips))
	renderOrder := make([]int, 0, len(collectionClips))
	preflight := make([]render.Result, len(collectionClips))
	shouldRender := make([]bool, len(collectionClips))

	for i, collClip := range collectionClips {
		segment, err := buildCollectionRenderSegment(pp, cfg, idx, resolver, collClip)
		segments[i] = segment

		if err != nil {
			if errors.Is(err, errMissingCachedSource) {
				preflight[i] = renderPreflightResult(collClip.Clip, err)
				if segment.OutputPath != "" {
					preflight[i].OutputPath = segment.OutputPath
				}
				continue
			}
			return err
		}
		renderOrder = append(renderOrder, i)
		shouldRender[i] = true
	}

	// Identify missing sources that can be auto-fetched (URLs only).
	var missingIndices []int
	for i, res := range preflight {
		if res.Err != nil && errors.Is(res.Err, errMissingCachedSource) {
			link := collectionClips[i].Clip.Row.Link
			if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "youtu") {
				missingIndices = append(missingIndices, i)
			}
		}
	}

	// Create cache service if we need to auto-fetch (before TUI starts, since tool
	// detection is slow and we don't want it to happen inside the render callback).
	var cacheSvc *cache.Service
	var fetchLogger *log.Logger
	var fetchLogCloser io.Closer
	if len(missingIndices) > 0 {
		var logErr error
		fetchLogger, fetchLogCloser, logErr = logx.New(pp)
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

	// autoFetchAndRebuild fetches missing sources, re-runs preflight for fetched clips,
	// then rebuilds validSegments, change detection, and toRender/skipResults.
	// If send is non-nil, it sends TUI row updates for each fetch.
	autoFetchAndRebuild := func(send func(tea.Msg)) ([]render.Segment, []render.Segment, map[string]render.Result, *state.RenderState, error) {
		if len(missingIndices) > 0 && cacheSvc != nil {
			opts := cache.ResolveOptions{}
			dirty := false
			for _, i := range missingIndices {
				cc := collectionClips[i]
				row := cc.Clip.Row
				key := collectionRenderKey(cc)

				if send != nil {
					send(tui.RowUpdateMsg{
						Key:    key,
						Fields: map[string]string{"STATUS": "fetching"},
					})
				}

				result, fetchErr := cacheSvc.Resolve(ctx, idx, row, opts)
				if fetchErr != nil {
					fetchLogger.Printf("auto-fetch collection=%s row %03d failed: %v", cc.CollectionName, row.Index, fetchErr)
					if send != nil {
						send(tui.RowUpdateMsg{
							Key:    key,
							Fields: map[string]string{"STATUS": "error", "SOURCE": "UNAVAILABLE"},
						})
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "fetch %s #%03d failed: %v\n", cc.CollectionName, row.Index, fetchErr)
					}
					continue
				}
				if result.Updated {
					dirty = true
				}

				// Re-run preflight for this clip.
				segment, buildErr := buildCollectionRenderSegment(pp, cfg, idx, resolver, cc)
				segments[i] = segment
				if buildErr != nil {
					if errors.Is(buildErr, errMissingCachedSource) {
						continue
					}
					return nil, nil, nil, nil, buildErr
				}
				preflight[i] = render.Result{}
				renderOrder = append(renderOrder, i)
				shouldRender[i] = true

				if send != nil {
					source := "-"
					if segment.SourcePath != "" {
						source = filepath.Base(segment.SourcePath)
					}
					send(tui.RowUpdateMsg{
						Key:    key,
						Fields: map[string]string{"STATUS": "fetched", "SOURCE": source},
					})
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "fetched %s #%03d\n", cc.CollectionName, row.Index)
				}
			}
			if dirty {
				if saveErr := cache.Save(pp, idx); saveErr != nil {
					return nil, nil, nil, nil, fmt.Errorf("save cache index after auto-fetch: %w", saveErr)
				}
			}
		}

		// Sort renderOrder so valid segments are in clip-index order.
		// mergeCollectionRenderResultsWithSkips iterates clips 0..N and
		// consumes render results sequentially, so the order must match.
		sort.Ints(renderOrder)

		// Rebuild valid segments list
		valid := make([]render.Segment, 0, len(renderOrder))
		for _, idx := range renderOrder {
			valid = append(valid, segments[idx])
		}

		// Change detection
		rs, loadErr := state.Load(pp.RenderStateFile)
		if loadErr != nil {
			return nil, nil, nil, nil, fmt.Errorf("load render state: %w", loadErr)
		}

		filenameTemplate := cfg.SegmentFilenameTemplate()
		actions := state.DetectChanges(rs, valid, cfg, filenameTemplate, renderForce)

		var toRender []render.Segment
		skip := make(map[string]render.Result)
		for i, a := range actions {
			seg := valid[i]
			if a.Action == state.ActionSkip {
				skip[seg.OutputPath] = render.Result{
					Index:      seg.Clip.Sequence,
					ClipType:   seg.Clip.ClipType,
					TypeIndex:  seg.Clip.TypeIndex,
					Title:      clipDisplayTitle(seg.Clip),
					OutputPath: seg.OutputPath,
					Skipped:    true,
					Reason:     a.Reason,
				}
			} else {
				toRender = append(toRender, seg)
			}
		}

		return valid, toRender, skip, rs, nil
	}

	if renderDryRun {
		validSegments, _, _, rs, buildErr := autoFetchAndRebuild(nil)
		if buildErr != nil {
			return buildErr
		}
		filenameTemplate := cfg.SegmentFilenameTemplate()
		actions := state.DetectChanges(rs, validSegments, cfg, filenameTemplate, renderForce)
		printDryRun(cmd, actions, outputJSON)
		return nil
	}

	var fullResults []render.Result

	if mode == tui.ModeTUI {
		fmt.Fprintf(outWriter, "Project: %s\n", pp.Root)
		model := buildCollectionRenderProgressModel(pp.Root, collectionClips, segments)

		// Build set of fetchable indices for quick lookup.
		fetchableSet := make(map[int]bool, len(missingIndices))
		for _, i := range missingIndices {
			fetchableSet[i] = true
		}

		err := tui.RunWithWork(outWriter, model, func(send func(tea.Msg)) {
			// Send non-fetchable preflight errors immediately so they show
			// as "error" rather than staying "pending" during the fetch phase.
			for i := range collectionClips {
				if preflight[i].Err != nil && !fetchableSet[i] {
					send(tui.RowUpdateMsg{
						Key:    collectionRenderKey(collectionClips[i]),
						Fields: collectionRenderResultFields(pp.Root, collectionClips[i], segments[i], preflight[i]),
					})
				}
			}

			// Phase 1: Auto-fetch missing sources
			validSegments, toRender, skipResults, rs, buildErr := autoFetchAndRebuild(send)
			if buildErr != nil {
				fullResults = mergeCollectionRenderResultsWithSkips(collectionClips, preflight, shouldRender, nil, nil)
				_ = rs
				_ = validSegments
				return
			}

			// Send preflight errors for clips that failed to fetch
			for i := range collectionClips {
				if preflight[i].Err != nil && fetchableSet[i] {
					send(tui.RowUpdateMsg{
						Key:    collectionRenderKey(collectionClips[i]),
						Fields: collectionRenderResultFields(pp.Root, collectionClips[i], segments[i], preflight[i]),
					})
				}
			}

			// Send skip results from change detection
			for _, clipIdx := range renderOrder {
				cc := collectionClips[clipIdx]
				seg := segments[clipIdx]
				if sr, ok := skipResults[seg.OutputPath]; ok {
					send(tui.RowUpdateMsg{
						Key:    collectionRenderKey(cc),
						Fields: collectionRenderResultFields(pp.Root, cc, seg, sr),
					})
				}
			}

			// Phase 2: Render
			seqToKey := make(map[int]string, len(collectionClips))
			for _, clipIdx := range renderOrder {
				cc := collectionClips[clipIdx]
				seqToKey[cc.Clip.Sequence] = collectionRenderKey(cc)
			}

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
				func(seg render.Segment) map[string]string {
					return map[string]string{"STATUS": "queued"}
				},
				func(res render.Result) map[string]string {
					for _, clipIdx := range renderOrder {
						cc := collectionClips[clipIdx]
						if cc.Clip.Sequence == res.Index {
							return collectionRenderResultFields(pp.Root, cc, segments[clipIdx], res)
						}
					}
					status := "rendered"
					if res.Err != nil {
						status = "error"
					} else if res.Skipped {
						status = "cached"
					}
					return map[string]string{"STATUS": status}
				},
			)

			var renderResults []render.Result
			if len(toRender) > 0 {
				renderResults = svc.Render(ctx, toRender, render.Options{
					Concurrency: renderConcurrency,
					Force:       renderForce,
					Reporter:    reporter,
				})
			}

			fullResults = mergeCollectionRenderResultsWithSkips(collectionClips, preflight, shouldRender, renderResults, skipResults)

			// Update render state
			rs.GlobalConfigHash = state.GlobalConfigHash(cfg)
			segByPath := make(map[string]render.Segment, len(validSegments))
			for _, seg := range validSegments {
				segByPath[seg.OutputPath] = seg
			}
			filenameTemplate := cfg.SegmentFilenameTemplate()
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
			currentKeys := make(map[string]bool, len(validSegments))
			for _, seg := range validSegments {
				currentKeys[seg.OutputPath] = true
			}
			state.Prune(rs, currentKeys)
			_ = rs.Save(pp.RenderStateFile)
		})
		if err != nil {
			return err
		}

		printCollectionRenderSummary(outWriter, fullResults)
	} else {
		// Non-TUI: fetch then render sequentially
		validSegments, toRender, skipResults, rs, buildErr := autoFetchAndRebuild(nil)
		if buildErr != nil {
			return buildErr
		}

		var renderResults []render.Result
		if len(toRender) > 0 {
			renderResults = svc.Render(ctx, toRender, render.Options{
				Concurrency: renderConcurrency,
				Force:       renderForce,
			})
		}

		fullResults = mergeCollectionRenderResultsWithSkips(collectionClips, preflight, shouldRender, renderResults, skipResults)

		// Update render state
		rs.GlobalConfigHash = state.GlobalConfigHash(cfg)
		segByPath := make(map[string]render.Segment, len(validSegments))
		for _, seg := range validSegments {
			segByPath[seg.OutputPath] = seg
		}
		filenameTemplate := cfg.SegmentFilenameTemplate()
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
		currentKeys := make(map[string]bool, len(validSegments))
		for _, seg := range validSegments {
			currentKeys[seg.OutputPath] = true
		}
		state.Prune(rs, currentKeys)
		if saveErr := rs.Save(pp.RenderStateFile); saveErr != nil {
			return fmt.Errorf("save render state: %w", saveErr)
		}

		if mode == tui.ModeJSON {
			return writeCollectionRenderJSON(cmd, pp.Root, collectionClips, fullResults)
		}

		writeCollectionRenderTable(cmd, pp.Root, collectionClips, segments, fullResults)
	}

	return printCollectionRenderErrors(cmd.ErrOrStderr(), collectionClips, fullResults)
}

func buildCollectionRenderSegment(pp paths.ProjectPaths, cfg config.Config, idx *cache.Index, resolver *project.CollectionResolver, collClip project.CollectionClip) (render.Segment, error) {
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
	isURL := strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "youtu")

	if !isURL {
		link = strings.Trim(link, "'\"")

		var sourcePath string
		if filepath.IsAbs(link) {
			if _, err := os.Stat(link); err == nil {
				sourcePath = link
			} else {
				sourcePath = filepath.Join(pp.Root, strings.TrimPrefix(link, string(filepath.Separator)))
			}
		} else {
			sourcePath = filepath.Join(pp.Root, link)
		}

		if _, err := os.Stat(sourcePath); err != nil {
			if os.IsNotExist(err) {
				return segment, missingCachedSourceError{
					msg: fmt.Sprintf("local file not found: %s", sourcePath),
				}
			}
			return segment, fmt.Errorf("collection %q row %03d: stat local file: %w", collClip.CollectionName, clip.Row.Index, err)
		}

		segment.SourcePath = sourcePath
		segment.CachedPath = sourcePath
	} else {
		entry, ok, err := resolveEntryForRow(pp, idx, clip.Row)
		if err != nil {
			return segment, err
		}
		if !ok {
			return segment, missingCachedSourceError{
				msg: "video not downloaded; may be unavailable or region-locked",
			}
		}

		segment.Entry = entry
		segment.SourcePath = entry.CachedPath
		segment.CachedPath = entry.CachedPath
	}

	return segment, nil
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
	{Header: "SOURCE", Width: 20},
	{Header: "OUTPUT", Width: 30},
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

func collectionRenderResultFields(projectRoot string, cc project.CollectionClip, seg render.Segment, res render.Result) map[string]string {
	fields := make(map[string]string)

	if res.Err != nil {
		fields["STATUS"] = "error"
		errMsg := res.Err.Error()
		if strings.Contains(errMsg, "not downloaded") ||
			strings.Contains(errMsg, "not found") {
			fields["SOURCE"] = "MISSING"
		}
	} else if res.Skipped {
		fields["STATUS"] = "skipped"
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

func mergeCollectionRenderResults(clips []project.CollectionClip, preflight []render.Result, shouldRender []bool, results []render.Result) []render.Result {
	fullResults := make([]render.Result, len(clips))
	resultIdx := 0
	for i := range clips {
		if shouldRender[i] {
			fullResults[i] = results[resultIdx]
			resultIdx++
		} else {
			fullResults[i] = preflight[i]
		}
	}
	return fullResults
}

// mergeCollectionRenderResultsWithSkips merges preflight errors, change-detection
// skips, and actual render results into a unified results slice.
func mergeCollectionRenderResultsWithSkips(clips []project.CollectionClip, preflight []render.Result, shouldRender []bool, renderResults []render.Result, skipResults map[string]render.Result) []render.Result {
	fullResults := make([]render.Result, len(clips))
	renderIdx := 0
	for i := range clips {
		if !shouldRender[i] {
			// Preflight error (e.g. missing cached source)
			fullResults[i] = preflight[i]
			continue
		}
		// Check if this segment was skipped by change detection
		outputPath := preflight[i].OutputPath // may be empty
		if outputPath == "" && i < len(clips) {
			// Try to find it from render or skip results
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
		// Actual render result
		if renderIdx < len(renderResults) {
			fullResults[i] = renderResults[renderIdx]
			renderIdx++
		}
	}
	return fullResults
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
				Title:  clipDisplayTitle(a.Segment.Clip),
				Action: a.Action,
				Reason: a.Reason,
				Output: a.Segment.OutputPath,
			})
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return
	}

	var renderCount, skipCount int
	for _, a := range actions {
		if a.Action == state.ActionRender {
			renderCount++
		} else {
			skipCount++
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "DRY RUN: %d segments would be rendered, %d would be skipped\n\n", renderCount, skipCount)
	for _, a := range actions {
		tag := "SKIP  "
		if a.Action == state.ActionRender {
			tag = "RENDER"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  %03d  %-20s  (%s)\n",
			tag, a.Segment.Clip.Sequence, clipDisplayTitle(a.Segment.Clip), a.Reason)
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
