package dashboard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/playback"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/pkg/csvplan"
)

// quitVLC gracefully quits any running VLC instance and waits for it to exit.
func quitVLC() {
	if !vlcRunning() {
		return
	}

	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e", `tell application "VLC" to quit`).Run()
	case "windows":
		_ = exec.Command("taskkill", "/IM", "vlc.exe", "/T").Run()
	default:
		_ = exec.Command("pkill", "-TERM", "-x", "vlc").Run()
	}

	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if !vlcRunning() {
			return
		}
	}

	// Graceful quit didn't finish in time — force-kill so the next launch
	// never races an already-running instance (which macOS/Windows can
	// silently hand the new file to instead of starting fresh playback).
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("pkill", "-9", "-x", "VLC").Run()
	case "windows":
		_ = exec.Command("taskkill", "/F", "/IM", "vlc.exe", "/T").Run()
	default:
		_ = exec.Command("pkill", "-9", "-x", "vlc").Run()
	}

	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if !vlcRunning() {
			return
		}
	}
}

// playFileInVLC opens a single file in VLC, replacing any existing playlist.
func playFileInVLC(vlcPath, filePath string) error {
	quitVLC()
	c := exec.Command(vlcPath, vlcLaunchArgs(filePath)...)
	return c.Start()
}

// playClipInVLC opens a source file in VLC at a specific offset and optionally
// limits playback to a stop time.
func playClipInVLC(vlcPath, filePath string, startSeconds float64, stopSeconds float64) error {
	quitVLC()

	args := vlcLaunchArgs(filePath)
	if startSeconds > 0 {
		args = append(args, "--start-time", strconv.FormatFloat(startSeconds, 'f', -1, 64))
	}
	if stopSeconds > startSeconds {
		args = append(args, "--stop-time", strconv.FormatFloat(stopSeconds, 'f', -1, 64))
	}

	c := exec.Command(vlcPath, args...)
	return c.Start()
}

// playPlaylistInVLC writes an m3u playlist and opens it in VLC with a fresh playlist.
// Returns (played, total, error).
func playPlaylistInVLC(vlcPath string, files []string, tmpDir string) (int, int, error) {
	total := len(files)
	var existing []string
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			existing = append(existing, f)
		}
	}

	if len(existing) == 0 {
		return 0, total, fmt.Errorf("no rendered files found")
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, f := range existing {
		b.WriteString(f)
		b.WriteByte('\n')
	}

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return 0, 0, fmt.Errorf("create temp dir: %w", err)
	}
	playlistPath := filepath.Join(tmpDir, "powerhour-preview.m3u")
	if err := os.WriteFile(playlistPath, []byte(b.String()), 0o644); err != nil {
		return 0, 0, fmt.Errorf("write playlist: %w", err)
	}

	quitVLC()
	c := exec.Command(vlcPath, vlcLaunchArgs(playlistPath)...)
	if err := c.Start(); err != nil {
		return 0, 0, err
	}

	return len(existing), total, nil
}

func vlcLaunchArgs(target string) []string {
	args := []string{}
	switch runtime.GOOS {
	case "darwin":
		args = append(args, "--macosx-continue-playback", "2", "--no-macosx-recentitems")
	}
	args = append(args, target)
	return args
}

func vlcRunning() bool {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("pgrep", "-x", "VLC").Run() == nil
	case "windows":
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq vlc.exe").Output()
		if err != nil {
			return false
		}
		return strings.Contains(strings.ToLower(string(out)), "vlc.exe")
	default:
		return exec.Command("pgrep", "-x", "vlc").Run() == nil
	}
}

// resolveRenderedSegment builds the render.Segment a collection row would
// produce, including its OutputPath — the same construction the render
// service and `status` command use, so hashes and paths stay consistent.
func resolveRenderedSegment(pp paths.ProjectPaths, cfg config.Config, pos playback.PositionIndex, collName string, coll project.Collection, row csvplan.CollectionRow) render.Segment {
	collCfg := cfg.Collections[collName]
	fadeIn, fadeOut := config.ResolveFade(collCfg.Fade, collCfg.FadeIn, collCfg.FadeOut)

	clip := project.Clip{
		Sequence: row.Index,
		// The filename template may embed the playback position, so the
		// path this resolves to is wrong without it — the row's segment
		// would look missing the moment the order moved it.
		PlaybackPosition: pos.Of(collName, row.RowID),
		ClipType:         project.ClipType(collName),
		TypeIndex:        row.Index,
		Row:              row.ToRow(),
		SourceKind:       project.SourceKindPlan,
		DurationSeconds:  row.DurationSeconds,
		FadeInSeconds:    fadeIn,
		FadeOutSeconds:   fadeOut,
	}
	clip.Row.DurationSeconds = clip.DurationSeconds
	if clip.Row.Index <= 0 {
		clip.Row.Index = clip.TypeIndex
	}

	seg := render.Segment{
		Clip:     clip,
		Overlays: collCfg.Overlays,
	}

	tmpl := cfg.SegmentFilenameTemplate()
	outputDir := coll.OutputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(pp.SegmentsDir, outputDir)
	}
	seg.OutputPath = filepath.Join(outputDir, render.SegmentBaseName(tmpl, seg)+".mp4")
	return seg
}

// resolveRenderedSegmentPath returns the best rendered segment on disk for a
// collection row, and whether its burned-in number matches the row's current
// playback position. A segment rendered at an older position still counts:
// it is the right clip at the right length, only mis-numbered.
func resolveRenderedSegmentPath(scan *segmentScanner, pp paths.ProjectPaths, cfg config.Config, pos playback.PositionIndex, collName string, coll project.Collection, row csvplan.CollectionRow) (string, bool) {
	seg := resolveRenderedSegment(pp, cfg, pos, collName, coll, row)
	return scan.find(cfg.SegmentFilenameTemplate(), seg)
}

// resolveSourcePath resolves a collection row's original (unrendered, uncut)
// source file: the cached download for a URL, or the local file it points at.
// Returns "" when nothing resolvable exists on disk.
func resolveSourcePath(idx *cache.Index, root string, row csvplan.CollectionRow) string {
	link := strings.TrimSpace(row.Link)
	if link == "" {
		return ""
	}

	if isURL(link) {
		if idx == nil {
			return ""
		}
		key, ok := idx.LookupLink(link)
		if !ok {
			return ""
		}
		entry, ok := idx.GetByIdentifier(key)
		if !ok || entry.CachedPath == "" {
			return ""
		}
		return entry.CachedPath
	}

	path := strings.Trim(link, "'\"")
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// existingPath returns the first candidate that exists on disk, or "" if none do.
func existingPath(candidates ...string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// resolvePlacementPathWithFallback resolves a single timeline placement to the
// best playable path: the rendered segment when it exists on disk, otherwise
// the raw (unrendered, uncut) source file when that exists, otherwise "".
func resolvePlacementPathWithFallback(scan *segmentScanner, pp paths.ProjectPaths, cfg config.Config, pos playback.PositionIndex, collections map[string]project.Collection, idx *cache.Index, placement project.TimelinePlacement) string {
	if placement.SourceFile != "" {
		raw := render.ResolveInlineFilePath(pp.Root, placement.SourceFile)
		rendered := render.InlineSegmentPath(pp.SegmentsDir, placement.SequenceEntryIndex, raw)
		return existingPath(rendered, raw)
	}

	coll, ok := collections[placement.Collection]
	if !ok {
		return ""
	}
	row, ok := findCollectionRow(coll, placement.RowIndex)
	if !ok {
		return ""
	}
	rendered, _ := resolveRenderedSegmentPath(scan, pp, cfg, pos, placement.Collection, coll, row)
	raw := resolveSourcePath(idx, pp.Root, row)
	return existingPath(rendered, raw)
}

// resolveAllTimelineSegmentPathsWithFallback returns the best playable path for
// every entry in timeline order: the rendered segment when it exists on disk,
// otherwise the raw (unrendered, uncut) source file when that exists, otherwise
// "" (dropped by playPlaylistInVLC's own existence filter).
func resolveAllTimelineSegmentPathsWithFallback(pp paths.ProjectPaths, cfg config.Config, collections map[string]project.Collection, idx *cache.Index) []string {
	order, _, err := playback.ResolveOrder(pp.Root, cfg, collections)
	if err != nil {
		return nil
	}
	placements, err := playback.Placements(order, cfg, collections)
	if err != nil {
		return nil
	}
	pos := playback.NewPositionIndex(order)

	scan := newSegmentScanner()
	result := make([]string, 0, len(placements))
	for _, placement := range placements {
		result = append(result, resolvePlacementPathWithFallback(scan, pp, cfg, pos, collections, idx, placement))
	}
	return result
}

// resolveSequenceEntrySegmentPathsWithFallback returns the best playable path
// for every clip belonging to a single sequence entry at seqIdx (0-based) —
// same rendered/raw-source fallback semantics as
// resolveAllTimelineSegmentPathsWithFallback, scoped to one entry.
func resolveSequenceEntrySegmentPathsWithFallback(pp paths.ProjectPaths, cfg config.Config, pos playback.PositionIndex, collections map[string]project.Collection, idx *cache.Index, seqIdx int) []string {
	placements, err := project.BuildTimelinePlacements(cfg.Timeline, collections)
	if err != nil {
		return nil
	}

	scan := newSegmentScanner()
	var result []string
	for _, placement := range placements {
		if placement.SequenceEntryIndex != seqIdx {
			continue
		}
		result = append(result, resolvePlacementPathWithFallback(scan, pp, cfg, pos, collections, idx, placement))
	}
	return result
}

// resolveCollectionAllPathsWithFallback returns the best playable path for every
// row of a single collection, in plan order: the rendered segment when it
// exists, otherwise the raw (unrendered, uncut) source file.
func resolveCollectionAllPathsWithFallback(pp paths.ProjectPaths, cfg config.Config, pos playback.PositionIndex, collName string, coll project.Collection, idx *cache.Index) []string {
	scan := newSegmentScanner()
	result := make([]string, 0, len(coll.Rows))
	for _, row := range coll.Rows {
		rendered, _ := resolveRenderedSegmentPath(scan, pp, cfg, pos, collName, coll, row)
		raw := resolveSourcePath(idx, pp.Root, row)
		result = append(result, existingPath(rendered, raw))
	}
	return result
}

func findCollectionRow(coll project.Collection, rowIndex int) (csvplan.CollectionRow, bool) {
	for _, row := range coll.Rows {
		if row.Index == rowIndex {
			return row, true
		}
	}
	return csvplan.CollectionRow{}, false
}

// segmentScanner finds a row's rendered segment even when the playback order
// has moved the row since it was rendered.
//
// The segment filename template may embed the playback position
// ($INDEX_PAD3_$SAFE_TITLE by default), and so does the number burned into the
// video. Reordering therefore invalidates a segment — but only its number. The
// clip itself is still the right source, trimmed to the right length, so it is
// far better to preview than the uncut source file. That is the middle state:
// rendered, wrong number.
//
// Directory listings are memoized per scanner because a whole-timeline lookup
// asks about the same handful of output directories once per slot.
type segmentScanner struct {
	dirs map[string][]string
}

func newSegmentScanner() *segmentScanner {
	return &segmentScanner{dirs: make(map[string][]string)}
}

func (s *segmentScanner) list(dir string) []string {
	if names, ok := s.dirs[dir]; ok {
		return names
	}
	var names []string
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
	}
	s.dirs[dir] = names
	return names
}

// find returns the best rendered segment for seg and whether its burned-in
// number matches the row's current playback position. An empty path means
// nothing has been rendered for this row at any position.
func (s *segmentScanner) find(tmpl string, seg render.Segment) (path string, numbered bool) {
	if seg.OutputPath == "" {
		return "", false
	}
	if _, err := os.Stat(seg.OutputPath); err == nil {
		return seg.OutputPath, true
	}

	prefix, suffix, varies := render.SegmentNamePattern(tmpl, seg)
	if !varies {
		// The name carries no position, so the exact miss above was the
		// only answer there is.
		return "", false
	}
	if prefix == "" && suffix == "" {
		// Everything in the name came from the playback position, so the
		// pattern matches every segment in the directory equally and would
		// hand back an arbitrary other row's file. Refuse rather than guess;
		// SegmentBaseName's withRowIdentity is what stops names from being
		// this under-specified in the first place.
		return "", false
	}
	// A row that has been reordered more than once leaves several segments on
	// disk under different numbers. Take the newest: it was rendered from the
	// most recent plan row, so its trim and overlays are the least stale.
	ext := filepath.Ext(seg.OutputPath)
	dir := filepath.Dir(seg.OutputPath)
	var best string
	var bestMod time.Time
	for _, name := range s.list(dir) {
		if len(name) < len(prefix)+len(suffix)+len(ext) {
			continue
		}
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix+ext) {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = candidate, info.ModTime()
		}
	}
	return best, false
}
