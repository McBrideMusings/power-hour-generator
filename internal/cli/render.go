package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
)

var (
	renderConcurrency int
	renderForce       bool
	renderIndexArg    []string
	renderNoProgress  bool
)

var errMissingCachedSource = errors.New("missing cached source")

type missingCachedSourceError struct {
	msg string
}

func (e missingCachedSourceError) Error() string {
	return e.msg
}

func (e missingCachedSourceError) Is(target error) bool {
	return target == errMissingCachedSource
}

func newRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render cached clips into individual segment files",
		RunE:  runRender,
	}

	defaultConcurrency := runtime.NumCPU()
	if defaultConcurrency < 1 {
		defaultConcurrency = 1
	}

	cmd.Flags().IntVar(&renderConcurrency, "concurrency", defaultConcurrency, "Concurrent ffmpeg processes")
	cmd.Flags().BoolVar(&renderForce, "force", false, "Re-render even if segment output already exists")
	cmd.Flags().BoolVar(&renderNoProgress, "no-progress", false, "Disable interactive progress output")
	cmd.Flags().StringSliceVar(&renderIndexArg, "index", nil, "Limit render to specific 1-based row index or range like 5-10 (repeat flag for multiple)")

	return cmd
}

func runRender(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)

	if err := ensureProjectDirs(pp); err != nil {
		return err
	}

	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	resolver, err := project.NewResolver(cfg, pp)
	if err != nil {
		return err
	}

	plans, err := resolver.LoadPlans()
	if err != nil {
		return err
	}

	if len(renderIndexArg) > 0 {
		songRows, ok := plans[project.ClipTypeSong]
		if !ok || len(songRows) == 0 {
			return fmt.Errorf("no song plan rows available to filter by index")
		}
		filtered, err := filterRowsByIndexArgs(songRows, renderIndexArg)
		if err != nil {
			return err
		}
		plans[project.ClipTypeSong] = filtered
	}

	timeline, err := resolver.BuildTimeline(plans)
	if err != nil {
		return err
	}

	if len(timeline) == 0 {
		return fmt.Errorf("no clips to render; check clips configuration")
	}

	segments := make([]render.Segment, 0, len(timeline))
	renderOrder := make([]int, 0, len(timeline))
	preflight := make([]render.Result, len(timeline))
	shouldRender := make([]bool, len(timeline))
	for i, clip := range timeline {
		segment, err := buildRenderSegment(pp, idx, resolver, clip)
		if err != nil {
			if errors.Is(err, errMissingCachedSource) {
				preflight[i] = renderPreflightResult(clip, err)
				continue
			}
			return err
		}
		segments = append(segments, segment)
		renderOrder = append(renderOrder, i)
		shouldRender[i] = true
	}

	svc, err := render.NewService(ctx, pp, cfg, nil)
	if err != nil {
		return err
	}
	svc.SetWriters(cmd.OutOrStdout(), nil)

	outWriter := cmd.OutOrStdout()
	useInteractive := detectInteractiveProgress(outWriter, renderNoProgress || outputJSON) && len(segments) > 0 && !outputJSON

	var progress *renderProgressPrinter
	var reporter render.ProgressReporter
	if useInteractive {
		fmt.Fprintf(outWriter, "Project: %s\n", pp.Root)
		progress = newRenderProgressPrinter(outWriter, segments, true)
		progress.render()
		reporter = progress
	}

	results := svc.Render(ctx, segments, render.Options{
		Concurrency: renderConcurrency,
		Force:       renderForce,
		Reporter:    reporter,
	})

	finalResults := make([]render.Result, 0, len(timeline))
	perTimeline := make([]render.Result, len(timeline))
	for j, res := range results {
		if j < len(renderOrder) {
			perTimeline[renderOrder[j]] = res
		}
	}
	for i := range timeline {
		if preflight[i].Err != nil {
			finalResults = append(finalResults, preflight[i])
			if progress != nil {
				progress.Complete(preflight[i])
			}
			continue
		}
		if shouldRender[i] {
			finalResults = append(finalResults, perTimeline[i])
		}
	}

	if outputJSON {
		return writeRenderJSON(cmd, pp.Root, finalResults)
	}

	if useInteractive {
		progress.Finalize(finalResults)
		writeRenderSummary(outWriter, cmd.ErrOrStderr(), finalResults)
	} else {
		return writeRenderOutput(cmd, finalResults)
	}
	return nil
}

func buildRenderSegment(pp paths.ProjectPaths, idx *cache.Index, resolver *project.Resolver, clip project.Clip) (render.Segment, error) {
	profile, ok := resolver.Profile(clip.OverlayProfile)
	if !ok {
		return render.Segment{}, fmt.Errorf("clip %s#%d references unknown overlay profile %q", clip.ClipType, clip.TypeIndex, clip.OverlayProfile)
	}

	segments := profile.ResolveSegments(clip.SegmentOverrides)

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

	switch clip.SourceKind {
	case project.SourceKindPlan:
		entry, ok, err := resolveEntryForRow(pp, idx, clip.Row)
		if err != nil {
			return render.Segment{}, err
		}
		if !ok {
			return render.Segment{}, missingCachedSourceError{
				msg: fmt.Sprintf("row %03d %q has no cached source; run `powerhour fetch` first", clip.Row.Index, clip.Row.Title),
			}
		}
		exists, err := paths.FileExists(entry.CachedPath)
		if err != nil {
			return render.Segment{}, fmt.Errorf("stat cached source for row %03d: %w", clip.Row.Index, err)
		}
		if !exists {
			return render.Segment{}, missingCachedSourceError{
				msg: fmt.Sprintf("cached source not found for row %03d %q (expected at %s)", clip.Row.Index, clip.Row.Title, entry.CachedPath),
			}
		}
		segment.SourcePath = entry.CachedPath
		segment.CachedPath = entry.CachedPath
		segment.Entry = entry
	case project.SourceKindMedia:
		sourcePath := strings.TrimSpace(clip.MediaPath)
		if sourcePath == "" {
			return render.Segment{}, fmt.Errorf("clip %s#%d missing media path", clip.ClipType, clip.TypeIndex)
		}
		exists, err := paths.FileExists(sourcePath)
		if err != nil {
			return render.Segment{}, fmt.Errorf("stat media for %s#%d: %w", clip.ClipType, clip.TypeIndex, err)
		}
		if !exists {
			return render.Segment{}, fmt.Errorf("media not found for %s#%d (expected at %s)", clip.ClipType, clip.TypeIndex, sourcePath)
		}
		segment.SourcePath = sourcePath
	default:
		return render.Segment{}, fmt.Errorf("clip %s#%d has no source configured", clip.ClipType, clip.TypeIndex)
	}

	return segment, nil
}

func renderPreflightResult(clip project.Clip, err error) render.Result {
	return render.Result{
		Index:     clip.Sequence,
		ClipType:  clip.ClipType,
		TypeIndex: clip.TypeIndex,
		Title:     clipDisplayTitle(clip),
		Err:       err,
	}
}

func writeRenderOutput(cmd *cobra.Command, results []render.Result) error {
	var (
		failures int
		rendered int
		skipped  int
	)

	for _, res := range results {
		if res.Err != nil {
			failures++
			fmt.Fprintf(cmd.ErrOrStderr(), "render %s failed: %v\n", renderResultLabel(res), res.Err)
			continue
		}
		if res.Skipped {
			skipped++
			fmt.Fprintf(cmd.OutOrStdout(), "skipped %s → %s (already exists)\n", renderResultLabel(res), res.OutputPath)
		} else {
			rendered++
			fmt.Fprintf(cmd.OutOrStdout(), "rendered %s → %s\n", renderResultLabel(res), res.OutputPath)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "completed renders: %d rendered, %d skipped, %d failed\n", rendered, skipped, failures)

	if failures > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "%d render(s) failed; see logs for details\n", failures)
	}
	return nil
}

func writeRenderJSON(cmd *cobra.Command, project string, results []render.Result) error {
	payload := struct {
		Project string             `json:"project"`
		Results []renderJSONResult `json:"results"`
		Summary renderJSONSummary  `json:"summary"`
	}{
		Project: project,
		Results: make([]renderJSONResult, 0, len(results)),
	}

	for _, res := range results {
		payload.Results = append(payload.Results, renderJSONResult{
			ClipType:   string(res.ClipType),
			TypeIndex:  res.TypeIndex,
			Index:      res.Index,
			Title:      res.Title,
			OutputPath: res.OutputPath,
			LogPath:    res.LogPath,
			Skipped:    res.Skipped,
			Error:      errorString(res.Err),
		})
		if res.Err != nil {
			payload.Summary.Failed++
		} else {
			if res.Skipped {
				payload.Summary.Skipped++
			} else {
				payload.Summary.Rendered++
			}
		}
	}

	sort.Slice(payload.Results, func(i, j int) bool {
		return payload.Results[i].Index < payload.Results[j].Index
	})

	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode render json: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(out))
	return nil
}

func writeRenderSummary(out io.Writer, errWriter io.Writer, results []render.Result) {
	var (
		rendered int
		skipped  int
		failed   int
	)

	for _, res := range results {
		if res.Err != nil {
			failed++
			if errWriter != nil {
				fmt.Fprintf(errWriter, "render %s failed: %v\n", renderResultLabel(res), res.Err)
			}
			continue
		}
		if res.Skipped {
			skipped++
		} else {
			rendered++
		}
	}

	fmt.Fprintf(out, "completed renders: %d rendered, %d skipped, %d failed\n", rendered, skipped, failed)
	if failed > 0 && errWriter != nil {
		fmt.Fprintf(errWriter, "%d render(s) failed; see logs for details\n", failed)
	}
}

type renderProgressPrinter struct {
	out         io.Writer
	interactive bool
	mu          sync.Mutex
	rows        map[string]*renderProgressRow
	order       []string
	lineCount   int
}

type renderProgressRow struct {
	ClipType string
	Index    int
	Title    string
	Status   string
	Output   string
	Log      string
	Error    string
}

func newRenderProgressPrinter(out io.Writer, segments []render.Segment, interactive bool) *renderProgressPrinter {
	rows := make(map[string]*renderProgressRow, len(segments))
	order := make([]string, 0, len(segments))
	seen := make(map[string]struct{}, len(segments))
	for _, seg := range segments {
		key := renderProgressKey(seg)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		order = append(order, key)
		title := clipDisplayTitle(seg.Clip)
		index := seg.Clip.TypeIndex
		if index <= 0 {
			index = seg.Clip.Sequence
		}
		rows[key] = &renderProgressRow{
			ClipType: string(seg.Clip.ClipType),
			Index:    index,
			Title:    renderTitle(title),
			Status:   "pending",
		}
	}
	return &renderProgressPrinter{
		out:         out,
		interactive: interactive,
		rows:        rows,
		order:       order,
	}
}

func (p *renderProgressPrinter) Start(seg render.Segment) {
	if !p.interactive {
		return
	}
	p.mu.Lock()
	row := p.ensureRow(renderProgressKey(seg), clipDisplayTitle(seg.Clip))
	row.ClipType = string(seg.Clip.ClipType)
	index := seg.Clip.TypeIndex
	if index <= 0 {
		index = seg.Clip.Sequence
	}
	row.Index = index
	row.Title = renderTitle(clipDisplayTitle(seg.Clip))
	row.Status = "rendering"
	row.Error = ""
	row.Output = ""
	row.Log = ""
	p.renderLocked()
	p.mu.Unlock()
}

func (p *renderProgressPrinter) Complete(res render.Result) {
	if !p.interactive {
		return
	}
	p.mu.Lock()
	row := p.ensureRow(renderResultKey(res), res.Title)
	row.ClipType = string(res.ClipType)
	if res.TypeIndex > 0 {
		row.Index = res.TypeIndex
	} else {
		row.Index = res.Index
	}
	row.Output = shortPath(res.OutputPath)
	row.Log = shortPath(res.LogPath)
	if res.Err != nil {
		row.Status = "error"
		row.Error = res.Err.Error()
	} else if res.Skipped {
		row.Status = "skipped"
		row.Error = ""
	} else {
		row.Status = "rendered"
		row.Error = ""
	}
	p.renderLocked()
	p.mu.Unlock()
}

func (p *renderProgressPrinter) Finalize(_ []render.Result) {
	if !p.interactive {
		return
	}
	p.mu.Lock()
	p.renderLocked()
	p.mu.Unlock()
}

func (p *renderProgressPrinter) Interactive() bool {
	return p != nil && p.interactive
}

func (p *renderProgressPrinter) render() {
	if !p.Interactive() {
		return
	}
	p.mu.Lock()
	p.renderLocked()
	p.mu.Unlock()
}

func (p *renderProgressPrinter) renderLocked() {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tINDEX\tSTATUS\tTITLE\tOUTPUT\tLOG\tERROR")
	for _, key := range p.order {
		row := p.rows[key]
		fmt.Fprintf(tw, "%s\t%03d\t%s\t%s\t%s\t%s\t%s\n",
			row.ClipType,
			row.Index,
			row.Status,
			row.Title,
			nonEmptyOrDash(row.Output),
			nonEmptyOrDash(row.Log),
			truncateWithEllipsis(row.Error, 60),
		)
	}
	tw.Flush()
	lines := bytes.Split(buf.Bytes(), []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	if p.lineCount > 0 {
		fmt.Fprintf(p.out, "\x1b[%dA\r", p.lineCount)
	}
	for i, line := range lines {
		fmt.Fprintf(p.out, "\x1b[2K%s", line)
		if i < len(lines)-1 {
			fmt.Fprint(p.out, "\n")
		}
	}
	fmt.Fprint(p.out, "\n")
	p.lineCount = len(lines)
}

func (p *renderProgressPrinter) ensureRow(key string, title string) *renderProgressRow {
	if p.rows == nil {
		p.rows = map[string]*renderProgressRow{}
	}
	row, ok := p.rows[key]
	if !ok {
		row = &renderProgressRow{}
		p.rows[key] = row
		p.order = append(p.order, key)
	}
	if row.Title == "" {
		row.Title = renderTitle(title)
	}
	return row
}

func renderTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, "\t", " ")
	return truncateWithEllipsis(title, 28)
}

func shortPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

func clipDisplayTitle(clip project.Clip) string {
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

func renderResultLabel(res render.Result) string {
	index := res.TypeIndex
	if index <= 0 {
		index = res.Index
	}
	label := fmt.Sprintf("%s#%03d", res.ClipType, index)
	if title := strings.TrimSpace(res.Title); title != "" {
		label = fmt.Sprintf("%s %s", label, title)
	}
	return label
}

func renderProgressKey(seg render.Segment) string {
	clip := seg.Clip
	index := clip.TypeIndex
	if index <= 0 {
		index = clip.Sequence
	}
	return fmt.Sprintf("%s:%03d", clip.ClipType, index)
}

func renderResultKey(res render.Result) string {
	index := res.TypeIndex
	if index <= 0 {
		index = res.Index
	}
	return fmt.Sprintf("%s:%03d", res.ClipType, index)
}

func truncateWithEllipsis(value string, max int) string {
	if max <= 0 {
		return ""
	}
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

type renderJSONResult struct {
	ClipType   string `json:"clip_type"`
	TypeIndex  int    `json:"type_index"`
	Index      int    `json:"sequence"`
	Title      string `json:"title"`
	OutputPath string `json:"output_path"`
	LogPath    string `json:"log_path"`
	Skipped    bool   `json:"skipped,omitempty"`
	Error      string `json:"error,omitempty"`
}

type renderJSONSummary struct {
	Rendered int `json:"rendered"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
