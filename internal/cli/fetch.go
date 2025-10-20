package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

var (
	fetchForce   bool
	fetchReprobe bool
	fetchIndexes []int
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
	cmd.Flags().IntSliceVar(&fetchIndexes, "index", nil, "Limit fetch to specific 1-based row index (repeat flag for multiple)")

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

	if len(fetchIndexes) > 0 {
		rows, err = filterRowsByIndex(rows, fetchIndexes)
		if err != nil {
			return err
		}
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

	opts := cache.ResolveOptions{Force: fetchForce, Reprobe: fetchReprobe}

	outcomes := make([]fetchRowResult, 0, len(rows))
	counts := fetchCounts{}
	dirty := false

	for _, row := range rows {
		result, err := svc.Resolve(ctx, idx, row, opts)
		if err != nil {
			return err
		}

		switch result.Status {
		case cache.ResolveStatusDownloaded:
			counts.Downloaded++
		case cache.ResolveStatusCopied:
			counts.Copied++
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
	return nil
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
	fmt.Fprintln(w, "INDEX\tSTATUS\tBYTES\tPROBED\tPATH")
	for _, row := range rows {
		fmt.Fprintf(w, "%03d\t%s\t%d\t%v\t%s\n",
			row.Index,
			row.Status,
			row.SizeBytes,
			row.Probed,
			row.CachedPath,
		)
	}
	w.Flush()

	fmt.Fprintf(cmd.OutOrStdout(), "Downloaded: %d, Copied: %d, Cached: %d, Probed: %d\n",
		counts.Downloaded, counts.Copied, counts.Cached, counts.Probed,
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
}

type fetchCounts struct {
	Downloaded int `json:"downloaded"`
	Copied     int `json:"copied"`
	Cached     int `json:"cached"`
	Probed     int `json:"probed"`
}
