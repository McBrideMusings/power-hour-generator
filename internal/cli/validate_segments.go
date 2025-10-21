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
	"powerhour/internal/render"
	"powerhour/pkg/csvplan"
)

var (
	validateSegmentIndexes []int
)

func newValidateSegmentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "segments",
		Short: "Validate rendered segment filenames against the configured template",
		RunE:  runValidateSegments,
	}

	cmd.Flags().IntSliceVar(&validateSegmentIndexes, "index", nil, "Limit validation to specific 1-based row index (repeat flag for multiple)")
	return cmd
}

func runValidateSegments(cmd *cobra.Command, _ []string) error {
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

	if len(validateSegmentIndexes) > 0 {
		rows, err = filterRowsByIndex(rows, validateSegmentIndexes)
		if err != nil {
			return err
		}
	}

	results, summary := collectSegmentChecks(pp, cfg, idx, rows)

	if outputJSON {
		if err := writeSegmentValidationJSON(cmd, pp.Root, results, summary); err != nil {
			return err
		}
	} else {
		writeSegmentValidationTable(cmd, pp.Root, results, summary)
	}

	issues := summary.Missing + summary.Errors
	if issues > 0 {
		return fmt.Errorf("segment validation reported %d issue(s)", issues)
	}

	return nil
}

func collectSegmentChecks(pp paths.ProjectPaths, cfg config.Config, idx *cache.Index, rows []csvplan.Row) ([]segmentValidationResult, segmentValidationSummary) {
	results := make([]segmentValidationResult, 0, len(rows))
	summary := segmentValidationSummary{Total: len(rows)}

	template := cfg.SegmentFilenameTemplate()
	if template == "" {
		template = config.Default().SegmentFilenameTemplate()
	}

	for _, row := range rows {
		res := segmentValidationResult{
			Index: row.Index,
		}

		entry, ok, err := resolveEntryForRow(pp, idx, row)
		if err != nil {
			res.Status = "error"
			res.Notes = append(res.Notes, err.Error())
			summary.Errors++
			results = append(results, res)
			continue
		}
		if !ok || strings.TrimSpace(entry.CachedPath) == "" {
			res.Status = "not_cached"
			res.Notes = append(res.Notes, "cache entry not available; skip segment rename")
			summary.NotCached++
			results = append(results, res)
			continue
		}

		seg := render.Segment{
			Row:        row,
			CachedPath: entry.CachedPath,
			Entry:      entry,
		}

		expectedBase := render.SegmentBaseName(template, seg)
		expectedVideo := filepath.Join(pp.SegmentsDir, expectedBase+".mp4")
		expectedLog := filepath.Join(pp.LogsDir, expectedBase+".log")

		res.ExpectedBase = expectedBase
		res.ExpectedVideo = expectedVideo
		res.ExpectedLog = expectedLog

		exists, err := paths.FileExists(expectedVideo)
		if err != nil {
			res.Status = "error"
			res.Notes = append(res.Notes, fmt.Sprintf("stat expected segment: %v", err))
			summary.Errors++
			results = append(results, res)
			continue
		}
		if exists {
			res.Status = "match"
			res.ActualVideo = expectedVideo
			res.ActualBase = expectedBase
			summary.Matches++
			results = append(results, res)
			continue
		}

		candidate, notes, err := locateSegmentCandidate(pp, row, expectedVideo)
		if err != nil {
			res.Status = "error"
			res.Notes = append(res.Notes, err.Error())
			summary.Errors++
			results = append(results, res)
			continue
		}
		if len(notes) > 0 {
			res.Notes = append(res.Notes, notes...)
		}

		if candidate == "" {
			res.Status = "missing"
			summary.Missing++
			results = append(results, res)
			continue
		}

		res.ActualVideo = candidate
		res.ActualBase = strings.TrimSuffix(filepath.Base(candidate), filepath.Ext(candidate))

		if err := os.Rename(candidate, expectedVideo); err != nil {
			res.Status = "error"
			res.Notes = append(res.Notes, fmt.Sprintf("rename failed: %v", err))
			summary.Errors++
			results = append(results, res)
			continue
		}

		res.Status = "renamed"
		res.ActualVideo = expectedVideo
		res.ActualBase = expectedBase
		res.Notes = append(res.Notes, fmt.Sprintf("renamed to %s", filepath.Base(expectedVideo)))
		summary.Renamed++

		oldLog := filepath.Join(pp.LogsDir, strings.TrimSuffix(filepath.Base(candidate), filepath.Ext(candidate))+".log")
		if samePath(oldLog, expectedLog) {
			results = append(results, res)
			continue
		}

		oldLogExists, err := paths.FileExists(oldLog)
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("stat existing log failed: %v", err))
			results = append(results, res)
			continue
		}
		if !oldLogExists {
			results = append(results, res)
			continue
		}

		expectedLogExists, err := paths.FileExists(expectedLog)
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("stat expected log failed: %v", err))
			results = append(results, res)
			continue
		}
		if expectedLogExists {
			res.Notes = append(res.Notes, fmt.Sprintf("log already exists at %s; left original file", filepath.Base(expectedLog)))
			results = append(results, res)
			continue
		}

		if err := os.Rename(oldLog, expectedLog); err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("log rename failed: %v", err))
			results = append(results, res)
			continue
		}
		res.Notes = append(res.Notes, fmt.Sprintf("log renamed to %s", filepath.Base(expectedLog)))
		results = append(results, res)
	}

	return results, summary
}

func locateSegmentCandidate(pp paths.ProjectPaths, row csvplan.Row, expected string) (string, []string, error) {
	notes := []string{}

	legacyBase := render.SegmentBaseName("", render.Segment{Row: row})
	legacyPath := filepath.Join(pp.SegmentsDir, legacyBase+".mp4")
	if !samePath(legacyPath, expected) {
		if exists, err := paths.FileExists(legacyPath); err != nil {
			return "", nil, fmt.Errorf("stat legacy segment: %w", err)
		} else if exists {
			return legacyPath, notes, nil
		}
	}

	pattern := filepath.Join(pp.SegmentsDir, fmt.Sprintf("%03d*.mp4", row.Index))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", nil, fmt.Errorf("glob segment pattern: %w", err)
	}

	candidates := make([]string, 0, len(matches))
	for _, path := range matches {
		if samePath(path, expected) {
			continue
		}
		exists, err := paths.FileExists(path)
		if err != nil {
			return "", nil, fmt.Errorf("stat candidate segment: %w", err)
		}
		if exists {
			candidates = append(candidates, path)
		}
	}

	if len(candidates) == 0 {
		return "", notes, nil
	}

	if len(candidates) == 1 {
		return candidates[0], notes, nil
	}

	var names []string
	for _, path := range candidates {
		names = append(names, filepath.Base(path))
	}
	notes = append(notes, fmt.Sprintf("multiple candidate segment files: %s", strings.Join(names, ", ")))
	return "", notes, nil
}

func writeSegmentValidationJSON(cmd *cobra.Command, project string, rows []segmentValidationResult, summary segmentValidationSummary) error {
	payload := struct {
		Project string                    `json:"project"`
		Rows    []segmentValidationResult `json:"rows"`
		Summary segmentValidationSummary  `json:"summary"`
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

func writeSegmentValidationTable(cmd *cobra.Command, project string, rows []segmentValidationResult, summary segmentValidationSummary) {
	fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\n", project)

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "INDEX\tSTATUS\tEXPECTED_BASE\tACTUAL_BASE\tVIDEO_PATH\tNOTES")
	for _, row := range rows {
		note := strings.Join(row.Notes, "; ")
		fmt.Fprintf(w, "%03d\t%s\t%s\t%s\t%s\t%s\n",
			row.Index,
			row.Status,
			row.ExpectedBase,
			row.ActualBase,
			row.ActualVideo,
			note,
		)
	}
	w.Flush()

	fmt.Fprintf(cmd.OutOrStdout(), "Matches: %d, Renamed: %d, Missing: %d, Not Cached: %d, Errors: %d\n",
		summary.Matches, summary.Renamed, summary.Missing, summary.NotCached, summary.Errors,
	)
}

type segmentValidationResult struct {
	Index         int      `json:"index"`
	Status        string   `json:"status"`
	ExpectedBase  string   `json:"expected_base"`
	ExpectedVideo string   `json:"expected_video"`
	ExpectedLog   string   `json:"expected_log"`
	ActualBase    string   `json:"actual_base,omitempty"`
	ActualVideo   string   `json:"actual_video,omitempty"`
	Notes         []string `json:"notes,omitempty"`
}

type segmentValidationSummary struct {
	Total     int `json:"total"`
	Matches   int `json:"matches"`
	Renamed   int `json:"renamed"`
	Missing   int `json:"missing"`
	NotCached int `json:"not_cached"`
	Errors    int `json:"errors"`
}
