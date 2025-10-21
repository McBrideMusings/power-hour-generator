package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

var (
	fetchForce      bool
	fetchReprobe    bool
	fetchNoDownload bool
	fetchNoProgress bool
	fetchIndexArg   []string
)

var newCacheService = cache.NewService

func newFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Populate the project source cache",
		RunE:  runFetch,
	}

	cmd.Flags().BoolVar(&fetchForce, "force", false, "Re-download all sources even if cached")
	cmd.Flags().BoolVar(&fetchReprobe, "reprobe", false, "Re-run ffprobe on cached entries")
	cmd.Flags().BoolVar(&fetchNoDownload, "no-download", false, "Skip downloading new sources; only match existing files")
	cmd.Flags().BoolVar(&fetchNoProgress, "no-progress", false, "Disable interactive progress output")
	cmd.Flags().StringSliceVar(&fetchIndexArg, "index", nil, "Limit fetch to specific 1-based row index or range like 5-10 (repeat flag for multiple)")

	return cmd
}

func runFetch(cmd *cobra.Command, _ []string) error {
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

	exists, err := paths.DirExists(pp.Root)
	if err != nil {
		return fmt.Errorf("stat project dir: %w", err)
	}
	if !exists {
		return fmt.Errorf("project directory does not exist: %s", pp.Root)
	}

	if err := pp.EnsureMetaDirs(); err != nil {
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

	rows, err = filterRowsByIndexArgs(rows, fetchIndexArg)
	if err != nil {
		return err
	}

	logger, closer, err := logx.New(pp)
	if err != nil {
		return err
	}
	defer closer.Close()

	svc, err := newCacheService(ctx, pp, logger, nil)
	if err != nil {
		return err
	}
	svc.SetLogOutput(cmd.ErrOrStderr())

	opts := cache.ResolveOptions{Force: fetchForce, Reprobe: fetchReprobe, NoDownload: fetchNoDownload}

	outWriter := cmd.OutOrStdout()
	useInteractive := detectInteractiveProgress(outWriter, fetchNoProgress || outputJSON) && len(rows) > 0 && !outputJSON
	if useInteractive {
		fmt.Fprintf(outWriter, "Project: %s\n", pp.Root)
	}
	progress := newFetchProgressPrinter(outWriter, rows, useInteractive)
	if useInteractive {
		progress.render()
	}

	outcomes := make([]fetchRowResult, 0, len(rows))
	counts := fetchCounts{}
	dirty := false

	for _, row := range rows {
		progress.Start(row, fetchForce, fetchNoDownload)

		result, err := svc.Resolve(ctx, idx, row, opts)
		if err != nil {
			counts.Failed++
			logger.Printf("fetch row %03d %q failed: %v", row.Index, row.Title, err)
			fmt.Fprintf(cmd.ErrOrStderr(), "fetch row %03d %q failed: %v\n", row.Index, row.Title, err)
			progress.Error(row, err)
			outcomes = append(outcomes, fetchRowResult{
				Index:  row.Index,
				Title:  row.Title,
				Status: "error",
				Link:   row.Link,
				Error:  err.Error(),
			})
			continue
		}

		switch result.Status {
		case cache.ResolveStatusDownloaded:
			counts.Downloaded++
		case cache.ResolveStatusCopied:
			counts.Copied++
		case cache.ResolveStatusMatched:
			counts.Matched++
		case cache.ResolveStatusMissing:
			counts.Missing++
		case cache.ResolveStatusCached:
			counts.Reused++
		}
		if result.Probed {
			counts.Probed++
		}
		if result.Updated {
			dirty = true
		}

		outcomes = append(outcomes, fetchRowResult{
			Index:      row.Index,
			Title:      row.Title,
			Status:     string(result.Status),
			CachedPath: result.Entry.CachedPath,
			Link:       row.Link,
			Identifier: result.Identifier,
			MediaID:    result.ID,
			SizeBytes:  result.Entry.SizeBytes,
			Probed:     result.Probed,
		})

		progress.Complete(row, result)
	}

	if dirty {
		if err := cache.Save(pp, idx); err != nil {
			return err
		}
	}

	if outputJSON {
		return writeFetchJSON(cmd, pp.Root, outcomes, counts)
	}

	if progress.Interactive() {
		progress.Finalize()
		printFetchSummary(outWriter, counts)
		if counts.Failed > 0 {
			writeFetchFailures(cmd, outcomes)
		}
	} else {
		writeFetchTable(cmd, pp.Root, outcomes, counts)
		if counts.Failed > 0 {
			writeFetchFailures(cmd, outcomes)
		}
	}
	return nil
}

func parseIndexArgs(args []string) ([]int, error) {
	indexes := make([]int, 0)
	for _, raw := range args {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		if strings.Contains(token, "-") {
			parts := strings.SplitN(token, "-", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid index range %q", token)
			}
			start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid index %q: %w", parts[0], err)
			}
			end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid index %q: %w", parts[1], err)
			}
			if start <= 0 || end <= 0 {
				return nil, fmt.Errorf("index values must be greater than zero: %d-%d", start, end)
			}
			if end < start {
				return nil, fmt.Errorf("index range start greater than end: %d-%d", start, end)
			}
			for i := start; i <= end; i++ {
				indexes = append(indexes, i)
			}
			continue
		}
		val, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("invalid index %q: %w", token, err)
		}
		if val <= 0 {
			return nil, fmt.Errorf("index must be greater than zero: %d", val)
		}
		indexes = append(indexes, val)
	}
	return indexes, nil
}

func filterRowsByIndex(rows []csvplan.Row, indexes []int) ([]csvplan.Row, error) {
	filter := make(map[int]struct{}, len(indexes))
	for _, idx := range indexes {
		if idx <= 0 {
			return nil, fmt.Errorf("index must be greater than zero: %d", idx)
		}
		filter[idx] = struct{}{}
	}
	if len(filter) == 0 {
		return nil, fmt.Errorf("no indexes provided")
	}

	filtered := make([]csvplan.Row, 0, len(filter))
	for _, row := range rows {
		if _, ok := filter[row.Index]; ok {
			filtered = append(filtered, row)
			delete(filter, row.Index)
		}
	}

	if len(filter) > 0 {
		missing := make([]int, 0, len(filter))
		for idx := range filter {
			missing = append(missing, idx)
		}
		sort.Ints(missing)
		return nil, fmt.Errorf("indexes not found in plan: %v", missing)
	}
	return filtered, nil
}

func writeFetchJSON(cmd *cobra.Command, project string, rows []fetchRowResult, counts fetchCounts) error {
	payload := struct {
		Project string           `json:"project"`
		Rows    []fetchRowResult `json:"rows"`
		Summary fetchCounts      `json:"summary"`
	}{
		Project: project,
		Rows:    rows,
		Summary: counts,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode fetch json: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func writeFetchTable(cmd *cobra.Command, project string, rows []fetchRowResult, counts fetchCounts) {
	fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\n", project)

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "INDEX\tSTATUS\tID\tPATH\tLINK\tERROR")
	for _, row := range rows {
		id := row.MediaID
		if id == "" {
			id = row.Identifier
		}
		fmt.Fprintf(w, "%03d\t%s\t%s\t%s\t%s\t%s\n",
			row.Index,
			row.Status,
			id,
			row.CachedPath,
			row.Link,
			row.Error,
		)
	}
	w.Flush()

	printFetchSummary(cmd.OutOrStdout(), counts)
}

type fetchRowResult struct {
	Index      int    `json:"index"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	CachedPath string `json:"cached_path"`
	Link       string `json:"link"`
	Identifier string `json:"identifier"`
	MediaID    string `json:"media_id"`
	SizeBytes  int64  `json:"size_bytes"`
	Probed     bool   `json:"probed"`
	Error      string `json:"error,omitempty"`
}

type fetchCounts struct {
	Downloaded int `json:"downloaded"`
	Copied     int `json:"copied"`
	Matched    int `json:"matched"`
	Reused     int `json:"reused"`
	Missing    int `json:"missing"`
	Probed     int `json:"probed"`
	Failed     int `json:"failed"`
}

func writeFetchFailures(cmd *cobra.Command, rows []fetchRowResult) {
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "Failures:")
	for _, row := range rows {
		if row.Status != "error" {
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %03d %s (%s): %s\n", row.Index, row.Title, row.Link, row.Error)
	}
}

func printFetchSummary(w io.Writer, counts fetchCounts) {
	fmt.Fprintf(w, "Downloaded: %d, Copied: %d, Matched: %d, Reused: %d, Missing: %d, Probed: %d, Failed: %d\n",
		counts.Downloaded, counts.Copied, counts.Matched, counts.Reused, counts.Missing, counts.Probed, counts.Failed,
	)
}

type progressRow struct {
	Index  int
	Status string
	ID     string
	Path   string
	Link   string
	Error  string
}

type fetchProgressPrinter struct {
	out         io.Writer
	interactive bool
	order       []int
	rows        map[int]*progressRow
	lineCount   int
}

func newFetchProgressPrinter(out io.Writer, planRows []csvplan.Row, interactive bool) *fetchProgressPrinter {
	rows := make(map[int]*progressRow, len(planRows))
	order := make([]int, len(planRows))
	for i, row := range planRows {
		idx := row.Index
		order[i] = idx
		rows[idx] = &progressRow{
			Index:  idx,
			Status: "pending",
			Link:   strings.TrimSpace(row.Link),
		}
	}
	return &fetchProgressPrinter{
		out:         out,
		interactive: interactive,
		order:       order,
		rows:        rows,
	}
}

func (p *fetchProgressPrinter) Interactive() bool {
	return p != nil && p.interactive
}

func (p *fetchProgressPrinter) Start(row csvplan.Row, force, noDownload bool) {
	if p == nil {
		return
	}
	state := p.ensureRow(row)
	state.Error = ""
	state.Path = ""
	state.ID = ""
	status := "resolving"
	link := strings.TrimSpace(row.Link)
	if isRemoteLink(link) {
		if force {
			status = "downloading"
		} else {
			status = "matching"
		}
	} else {
		status = "copying"
	}
	state.Status = status
	p.render()
}

func (p *fetchProgressPrinter) Complete(row csvplan.Row, res cache.ResolveResult) {
	if p == nil {
		return
	}
	state := p.ensureRow(row)
	state.Status = string(res.Status)
	state.Error = ""
	if res.ID != "" {
		state.ID = res.ID
	} else {
		state.ID = res.Identifier
	}
	state.Path = res.Entry.CachedPath
	p.render()
}

func (p *fetchProgressPrinter) Error(row csvplan.Row, err error) {
	if p == nil {
		return
	}
	state := p.ensureRow(row)
	state.Status = "error"
	if err != nil {
		state.Error = err.Error()
	} else {
		state.Error = ""
	}
	p.render()
}

func (p *fetchProgressPrinter) Finalize() {
	if !p.Interactive() {
		return
	}
	p.render()
}

func (p *fetchProgressPrinter) ensureRow(row csvplan.Row) *progressRow {
	if p.rows == nil {
		p.rows = map[int]*progressRow{}
	}
	state, ok := p.rows[row.Index]
	if !ok {
		state = &progressRow{Index: row.Index}
		p.rows[row.Index] = state
		p.order = append(p.order, row.Index)
	}
	if strings.TrimSpace(state.Link) == "" {
		state.Link = strings.TrimSpace(row.Link)
	}
	return state
}

func (p *fetchProgressPrinter) render() {
	if !p.Interactive() {
		return
	}
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "INDEX\tSTATUS\tID\tPATH\tLINK\tERROR")
	for _, idx := range p.order {
		state := p.rows[idx]
		id := nonEmptyOrDash(state.ID)
		path := nonEmptyOrDash(state.Path)
		errMsg := state.Error
		fmt.Fprintf(tw, "%03d\t%s\t%s\t%s\t%s\t%s\n",
			state.Index,
			state.Status,
			id,
			path,
			state.Link,
			errMsg,
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

func isRemoteLink(link string) bool {
	link = strings.ToLower(strings.TrimSpace(link))
	return strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://")
}
