package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

var (
	validateCollectionName string
)

func newValidateCollectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collection",
		Short: "Validate a specific collection with detailed row information",
		RunE:  runValidateCollection,
	}

	cmd.Flags().StringVar(&validateCollectionName, "collection", "", "Collection name to validate (required)")
	cmd.MarkFlagRequired("collection")

	return cmd
}

func runValidateCollection(cmd *cobra.Command, _ []string) error {
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
	pp = paths.ApplyGlobalCache(pp, cfg.GlobalCacheEnabled())

	// Ensure collections are configured
	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured in project")
	}

	// Load collection resolver
	collResolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return err
	}

	collections, err := collResolver.LoadCollections()
	if err != nil {
		return err
	}

	// Find the requested collection
	collection, ok := collections[validateCollectionName]
	if !ok {
		available := make([]string, 0, len(collections))
		for name := range collections {
			available = append(available, name)
		}
		return fmt.Errorf("collection %q not found; available: %v", validateCollectionName, available)
	}

	// Load cache index
	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	// Build clips for this collection
	clips, err := collResolver.BuildCollectionClips(map[string]project.Collection{validateCollectionName: collection})
	if err != nil {
		return err
	}

	// Build validation results
	results := make([]collectionValidationRow, 0, len(clips))
	for _, collClip := range clips {
		result := validateCollectionRow(pp, idx, collResolver, collClip)
		results = append(results, result)
	}

	// Output results
	if outputJSON {
		return writeCollectionValidationJSON(cmd, validateCollectionName, collection, results)
	}
	writeCollectionValidationTable(cmd, validateCollectionName, collection, results)
	return nil
}

func validateCollectionRow(pp paths.ProjectPaths, idx *cache.Index, resolver *project.CollectionResolver, collClip project.CollectionClip) collectionValidationRow {
	clip := collClip.Clip
	row := clip.Row

	result := collectionValidationRow{
		Index:        row.Index,
		Link:         row.Link,
		StartTime:    row.StartRaw,
		Duration:     row.DurationSeconds,
		CustomFields: row.CustomFields,
	}

	// Determine expected identifier for cache lookup
	expectedID := determineExpectedIdentifier(pp, row)
	result.ExpectedID = expectedID

	// Resolve cache entry
	entry, hasEntry, err := resolveEntryForRow(pp, idx, row)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}

	if !hasEntry || strings.TrimSpace(entry.CachedPath) == "" {
		result.Status = "missing"
		result.ExpectedFile = expectedID
		return result
	}

	result.CacheFile = entry.CachedPath
	result.ActualID = entry.Identifier

	// Check if file exists
	info, statErr := os.Stat(entry.CachedPath)
	if statErr != nil {
		result.Status = "missing"
		result.ExpectedFile = entry.CachedPath
		result.Error = statErr.Error()
		return result
	}
	if info.IsDir() {
		result.Status = "error"
		result.Error = "cached path is a directory"
		return result
	}

	// Get profile and segments if configured
	if clip.OverlayProfile != "" {
		profile, hasProfile := resolver.Profile(clip.OverlayProfile)
		if !hasProfile {
			result.Status = "error"
			result.Error = fmt.Sprintf("unknown overlay profile: %s", clip.OverlayProfile)
			return result
		}

		segments := profile.ResolveSegments()
		result.Segments = formatSegments(segments)
	}

	// Build output path
	result.OutputPath = buildOutputPath(pp, collClip, row)

	result.Status = "valid"
	return result
}

func determineExpectedIdentifier(pp paths.ProjectPaths, row csvplan.Row) string {
	link := strings.TrimSpace(row.Link)
	if link == "" {
		return ""
	}

	// Check if it's a URL
	if parsed, err := url.Parse(link); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return link
	}

	// Otherwise it's a file path - resolve to absolute path
	path := link
	if !filepath.IsAbs(path) {
		path = filepath.Join(pp.Root, link)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return link
	}
	return abs
}

func buildOutputPath(pp paths.ProjectPaths, collClip project.CollectionClip, row csvplan.Row) string {
	// Use collection output directory
	outputDir := collClip.OutputDir

	// Generate filename based on index
	filename := fmt.Sprintf("%03d.mp4", row.Index)

	return filepath.Join(outputDir, filename)
}

func formatSegments(segments []config.OverlaySegment) string {
	if len(segments) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg.Name != "" {
			parts = append(parts, seg.Name)
		} else if seg.Template != "" {
			parts = append(parts, "template")
		}
	}
	return strings.Join(parts, ", ")
}

func writeCollectionValidationJSON(cmd *cobra.Command, collectionName string, collection project.Collection, rows []collectionValidationRow) error {
	payload := struct {
		Collection string                     `json:"collection"`
		Plan       string                     `json:"plan"`
		Profile    string                     `json:"profile,omitempty"`
		Rows       []collectionValidationRow  `json:"rows"`
		Summary    collectionValidationSummary `json:"summary"`
	}{
		Collection: collectionName,
		Plan:       collection.Plan,
		Profile:    collection.Profile,
		Rows:       rows,
		Summary:    buildValidationSummary(rows),
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode validation json: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func writeCollectionValidationTable(cmd *cobra.Command, collectionName string, collection project.Collection, rows []collectionValidationRow) {
	fmt.Fprintf(cmd.OutOrStdout(), "Collection: %s\n", collectionName)
	fmt.Fprintf(cmd.OutOrStdout(), "Plan: %s\n", collection.Plan)
	if collection.Profile != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Profile: %s\n", collection.Profile)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "INDEX\tSTATUS\tCACHE FILE / EXPECTED ID\tSEGMENTS\tOUTPUT\tDATA")

	for _, row := range rows {
		// Format cache file or expected ID for missing
		cacheFileDisplay := row.CacheFile
		if cacheFileDisplay != "" {
			cacheFileDisplay = filepath.Base(cacheFileDisplay)
		} else if row.ExpectedFile != "" {
			// Use italics-style markers for missing files
			shortID := row.ExpectedFile
			if len(shortID) > 50 {
				shortID = "..." + shortID[len(shortID)-47:]
			}
			cacheFileDisplay = fmt.Sprintf("*%s*", shortID)
		} else {
			cacheFileDisplay = "*missing*"
		}

		// Format output path
		outputPath := "-"
		if row.OutputPath != "" {
			outputPath = filepath.Base(row.OutputPath)
		}

		// Format dynamic data as compact JSON
		dynamicData := formatDynamicData(row.CustomFields)

		// Format segments
		segments := row.Segments
		if segments == "" {
			segments = "-"
		}

		// Format status
		status := row.Status
		if row.Error != "" {
			status = fmt.Sprintf("%s: %s", status, truncateString(row.Error, 30))
		}

		fmt.Fprintf(w, "%03d\t%s\t%s\t%s\t%s\t%s\n",
			row.Index,
			status,
			truncateString(cacheFileDisplay, 50),
			truncateString(segments, 30),
			outputPath,
			truncateString(dynamicData, 40),
		)
	}
	w.Flush()

	// Print summary
	summary := buildValidationSummary(rows)
	fmt.Fprintf(cmd.OutOrStdout(), "\nSummary: %d valid, %d missing, %d errors\n",
		summary.Valid, summary.Missing, summary.Errors)
}

func formatDynamicData(customFields map[string]string) string {
	if len(customFields) == 0 {
		return "{}"
	}

	// Create a compact JSON representation
	data, err := json.Marshal(customFields)
	if err != nil {
		return "{}"
	}

	return string(data)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func buildValidationSummary(rows []collectionValidationRow) collectionValidationSummary {
	summary := collectionValidationSummary{}
	for _, row := range rows {
		switch row.Status {
		case "valid":
			summary.Valid++
		case "missing":
			summary.Missing++
		case "error":
			summary.Errors++
		}
	}
	summary.Total = len(rows)
	return summary
}

type collectionValidationRow struct {
	Index        int               `json:"index"`
	Status       string            `json:"status"`
	Link         string            `json:"link"`
	StartTime    string            `json:"start_time"`
	Duration     int               `json:"duration"`
	CacheFile    string            `json:"cache_file,omitempty"`
	ExpectedFile string            `json:"expected_file,omitempty"`
	ExpectedID   string            `json:"expected_id,omitempty"`
	ActualID     string            `json:"actual_id,omitempty"`
	Segments     string            `json:"segments,omitempty"`
	OutputPath   string            `json:"output_path,omitempty"`
	CustomFields map[string]string `json:"custom_fields,omitempty"`
	Error        string            `json:"error,omitempty"`
}

type collectionValidationSummary struct {
	Total   int `json:"total"`
	Valid   int `json:"valid"`
	Missing int `json:"missing"`
	Errors  int `json:"errors"`
}
