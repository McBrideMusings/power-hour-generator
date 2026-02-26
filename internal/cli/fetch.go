package cli

import (
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
	"powerhour/internal/tui"
	"powerhour/pkg/csvplan"
)

var (
	fetchForce      bool
	fetchReprobe    bool
	fetchNoDownload bool
	fetchNoProgress bool
	fetchIndexArg   []string
)

var newCacheServiceWithStatus = cache.NewServiceWithStatus

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
	addCollectionFetchFlags(cmd)

	return cmd
}

func runFetch(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	glog, gcloser, _ := logx.NewGlobal("fetch")
	if gcloser != nil {
		defer gcloser.Close()
	}
	glogf := func(format string, v ...any) {
		if glog != nil {
			glog.Printf(format, v...)
		}
	}
	glogf("fetch started")

	status := tui.NewStatusWriter(cmd.ErrOrStderr())
	defer status.Stop()

	status.Update("Resolving project...")
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}
	glogf("project resolved: %s", pp.Root)

	status.Update("Loading config...")
	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())
	glogf("config loaded")

	exists, err := paths.DirExists(pp.Root)
	if err != nil {
		return fmt.Errorf("stat project dir: %w", err)
	}
	if !exists {
		return fmt.Errorf("project directory does not exist: %s", pp.Root)
	}

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	glogf("routing to collection fetch (%d collections)", len(cfg.Collections))
	return runCollectionFetch(ctx, cmd, pp, cfg, glog, status)
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
	fmt.Fprintln(w, "TYPE\tINDEX\tSTATUS\tID\tPATH\tLINK\tERROR")
	for _, row := range rows {
		id := row.MediaID
		if id == "" {
			id = row.Identifier
		}
		fmt.Fprintf(w, "%s\t%03d\t%s\t%s\t%s\t%s\t%s\n",
			row.ClipType,
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
	ClipType   string `json:"clip_type"`
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
		fmt.Fprintf(cmd.OutOrStdout(), "  %s %03d %s (%s): %s\n", row.ClipType, row.Index, row.Title, row.Link, row.Error)
	}
}

func printFetchSummary(w io.Writer, counts fetchCounts) {
	fmt.Fprintf(w, "Downloaded: %d, Copied: %d, Matched: %d, Reused: %d, Missing: %d, Probed: %d, Failed: %d\n",
		counts.Downloaded, counts.Copied, counts.Matched, counts.Reused, counts.Missing, counts.Probed, counts.Failed,
	)
}

func isRemoteLink(link string) bool {
	link = strings.ToLower(strings.TrimSpace(link))
	return strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://")
}
