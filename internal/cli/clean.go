package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/playback"
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
	cmd.AddCommand(newCleanStaleLocalCopiesCmd())
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

func newCleanStaleLocalCopiesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stale-local-copies",
		Short: "Remove duplicate cache files for local-sourced entries",
		RunE:  runCleanStaleLocalCopies,
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
	glogf, gcloser := logx.StartCommand("clean-segments")
	defer gcloser.Close()
	glogf("clean segments started (dry_run=%v)", cleanDryRun)

	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}
	glogf("project resolved: %s", pp.Root)

	out := cmd.OutOrStdout()
	result := cleanResult{DryRun: cleanDryRun}

	removeGlob(pp.SegmentsDir, "**/*.mp4", out, &result)
	removeSingleFile(pp.RenderStateFile, out, &result)

	glogf("clean segments finished: %d removed, %d skipped", result.Removed, result.Skipped)
	return writeCleanResult(out, "segments", result)
}

func runCleanLogs(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("clean-logs")
	defer gcloser.Close()
	glogf("clean logs started (dry_run=%v)", cleanDryRun)

	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}
	glogf("project resolved: %s", pp.Root)

	out := cmd.OutOrStdout()
	result := cleanResult{DryRun: cleanDryRun}

	removeGlob(pp.LogsDir, "*", out, &result)

	glogf("clean logs finished: %d removed, %d skipped", result.Removed, result.Skipped)
	return writeCleanResult(out, "logs", result)
}

func runCleanOrphans(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("clean-orphans")
	defer gcloser.Close()
	glogf("clean orphans started (dry_run=%v)", cleanDryRun)

	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}
	glogf("project resolved: %s", pp.Root)

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	if len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	expectedSegments, err := buildExpectedSegments(pp, cfg)
	if err != nil {
		return err
	}
	expected := make(map[string]bool, len(expectedSegments))
	for _, seg := range expectedSegments {
		expected[seg.OutputPath] = true
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

	glogf("found %d orphans", len(orphans))

	// Prune render state
	if !cleanDryRun {
		rs, err := state.Load(pp.RenderStateFile)
		if err != nil {
			return err
		}
		// Prune keys on row identity, not on output path, so the keep-set has
		// to be built the same way. The scope is every collection rather than
		// the whole project because buildExpectedSegments resolves collection
		// clips only — an inline file: entry never appears in the keep-set, so
		// claiming authority over its path: key would delete it as stale.
		keep := make(map[string]bool, len(expectedSegments))
		for _, key := range state.SegmentKeys(expectedSegments) {
			keep[key] = true
		}
		names := make([]string, 0, len(cfg.Collections))
		for name := range cfg.Collections {
			names = append(names, name)
		}
		state.Prune(rs, keep, state.PruneCollections(names...))
		if err := rs.Save(pp.RenderStateFile); err != nil {
			return fmt.Errorf("save render state: %w", err)
		}
	}

	return writeCleanResult(out, "orphans", result)
}

func runCleanStaleLocalCopies(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("clean-stale-local-copies")
	defer gcloser.Close()
	glogf("clean stale-local-copies started (dry_run=%v)", cleanDryRun)

	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}
	glogf("project resolved: %s", pp.Root)

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)

	// Deliberately skip paths.ApplyLibrary: "stale local copies" means files
	// left behind in this project's own cache/ dir from before local
	// sources stopped being copied there. The shared library's index and
	// sources dir are a different concern and must not be substituted here.
	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	var staleIdentifiers []string
	for identifier, entry := range idx.Entries {
		if entry.SourceType != cache.SourceTypeLocal {
			continue
		}
		if entry.CachedPath == "" {
			continue
		}
		if !isPathInsideDir(entry.CachedPath, pp.CacheDir) {
			continue
		}
		staleIdentifiers = append(staleIdentifiers, identifier)
	}
	sort.Strings(staleIdentifiers)

	out := cmd.OutOrStdout()
	result := cleanResult{DryRun: cleanDryRun}

	for _, identifier := range staleIdentifiers {
		entry := idx.Entries[identifier]
		removed := removeFileEntry(entry.CachedPath, out, &result)
		if !cleanDryRun && removed {
			entry.CachedPath = ""
			idx.SetEntry(entry)
		}
	}

	glogf("found %d stale local copies", len(staleIdentifiers))

	if !cleanDryRun && len(staleIdentifiers) > 0 {
		if err := cache.Save(pp, idx); err != nil {
			return fmt.Errorf("save index: %w", err)
		}
	}

	return writeCleanResult(out, "stale-local-copies", result)
}

// isPathInsideDir reports whether path lies inside dir (after cleaning both
// to absolute form). A path equal to dir itself is not considered inside it.
func isPathInsideDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absPath = filepath.Clean(absPath)
	absDir = filepath.Clean(absDir)
	if absPath == absDir {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false
	}
	return true
}

func runCleanAll(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("clean-all")
	defer gcloser.Close()
	glogf("clean all started (dry_run=%v)", cleanDryRun)

	pp, err := resolveCleanPaths()
	if err != nil {
		return err
	}
	glogf("project resolved: %s", pp.Root)

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

	glogf("clean all finished: %d removed, %d skipped", result.Removed, result.Skipped)
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

// buildExpectedPaths returns the set of segment output paths the project's
// current plans and config call for.
func buildExpectedPaths(pp paths.ProjectPaths, cfg config.Config) (map[string]bool, error) {
	segments, err := buildExpectedSegments(pp, cfg)
	if err != nil {
		return nil, err
	}
	expected := make(map[string]bool, len(segments))
	for _, seg := range segments {
		expected[seg.OutputPath] = true
	}
	return expected, nil
}

// buildExpectedSegments resolves every collection clip the project currently
// calls for into the segment it would render to.
func buildExpectedSegments(pp paths.ProjectPaths, cfg config.Config) ([]render.Segment, error) {
	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return nil, err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		return nil, err
	}

	clips, err := resolver.BuildCollectionClips(collections)
	if err == nil {
		clips, err = playback.AnnotateClipsFromProject(pp.Root, cfg, collections, clips)
	}
	if err != nil {
		return nil, err
	}

	segments := make([]render.Segment, 0, len(clips))
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

		seg := render.Segment{
			Clip:     clip,
			Overlays: collClip.Overlays,
		}

		outputDir := collClip.OutputDir
		if !filepath.IsAbs(outputDir) {
			outputDir = filepath.Join(pp.SegmentsDir, outputDir)
		}
		baseName := render.SegmentBaseName(tmpl, seg)
		seg.OutputPath = filepath.Join(outputDir, baseName+".mp4")
		segments = append(segments, seg)
	}

	return segments, nil
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
		switch pattern {
		case "**/*.mp4":
			if matched, _ := filepath.Match("*.mp4", filepath.Base(path)); matched {
				matches = append(matches, path)
			}
		case "*":
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

// removeFileEntry removes the file at path, reporting outcome via result and
// out. It returns true when path is now confirmed gone from disk (removal
// succeeded, or nothing was there to begin with) — safe for a caller to drop
// any index reference to it — and false when os.Remove itself failed and the
// file is still present, meaning any index reference to it must be kept.
func removeFileEntry(path string, out io.Writer, result *cleanResult) bool {
	info, err := os.Stat(path)
	if err != nil {
		result.Skipped++
		return true
	}
	size := info.Size()

	if cleanDryRun {
		fmt.Fprintf(out, "would remove %s (%s)\n", path, formatSize(size))
		result.Removed++
		result.FreedBytes += size
		return true
	}

	if err := os.Remove(path); err != nil {
		if !outputJSON {
			fmt.Fprintf(out, "error removing %s: %v\n", path, err)
		}
		result.Skipped++
		return false
	}

	result.Removed++
	result.FreedBytes += size
	if !outputJSON {
		fmt.Fprintf(out, "removed %s (%s)\n", path, formatSize(size))
	}
	return true
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
