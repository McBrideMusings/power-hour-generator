package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/state"
)

var cleanDryRun bool

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove derived artifacts from the project",
	}

	cmd.PersistentFlags().BoolVar(&cleanDryRun, "dry-run", false, "List what would be removed without deleting")

	cmd.AddCommand(newCleanSegmentsCmd())
	cmd.AddCommand(newCleanLogsCmd())
	cmd.AddCommand(newCleanOrphansCmd())
	cmd.AddCommand(newCleanAllCmd())

	return cmd
}

func newCleanSegmentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "segments",
		Short: "Remove all rendered segments and render state",
		RunE:  runCleanSegments,
	}
}

func newCleanLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Remove all log files",
		RunE:  runCleanLogs,
	}
}

func newCleanOrphansCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "orphans",
		Short: "Remove segment files not in the current plan",
		RunE:  runCleanOrphans,
	}
}

func newCleanAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "all",
		Short: "Remove segments, logs, render state, and concat artifacts",
		RunE:  runCleanAll,
	}
}

type cleanResult struct {
	Removed    int   `json:"removed"`
	FreedBytes int64 `json:"freed_bytes"`
	Skipped    int   `json:"skipped"`
	DryRun     bool  `json:"dry_run"`
}

func runCleanSegments(cmd *cobra.Command, _ []string) error {
	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	result := cleanResult{DryRun: cleanDryRun}

	removeGlob(pp.SegmentsDir, "**/*.mp4", out, &result)
	removeSingleFile(pp.RenderStateFile, out, &result)

	return writeCleanResult(out, "segments", result)
}

func runCleanLogs(cmd *cobra.Command, _ []string) error {
	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	result := cleanResult{DryRun: cleanDryRun}

	removeGlob(pp.LogsDir, "*", out, &result)

	return writeCleanResult(out, "logs", result)
}

func runCleanOrphans(cmd *cobra.Command, _ []string) error {
	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	expected, err := buildExpectedPaths(pp, cfg)
	if err != nil {
		return err
	}

	actual, err := globFiles(pp.SegmentsDir, "**/*.mp4")
	if err != nil {
		return err
	}

	orphans := diffPaths(actual, expected)
	sort.Strings(orphans)

	out := cmd.OutOrStdout()
	result := cleanResult{DryRun: cleanDryRun}

	for _, path := range orphans {
		removeFileEntry(path, out, &result)
	}

	// Prune render state
	if !cleanDryRun {
		rs, err := state.Load(pp.RenderStateFile)
		if err != nil {
			return err
		}
		state.Prune(rs, expected)
		if err := rs.Save(pp.RenderStateFile); err != nil {
			return fmt.Errorf("save render state: %w", err)
		}
	}

	return writeCleanResult(out, "orphans", result)
}

func runCleanAll(cmd *cobra.Command, _ []string) error {
	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	result := cleanResult{DryRun: cleanDryRun}

	removeGlob(pp.SegmentsDir, "**/*.mp4", out, &result)
	removeGlob(pp.LogsDir, "*", out, &result)
	removeSingleFile(pp.RenderStateFile, out, &result)
	removeSingleFile(pp.ConcatListFile, out, &result)

	concatGlob, _ := filepath.Glob(filepath.Join(pp.MetaDir, "concat*.txt"))
	for _, f := range concatGlob {
		removeFileEntry(f, out, &result)
	}

	return writeCleanResult(out, "all", result)
}

func resolveCleanPaths() (paths.ProjectPaths, error) {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return pp, err
	}
	exists, err := paths.DirExists(pp.Root)
	if err != nil {
		return pp, fmt.Errorf("stat project dir: %w", err)
	}
	if !exists {
		return pp, fmt.Errorf("project directory does not exist: %s", pp.Root)
	}
	return pp, nil
}

func buildExpectedPaths(pp paths.ProjectPaths, cfg config.Config) (map[string]bool, error) {
	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return nil, err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		return nil, err
	}

	clips, err := resolver.BuildCollectionClips(collections)
	if err != nil {
		return nil, err
	}

	expected := make(map[string]bool, len(clips))
	tmpl := cfg.SegmentFilenameTemplate()

	for _, collClip := range clips {
		clip := collClip.Clip
		clip.Row.DurationSeconds = clip.DurationSeconds
		if clip.Row.Index <= 0 {
			clip.Row.Index = clip.TypeIndex
			if clip.Row.Index <= 0 {
				clip.Row.Index = clip.Sequence
			}
		}

		var prof project.ResolvedProfile
		var segments []config.OverlaySegment
		if clip.OverlayProfile != "" {
			if p, ok := resolver.Profile(clip.OverlayProfile); ok {
				prof = p
				segments = p.ResolveSegments()
			}
		}

		seg := render.Segment{
			Clip:     clip,
			Profile:  prof,
			Segments: segments,
		}

		outputDir := collClip.OutputDir
		if !filepath.IsAbs(outputDir) {
			outputDir = filepath.Join(pp.SegmentsDir, outputDir)
		}
		baseName := render.SegmentBaseName(tmpl, seg)
		outputPath := filepath.Join(outputDir, baseName+".mp4")
		expected[outputPath] = true
	}

	return expected, nil
}

func globFiles(root, pattern string) ([]string, error) {
	exists, err := paths.DirExists(root)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	var matches []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if pattern == "**/*.mp4" {
			if matched, _ := filepath.Match("*.mp4", filepath.Base(path)); matched {
				matches = append(matches, path)
			}
		} else if pattern == "*" {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}

func diffPaths(actual []string, expected map[string]bool) []string {
	var orphans []string
	for _, path := range actual {
		if !expected[path] {
			orphans = append(orphans, path)
		}
	}
	return orphans
}

func removeGlob(root, pattern string, out io.Writer, result *cleanResult) {
	files, err := globFiles(root, pattern)
	if err != nil {
		return
	}
	for _, path := range files {
		removeFileEntry(path, out, result)
	}
}

func removeSingleFile(path string, out io.Writer, result *cleanResult) {
	exists, err := paths.FileExists(path)
	if err != nil || !exists {
		return
	}
	removeFileEntry(path, out, result)
}

func removeFileEntry(path string, out io.Writer, result *cleanResult) {
	info, err := os.Stat(path)
	if err != nil {
		result.Skipped++
		return
	}
	size := info.Size()

	if cleanDryRun {
		fmt.Fprintf(out, "would remove %s (%s)\n", path, formatSize(size))
		result.Removed++
		result.FreedBytes += size
		return
	}

	if err := os.Remove(path); err != nil {
		if !outputJSON {
			fmt.Fprintf(out, "error removing %s: %v\n", path, err)
		}
		result.Skipped++
		return
	}

	result.Removed++
	result.FreedBytes += size
	if !outputJSON {
		fmt.Fprintf(out, "removed %s (%s)\n", path, formatSize(size))
	}
}

func writeCleanResult(out io.Writer, label string, result cleanResult) error {
	if outputJSON {
		return json.NewEncoder(out).Encode(result)
	}

	action := "complete"
	if cleanDryRun {
		action = "(dry run)"
	}
	fmt.Fprintf(out, "\nClean %s %s: %d removed, %s freed, %d skipped\n",
		label, action, result.Removed, formatSize(result.FreedBytes), result.Skipped)
	return nil
}
