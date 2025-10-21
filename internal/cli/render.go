package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	"powerhour/internal/render"
	"powerhour/pkg/csvplan"
)

var (
	renderConcurrency int
	renderForce       bool
	renderIndexArg    []string
	renderNoProgress  bool
)

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

	planOpts := csvplan.Options{
		HeaderAliases:   cfg.HeaderAliases(),
		DefaultDuration: cfg.PlanDefaultDuration(),
	}
	rows, err := csvplan.LoadWithOptions(pp.CSVFile, planOpts)
	if err != nil {
		return err
	}

	rows, err = filterRowsByIndexArgs(rows, renderIndexArg)
	if err != nil {
		return err
	}

	segments := make([]render.Segment, 0, len(rows))
	for _, row := range rows {
		entry, ok, err := resolveEntryForRow(pp, idx, row)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("row %03d %q has no cached source; run `powerhour fetch` first", row.Index, row.Title)
		}
		exists, err := paths.FileExists(entry.CachedPath)
		if err != nil {
			return fmt.Errorf("stat cached source for row %03d: %w", row.Index, err)
		}
		if !exists {
			return fmt.Errorf("cached source not found for row %03d %q (expected at %s)", row.Index, row.Title, entry.CachedPath)
		}
		segments = append(segments, render.Segment{
			Row:        row,
			CachedPath: entry.CachedPath,
			Entry:      entry,
		})
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

	if outputJSON {
		return writeRenderJSON(cmd, pp.Root, results)
	}

	if useInteractive {
		progress.Finalize(results)
		writeRenderSummary(outWriter, cmd.ErrOrStderr(), results)
	} else {
		return writeRenderOutput(cmd, results)
	}
	return nil
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
			fmt.Fprintf(cmd.ErrOrStderr(), "render %03d %q failed: %v\n", res.Index, res.Title, res.Err)
			continue
		}
		if res.Skipped {
			skipped++
			fmt.Fprintf(cmd.OutOrStdout(), "skipped %03d → %s (already exists)\n", res.Index, res.OutputPath)
		} else {
			rendered++
			fmt.Fprintf(cmd.OutOrStdout(), "rendered %03d → %s\n", res.Index, res.OutputPath)
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
				fmt.Fprintf(errWriter, "render %03d %q failed: %v\n", res.Index, res.Title, res.Err)
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
	rows        map[int]*renderProgressRow
	order       []int
	lineCount   int
}

type renderProgressRow struct {
	Index  int
	Title  string
	Status string
	Output string
	Log    string
	Error  string
}

func newRenderProgressPrinter(out io.Writer, segments []render.Segment, interactive bool) *renderProgressPrinter {
	rows := make(map[int]*renderProgressRow, len(segments))
	order := make([]int, 0, len(segments))
	seen := make(map[int]struct{}, len(segments))
	for _, seg := range segments {
		idx := seg.Row.Index
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		order = append(order, idx)
		rows[idx] = &renderProgressRow{
			Index:  idx,
			Title:  renderTitle(seg.Row.Title),
			Status: "pending",
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
	row := p.ensureRow(seg.Row.Index, seg.Row.Title)
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
	row := p.ensureRow(res.Index, res.Title)
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
	fmt.Fprintln(tw, "INDEX\tSTATUS\tTITLE\tOUTPUT\tLOG\tERROR")
	for _, idx := range p.order {
		row := p.rows[idx]
		fmt.Fprintf(tw, "%03d\t%s\t%s\t%s\t%s\t%s\n",
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

func (p *renderProgressPrinter) ensureRow(index int, title string) *renderProgressRow {
	if p.rows == nil {
		p.rows = map[int]*renderProgressRow{}
	}
	row, ok := p.rows[index]
	if !ok {
		row = &renderProgressRow{Index: index}
		p.rows[index] = row
		p.order = append(p.order, index)
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
	Index      int    `json:"index"`
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
