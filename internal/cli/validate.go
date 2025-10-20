package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

var (
	validateIndexes []int
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Run project validations",
	}

	cmd.AddCommand(newValidateFilenamesCmd())
	cmd.AddCommand(newValidateSegmentsCmd())
	return cmd
}

func newValidateFilenamesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filenames",
		Short: "Validate cached filenames against the configured template",
		RunE:  runValidateFilenames,
	}

	cmd.Flags().IntSliceVar(&validateIndexes, "index", nil, "Limit validation to specific 1-based row index (repeat flag for multiple)")
	return cmd
}

func runValidateFilenames(cmd *cobra.Command, _ []string) error {
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

	if len(validateIndexes) > 0 {
		rows, err = filterRowsByIndex(rows, validateIndexes)
		if err != nil {
			return err
		}
	}

	if outputJSON && len(rows) == 0 {
		return writeNameValidationJSON(cmd, pp.Root, nil, filenameCheckSummary{})
	}

	svc, err := newCacheService(ctx, pp, nil, nil)
	if err != nil {
		return err
	}

	results, summary, dirty := collectFilenameChecks(pp, svc, idx, rows)
	if dirty {
		if err := cache.Save(pp, idx); err != nil {
			return fmt.Errorf("save cache index: %w", err)
		}
	}

	if outputJSON {
		if err := writeNameValidationJSON(cmd, pp.Root, results, summary); err != nil {
			return err
		}
	} else {
		writeNameValidationTable(cmd, pp.Root, results, summary)
	}

	issues := summary.Mismatched + summary.Missing + summary.Errors
	if issues > 0 {
		return fmt.Errorf("filename validation failed for %d row(s)", issues)
	}

	return nil
}

func collectFilenameChecks(pp paths.ProjectPaths, svc *cache.Service, idx *cache.Index, rows []csvplan.Row) ([]filenameCheckResult, filenameCheckSummary, bool) {
	results := make([]filenameCheckResult, 0, len(rows))
	summary := filenameCheckSummary{Total: len(rows)}
	dirty := false

	for _, row := range rows {
		res := filenameCheckResult{Index: row.Index}

		entry, ok := idx.Get(row.Index)
		if !ok || strings.TrimSpace(entry.CachedPath) == "" {
			res.Status = "not_cached"
			summary.NotCached++
			results = append(results, res)
			continue
		}

		expectedBase, err := svc.ExpectedFilenameBase(row, entry)
		if err != nil {
			res.Status = "error"
			res.Notes = append(res.Notes, err.Error())
			summary.Errors++
			results = append(results, res)
			continue
		}
		res.Expected = expectedBase

		res.CachedPath = entry.CachedPath
		res.Source = entry.Source
		ext := filepath.Ext(entry.CachedPath)
		actualBase := strings.TrimSuffix(filepath.Base(entry.CachedPath), ext)
		res.Actual = actualBase

		info, statErr := os.Stat(entry.CachedPath)
		if statErr != nil {
			res.Status = "missing_file"
			res.Notes = append(res.Notes, statErr.Error())
			summary.Missing++
			results = append(results, res)
			continue
		}
		if info.IsDir() {
			res.Status = "missing_file"
			res.Notes = append(res.Notes, "cached path is a directory")
			summary.Missing++
			results = append(results, res)
			continue
		}

		if rel, err := filepath.Rel(pp.SrcDir, entry.CachedPath); err == nil && strings.HasPrefix(rel, "..") {
			res.Notes = append(res.Notes, "cached file stored outside src dir")
		}

		expectedPath := filepath.Join(pp.SrcDir, expectedBase+ext)
		if samePath(entry.CachedPath, expectedPath) {
			res.Status = "match"
			summary.Matches++
			results = append(results, res)
			continue
		}

		if _, err := os.Stat(expectedPath); err == nil {
			res.Status = "error"
			res.Notes = append(res.Notes, "expected path already exists")
			summary.Errors++
			results = append(results, res)
			continue
		}

		if err := os.Rename(entry.CachedPath, expectedPath); err != nil {
			res.Status = "error"
			res.Notes = append(res.Notes, fmt.Sprintf("rename failed: %v", err))
			summary.Errors++
			results = append(results, res)
			continue
		}

		entry.CachedPath = expectedPath
		idx.Set(entry)
		dirty = true

		res.Status = "renamed"
		res.Notes = append(res.Notes, fmt.Sprintf("renamed to %s", filepath.Base(expectedPath)))
		res.CachedPath = expectedPath
		res.Actual = expectedBase
		summary.Renamed++
		results = append(results, res)
	}

	return results, summary, dirty
}

func writeNameValidationJSON(cmd *cobra.Command, project string, rows []filenameCheckResult, summary filenameCheckSummary) error {
	payload := struct {
		Project string                `json:"project"`
		Rows    []filenameCheckResult `json:"rows"`
		Summary filenameCheckSummary  `json:"summary"`
	}{
		Project: project,
		Rows:    rows,
		Summary: summary,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode validation json: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func writeNameValidationTable(cmd *cobra.Command, project string, rows []filenameCheckResult, summary filenameCheckSummary) {
	fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\n", project)

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "INDEX\tSTATUS\tEXPECTED\tACTUAL\tPATH\tNOTES")
	for _, row := range rows {
		note := strings.Join(row.Notes, "; ")
		fmt.Fprintf(w, "%03d\t%s\t%s\t%s\t%s\t%s\n",
			row.Index,
			row.Status,
			row.Expected,
			row.Actual,
			row.CachedPath,
			note,
		)
	}
	w.Flush()

	fmt.Fprintf(cmd.OutOrStdout(), "Matches: %d, Renamed: %d, Mismatched: %d, Missing: %d, Not Cached: %d, Errors: %d\n",
		summary.Matches, summary.Renamed, summary.Mismatched, summary.Missing, summary.NotCached, summary.Errors,
	)
}

func matchTemplateBase(pattern, actual string) bool {
	pattern = strings.TrimSpace(pattern)
	actual = strings.TrimSpace(actual)
	if pattern == "" {
		return actual == ""
	}
	if !strings.Contains(pattern, "%(") {
		return pattern == actual
	}

	segments, placeholderCount := splitTemplatePattern(pattern)
	if len(segments) == 0 {
		return actual != ""
	}
	if segments[0] != "" && !strings.HasPrefix(actual, segments[0]) {
		return false
	}

	pos := len(segments[0])
	if len(segments) > 2 {
		for _, segment := range segments[1 : len(segments)-1] {
			if segment == "" {
				continue
			}
			idx := strings.Index(actual[pos:], segment)
			if idx == -1 {
				return false
			}
			pos += idx + len(segment)
		}
	}

	last := segments[len(segments)-1]
	if last != "" && !strings.HasSuffix(actual, last) {
		return false
	}

	if placeholderCount > 0 {
		staticLen := 0
		for _, seg := range segments {
			staticLen += len(seg)
		}
		if len(actual) <= staticLen {
			return false
		}
		if len(actual)-staticLen < placeholderCount {
			return false
		}
	}
	return true
}

func splitTemplatePattern(pattern string) ([]string, int) {
	segments := make([]string, 0)
	placeholderCount := 0
	cursor := 0
	for cursor < len(pattern) {
		start := strings.Index(pattern[cursor:], "%(")
		if start == -1 {
			segments = append(segments, pattern[cursor:])
			return segments, placeholderCount
		}
		start += cursor
		segments = append(segments, pattern[cursor:start])
		end := strings.Index(pattern[start:], ")s")
		if end == -1 {
			segments[len(segments)-1] += pattern[start:]
			return segments, placeholderCount
		}
		placeholderCount++
		cursor = start + end + 2
		if cursor == len(pattern) {
			segments = append(segments, "")
		}
	}
	if len(segments) == 0 {
		segments = append(segments, "")
	}
	return segments, placeholderCount
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return strings.EqualFold(a, b)
}

type filenameCheckResult struct {
	Index      int      `json:"index"`
	Status     string   `json:"status"`
	Expected   string   `json:"expected_base"`
	Actual     string   `json:"actual_base,omitempty"`
	CachedPath string   `json:"cached_path,omitempty"`
	Source     string   `json:"source,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

type filenameCheckSummary struct {
	Total      int `json:"total"`
	Matches    int `json:"matches"`
	Mismatched int `json:"mismatched"`
	Missing    int `json:"missing"`
	NotCached  int `json:"not_cached"`
	Errors     int `json:"errors"`
	Renamed    int `json:"renamed"`
}
