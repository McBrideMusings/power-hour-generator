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

	svc, err := render.NewService(ctx, pp, cfg, nil)
	if err != nil {
		return err
	}

	// Handle sample mode
	if renderSampleTime != "" {
		svc.SetWriters(cmd.OutOrStdout(), nil)
		validSegments := make([]render.Segment, 0, len(renderOrder))
		for _, idx := range renderOrder {
			validSegments = append(validSegments, segments[idx])
		}
		if len(validSegments) == 0 {
			return fmt.Errorf("no valid segments to sample")
		}
		return runRenderSample(ctx, cmd, svc, validSegments, nil)
	}

	outWriter := cmd.OutOrStdout()
	mode := tui.DetectMode(outWriter, renderNoProgress, outputJSON)

	// In TUI mode, suppress render service stdout to avoid corrupting the display.
	if mode != tui.ModeTUI {
		svc.SetWriters(cmd.OutOrStdout(), nil)
	}

	// Build valid segments list
	validSegments := make([]render.Segment, 0, len(renderOrder))
	for _, idx := range renderOrder {
		validSegments = append(validSegments, segments[idx])
	}

	// --- Smart re-rendering: change detection ---
	rs, err := state.Load(pp.RenderStateFile)
	if err != nil {
		return fmt.Errorf("load render state: %w", err)
	}

	filenameTemplate := cfg.SegmentFilenameTemplate()
	actions := state.DetectChanges(rs, validSegments, cfg, filenameTemplate, renderForce)

	if renderDryRun {
		printDryRun(cmd, actions, outputJSON)
		return nil
	}

	// Split into segments to render vs skip
	var toRender []render.Segment
	skipResults := make(map[string]render.Result) // keyed by output path
	for i, a := range actions {
		seg := validSegments[i]
		if a.Action == state.ActionSkip {
			skipResults[seg.OutputPath] = render.Result{
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

	var fullResults []render.Result

	if mode == tui.ModeTUI {
		fmt.Fprintf(outWriter, "Project: %s\n", pp.Root)
		model := buildCollectionRenderProgressModel(pp.Root, collectionClips, segments)

		// Build sequence -> key lookup for the reporter
		seqToKey := make(map[int]string, len(collectionClips))
		for _, clipIdx := range renderOrder {
			cc := collectionClips[clipIdx]
			seqToKey[cc.Clip.Sequence] = collectionRenderKey(cc)
		}

		err := tui.RunWithWork(outWriter, model, func(send func(tea.Msg)) {
			// Send preflight errors
			for i := range collectionClips {
				if preflight[i].Err != nil {
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
					return map[string]string{"STATUS": "rendering"}
				},
				func(res render.Result) map[string]string {
					// Find the matching clip and segment
					for _, clipIdx := range renderOrder {
						cc := collectionClips[clipIdx]
						if cc.Clip.Sequence == res.Index {
							return collectionRenderResultFields(pp.Root, cc, segments[clipIdx], res)
						}
					}
					// Fallback
					status := "rendered"
					if res.Err != nil {
						status = "error"
					} else if res.Skipped {
						status = "skipped"
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
		})
		if err != nil {
			return err
		}

		printCollectionRenderSummary(outWriter, fullResults)
	} else {
		var renderResults []render.Result
		if len(toRender) > 0 {
			renderResults = svc.Render(ctx, toRender, render.Options{
				Concurrency: renderConcurrency,
				Force:       renderForce,
			})
		}

		fullResults = mergeCollectionRenderResultsWithSkips(collectionClips, preflight, shouldRender, renderResults, skipResults)

		if mode == tui.ModeJSON {
			return writeCollectionRenderJSON(cmd, pp.Root, collectionClips, fullResults)
		}

		writeCollectionRenderTable(cmd, pp.Root, collectionClips, segments, fullResults)
	}

	// --- Update render state ---
	rs.GlobalConfigHash = state.GlobalConfigHash(cfg)

	// Build segment lookup by output path for state updates
	segByPath := make(map[string]render.Segment, len(validSegments))
	for _, seg := range validSegments {
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

	// Prune entries for segments no longer in the plan
	currentKeys := make(map[string]bool, len(validSegments))
	for _, seg := range validSegments {
		currentKeys[seg.OutputPath] = true
	}
	state.Prune(rs, currentKeys)

	if err := rs.Save(pp.RenderStateFile); err != nil {
		return fmt.Errorf("save render state: %w", err)
	}

	return nil
}

func buildCollectionRenderSegment(pp paths.ProjectPaths, cfg config.Config, idx *cache.Index, resolver *project.CollectionResolver, collClip project.CollectionClip) (render.Segment, error) {
	clip := collClip.Clip

	var profile project.ResolvedProfile
	var segments []config.OverlaySegment
	if clip.OverlayProfile != "" {
		var ok bool
		profile, ok = resolver.Profile(clip.OverlayProfile)
		if !ok {
			return render.Segment{}, fmt.Errorf("collection %q references unknown overlay profile %q", collClip.CollectionName, clip.OverlayProfile)
		}
		segments = profile.ResolveSegments()
	}

	clip.Row.DurationSeconds = clip.DurationSeconds
	if clip.Row.Index <= 0 {
		clip.Row.Index = clip.TypeIndex
		if clip.Row.Index <= 0 {
			clip.Row.Index = clip.Sequence
		}
	}

	segment := render.Segment{
		Clip:     clip,
		Profile:  profile,
		Segments: segments,
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
					msg: fmt.Sprintf("collection %q row %03d: local file not found: %s", collClip.CollectionName, clip.Row.Index, sourcePath),
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
				msg: fmt.Sprintf("collection %q row %03d has no cached source; run `powerhour fetch` first", collClip.CollectionName, clip.Row.Index),
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
	fmt.Fprintln(w, "COLLECTION\tINDEX\tSTATUS\tSOURCE\tOUTPUT\tERROR")
	for i, collClip := range clips {
		res := results[i]
		status := "success"
		source := "-"
		outputPath := "-"
		errMsg := ""

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
			errMsg = res.Err.Error()
			if strings.Contains(errMsg, "has no cached source") ||
				strings.Contains(errMsg, "not found") {
				source = "MISSING"
			}
		} else if res.OutputPath != "" {
			relPath, err := filepath.Rel(projectRoot, res.OutputPath)
			if err == nil && !strings.HasPrefix(relPath, "..") {
				outputPath = relPath
			} else {
				outputPath = res.OutputPath
			}
		}

		fmt.Fprintf(w, "%s\t%03d\t%s\t%s\t%s\t%s\n",
			collClip.CollectionName,
			collClip.Clip.Row.Index,
			status,
			source,
			outputPath,
			errMsg,
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
		if strings.Contains(errMsg, "has no cached source") ||
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
