package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

var (
	cleanDryRun     bool
	cleanRenumbered bool
)

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
	cmd := &cobra.Command{
		Use:   "orphans",
		Short: "Remove segment files not in the current plan",
		RunE:  runCleanOrphans,
	}
	cmd.Flags().BoolVar(&cleanRenumbered, "renumbered", false,
		"Also remove mis-numbered renders of a live row that a newer render has superseded; the newest is always kept")
	return cmd
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
	// Renumbered counts files clean orphans recognised as live renders sitting
	// at an old playback position and deliberately left on disk.
	Renumbered      int   `json:"renumbered,omitempty"`
	RenumberedBytes int64 `json:"renumbered_bytes,omitempty"`
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

	expected, err := buildExpectedRender(pp, cfg)
	if err != nil {
		return err
	}

	actual, err := globFiles(pp.SegmentsDir, "**/*.mp4")
	if err != nil {
		return err
	}

	orphans, kept, superseded := classifySegmentFiles(cfg.SegmentFilenameTemplate(), expected, actual)
	// Without --renumbered the superseded duplicates are kept too, and the
	// footer says so, so the flag is only ever advertised when passing it
	// would actually free something.
	offerRenumberedFlag := !cleanRenumbered && len(superseded) > 0
	if !cleanRenumbered {
		kept = append(kept, superseded...)
		superseded = nil
		sort.Strings(kept)
	}

	out := cmd.OutOrStdout()
	result := cleanResult{DryRun: cleanDryRun}

	for _, path := range orphans {
		removeFileEntry(path, out, &result)
	}
	for _, path := range superseded {
		removeFileEntry(path, out, &result)
	}
	reportRenumbered(out, kept, offerRenumberedFlag, &result)

	glogf("found %d orphans, %d renumbered kept, %d superseded removed", len(orphans), len(kept), len(superseded))

	// Prune render state
	if !cleanDryRun {
		rs, err := state.Load(pp.RenderStateFile)
		if err != nil {
			return err
		}
		// Prune keys on row identity, not on output path, so the keep-set has
		// to be built the same way. buildExpectedRender resolves every
		// collection clip AND every inline file: entry the timeline calls for,
		// so this pass is authoritative for the whole project: both the
		// row: and path: key spaces are covered and any key outside the
		// keep-set is genuinely stale.
		keep := make(map[string]bool, len(expected.segments)+len(expected.inlinePaths))
		for _, key := range state.SegmentKeys(expected.segments) {
			keep[key] = true
		}
		for _, path := range expected.inlinePaths {
			keep["path:"+path] = true
		}
		state.Prune(rs, keep, state.PruneAll())
		if err := rs.Save(pp.RenderStateFile); err != nil {
			return fmt.Errorf("save render state: %w", err)
		}
	}

	return writeCleanResult(out, "orphans", result)
}

// reportRenumbered lists the mis-numbered segments this run deliberately left
// alone. They are not orphans: each is a real render of a live row, made while
// that row sat at a different playback position, which the TUI previewer plays
// and which state.DetectChanges renames rather than re-encodes.
func reportRenumbered(out io.Writer, kept []string, offerRenumberedFlag bool, result *cleanResult) {
	if len(kept) == 0 {
		return
	}
	for _, path := range kept {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		result.Renumbered++
		result.RenumberedBytes += info.Size()
		if !outputJSON {
			fmt.Fprintf(out, "kept %s (%s, rendered at an older playback position)\n", path, formatSize(info.Size()))
		}
	}
	if offerRenumberedFlag && !outputJSON {
		fmt.Fprintln(out, "pass --renumbered to also remove the superseded duplicates")
	}
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

// expectedRender is everything the project currently calls for on disk: the
// segment each collection clip renders to, plus the normalized segment each
// inline file: timeline entry renders to.
//
// The two halves are resolved together because the render-state prune needs
// both key spaces — row: for collection clips, path: for inline entries — to
// claim authority over the whole project.
type expectedRender struct {
	segments    []render.Segment
	inlinePaths []string
}

// buildExpectedRender resolves every collection clip and every inline file:
// entry the project currently calls for into the segment it would render to.
func buildExpectedRender(pp paths.ProjectPaths, cfg config.Config) (expectedRender, error) {
	var exp expectedRender

	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return exp, err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		return exp, err
	}

	clips, err := resolver.BuildCollectionClips(collections)
	if err == nil {
		clips, err = playback.AnnotateClipsFromProject(pp.Root, cfg, collections, clips)
	}
	if err != nil {
		return exp, err
	}

	exp.inlinePaths, err = buildExpectedInlinePaths(pp, cfg, collections)
	if err != nil {
		return exp, err
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

	exp.segments = segments
	return exp, nil
}

// buildExpectedInlinePaths returns the normalized segment path for every
// inline file: entry the playback order places. It resolves through
// playback.OrderedPlacements and render.InlineSegmentPath — the same pair
// render.ResolveTimelineSegments uses — because the __inline__ filename keys
// on the sequence entry index, which only the placement walk recovers.
func buildExpectedInlinePaths(pp paths.ProjectPaths, cfg config.Config, collections map[string]project.Collection) ([]string, error) {
	if len(cfg.Timeline.Sequence) == 0 {
		return nil, nil
	}

	placements, err := playback.OrderedPlacements(pp.Root, cfg, collections)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, placement := range placements {
		if placement.SourceFile == "" {
			continue
		}
		raw := render.ResolveInlineFilePath(pp.Root, placement.SourceFile)
		out = append(out, render.InlineSegmentPath(pp.SegmentsDir, placement.SequenceEntryIndex, raw))
	}
	return out, nil
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

// namePattern is the position-invariant part of one live row's segment name:
// what render.SegmentNamePattern found to be fixed no matter where the row
// sits in the playback order.
type namePattern struct {
	prefix string
	suffix string
	ext    string
}

func (p namePattern) matches(name string) bool {
	if len(name) < len(p.prefix)+len(p.suffix)+len(p.ext) {
		return false
	}
	return strings.HasPrefix(name, p.prefix) && strings.HasSuffix(name, p.suffix+p.ext)
}

func (p namePattern) key(dir string) string {
	return dir + "\x00" + p.prefix + "\x00" + p.suffix + p.ext
}

// classifySegmentFiles sorts every .mp4 under the segments tree into three
// buckets against what the project currently calls for.
//
//   - orphans: nothing live claims the file. Safe to delete.
//   - kept: the file is a real render of a live row, made while that row sat
//     at a different playback position. Its number is stale; its clip, trim
//     and overlays are not. The TUI previewer plays it (the yellow ◐ state)
//     and state.DetectChanges renames it instead of re-encoding, so deleting
//     it converts an mv into a full ffmpeg pass.
//   - superseded: an older mis-numbered render of a row that also has a newer
//     one. Only the newest is ever read back, so these are removable — but
//     only on an explicit --renumbered, never as a side effect of "orphans".
//
// A file at a live row's current path is expected and appears in no bucket.
func classifySegmentFiles(tmpl string, expected expectedRender, actual []string) (orphans, kept, superseded []string) {
	exact := make(map[string]bool, len(expected.segments)+len(expected.inlinePaths))
	for _, path := range expected.inlinePaths {
		exact[path] = true
	}

	patterns := make(map[string][]namePattern)
	for _, seg := range expected.segments {
		if seg.OutputPath == "" {
			continue
		}
		exact[seg.OutputPath] = true

		prefix, suffix, varies := render.SegmentNamePattern(tmpl, seg)
		if !varies {
			// The name carries no playback position, so it cannot go stale by
			// reordering and the exact path above is the only answer.
			continue
		}
		if prefix == "" && suffix == "" {
			// Every character of the name came from the position, so this
			// pattern matches every file in the directory equally and would
			// claim other rows' segments. Refuse rather than guess — the same
			// call segmentScanner.find makes.
			continue
		}
		dir := filepath.Dir(seg.OutputPath)
		patterns[dir] = append(patterns[dir], namePattern{
			prefix: prefix,
			suffix: suffix,
			ext:    filepath.Ext(seg.OutputPath),
		})
	}

	matched := make(map[string][]string)
	for _, path := range actual {
		if exact[path] {
			continue
		}
		dir := filepath.Dir(path)
		name := filepath.Base(path)
		// Longest match wins. Two rows' patterns can overlap when one row's
		// fixed part is a substring of another's, and first-match-wins would
		// then pool a file under whichever row happened to be resolved first.
		// The longest match is the most specific claim on the name.
		best := ""
		bestLen := -1
		for _, p := range patterns[dir] {
			if !p.matches(name) {
				continue
			}
			if n := len(p.prefix) + len(p.suffix); n > bestLen {
				best, bestLen = p.key(dir), n
			}
		}
		if bestLen < 0 {
			orphans = append(orphans, path)
			continue
		}
		matched[best] = append(matched[best], path)
	}

	for _, group := range matched {
		newest := newestPath(group)
		for _, path := range group {
			if path == newest {
				kept = append(kept, path)
				continue
			}
			superseded = append(superseded, path)
		}
	}

	sort.Strings(orphans)
	sort.Strings(kept)
	sort.Strings(superseded)
	return orphans, kept, superseded
}

// newestPath returns the most recently modified of group — the render made
// from the most recent plan row, so the least stale of the group.
func newestPath(group []string) string {
	var best string
	var bestMod time.Time
	for _, path := range group {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = path, info.ModTime()
		}
	}
	if best == "" && len(group) > 0 {
		return group[0]
	}
	return best
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
