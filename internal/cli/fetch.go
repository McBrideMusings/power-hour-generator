package cli

import (
	"context"
	"encoding/json"
	"fmt"
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
	fetchForce    bool
	fetchReprobe  bool
	fetchDryRun   bool
	fetchIndexArg []string
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
	cmd.Flags().BoolVar(&fetchDryRun, "dry-run", false, "Preview fetch actions without downloading or copying")
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

	opts := cache.ResolveOptions{Force: fetchForce, Reprobe: fetchReprobe, DryRun: fetchDryRun}

	outcomes := make([]fetchRowResult, 0, len(rows))
	counts := fetchCounts{}
	dirty := false

	for _, row := range rows {
		result, err := svc.Resolve(ctx, idx, row, opts)
		if err != nil {
			counts.Failed++
			logger.Printf("fetch row %03d %q failed: %v", row.Index, row.Title, err)
			fmt.Fprintf(cmd.ErrOrStderr(), "fetch row %03d %q failed: %v\n", row.Index, row.Title, err)
			outcomes = append(outcomes, fetchRowResult{
				Index:  row.Index,
				Title:  row.Title,
				Status: "error",
				Source: row.Link,
				Error:  err.Error(),
			})
			continue
		}

		switch result.Status {
		case cache.ResolveStatusDownloaded:
			counts.Downloaded++
		case cache.ResolveStatusCopied:
			counts.Copied++
		case cache.ResolveStatusWouldDownload, cache.ResolveStatusWouldCopy:
			counts.Pending++
		default:
			counts.Cached++
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
			Source:     result.Entry.Source,
			SizeBytes:  result.Entry.SizeBytes,
			Probed:     result.Probed,
		})
	}

	if dirty {
		if err := cache.Save(pp, idx); err != nil {
			return err
		}
	}

	if outputJSON {
		return writeFetchJSON(cmd, pp.Root, outcomes, counts)
	}

	writeFetchTable(cmd, pp.Root, outcomes, counts)
	if counts.Failed > 0 {
		writeFetchFailures(cmd, outcomes)
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
	fmt.Fprintln(w, "INDEX\tSTATUS\tBYTES\tPROBED\tPATH\tERROR")
	for _, row := range rows {
		fmt.Fprintf(w, "%03d\t%s\t%d\t%v\t%s\t%s\n",
			row.Index,
			row.Status,
			row.SizeBytes,
			row.Probed,
			row.CachedPath,
			row.Error,
		)
	}
	w.Flush()

	fmt.Fprintf(cmd.OutOrStdout(), "Downloaded: %d, Copied: %d, Cached: %d, Pending: %d, Probed: %d, Failed: %d\n",
		counts.Downloaded, counts.Copied, counts.Cached, counts.Pending, counts.Probed, counts.Failed,
	)
}

type fetchRowResult struct {
	Index      int    `json:"index"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	CachedPath string `json:"cached_path"`
	Source     string `json:"source"`
	SizeBytes  int64  `json:"size_bytes"`
	Probed     bool   `json:"probed"`
	Error      string `json:"error,omitempty"`
}

type fetchCounts struct {
	Downloaded int `json:"downloaded"`
	Copied     int `json:"copied"`
	Cached     int `json:"cached"`
	Pending    int `json:"pending"`
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
		fmt.Fprintf(cmd.OutOrStdout(), "  %03d %s: %s\n", row.Index, row.Title, row.Error)
	}
}
