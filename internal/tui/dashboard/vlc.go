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

	"powerhour/internal/config"
	"powerhour/internal/paths"
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

// resolveRenderedSegmentPath returns the rendered segment output path for a collection row.
func resolveRenderedSegmentPath(pp paths.ProjectPaths, cfg config.Config, collName string, coll project.Collection, row csvplan.CollectionRow) string {
	collCfg := cfg.Collections[collName]
	fadeIn, fadeOut := config.ResolveFade(collCfg.Fade, collCfg.FadeIn, collCfg.FadeOut)

	clip := project.Clip{
		Sequence:        row.Index,
		ClipType:        project.ClipType(collName),
		TypeIndex:       row.Index,
		Row:             row.ToRow(),
		SourceKind:      project.SourceKindPlan,
		DurationSeconds: row.DurationSeconds,
		FadeInSeconds:   fadeIn,
		FadeOutSeconds:  fadeOut,
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
	return filepath.Join(outputDir, render.SegmentBaseName(tmpl, seg)+".mp4")
}

// resolveAllTimelineSegmentPaths returns all rendered segment paths in timeline order.
func resolveAllTimelineSegmentPaths(pp paths.ProjectPaths, cfg config.Config, collections map[string]project.Collection) []string {
	segments, err := render.ResolveTimelineSegments(pp, cfg, collections)
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(segments))
	for _, seg := range segments {
		result = append(result, seg.Path)
	}
	return result
}

// resolveSequenceEntrySegmentPaths returns the rendered segment paths for a single
// sequence entry at seqIdx (0-based). It resolves all timeline segments then picks
// the slice that belongs to the given entry by counting how many clips each entry
// contributes.
func resolveSequenceEntrySegmentPaths(pp paths.ProjectPaths, cfg config.Config, collections map[string]project.Collection, seqIdx int) []string {
	allSegs, err := render.ResolveTimelineSegments(pp, cfg, collections)
	if err != nil {
		return nil
	}

	// Also resolve the timeline entries to get the same ordering with collection metadata.
	timeline, err := project.ResolveTimeline(cfg.Timeline, collections)
	if err != nil {
		return nil
	}

	// Both allSegs and timeline should have the same length and ordering.
	if len(allSegs) != len(timeline) {
		return nil
	}

	// Map each timeline entry to its originating sequence entry index.
	// Replay the cursor to determine which sequence entry produced each timeline entry.
	seqEntryForTimeline := assignTimelineToSequenceEntries(cfg.Timeline, collections, len(timeline))

	var result []string
	for i, assignment := range seqEntryForTimeline {
		if assignment == seqIdx && i < len(allSegs) {
			result = append(result, allSegs[i].Path)
		}
	}
	return result
}

// assignTimelineToSequenceEntries returns a slice mapping each timeline position
// to the index of its originating sequence entry. Replays the same cursor logic
// as ResolveTimeline.
func assignTimelineToSequenceEntries(timeline config.TimelineConfig, collections map[string]project.Collection, totalEntries int) []int {
	result := make([]int, 0, totalEntries)
	cursor := make(map[string]int)

	for entryIdx, entry := range timeline.Sequence {
		if entry.File != "" {
			result = append(result, entryIdx)
			continue
		}

		coll, ok := collections[entry.Collection]
		if !ok {
			continue
		}

		start := cursor[entry.Collection]
		rows := coll.Rows
		if start >= len(rows) {
			continue
		}
		rows = rows[start:]
		if entry.Count > 0 && entry.Count < len(rows) {
			rows = rows[:entry.Count]
		}
		cursor[entry.Collection] = start + len(rows)

		if entry.Interleave == nil {
			for range rows {
				result = append(result, entryIdx)
			}
			continue
		}

		ilColl, ok := collections[entry.Interleave.Collection]
		if !ok {
			for range rows {
				result = append(result, entryIdx)
			}
			continue
		}

		ilStart := cursor[entry.Interleave.Collection]
		ilAvail := len(ilColl.Rows) - ilStart
		if ilAvail <= 0 {
			ilStart = 0
			ilAvail = len(ilColl.Rows)
		}

		every := entry.Interleave.Every
		placement := project.ResolvePlacement(entry.Interleave.Placement)
		ilIdx := 0

		emitIL := func() {
			if ilAvail <= 0 {
				return
			}
			result = append(result, entryIdx)
			ilIdx++
		}

		for i := range rows {
			isLast := i == len(rows)-1

			if placement == "before" || placement == "around" {
				if i%every == 0 {
					emitIL()
				}
			}

			// Primary clip.
			result = append(result, entryIdx)

			switch placement {
			case "after":
				if (i+1)%every == 0 {
					emitIL()
				}
			case "between":
				if (i+1)%every == 0 && !isLast {
					emitIL()
				}
			case "around":
				if isLast {
					emitIL()
				}
			}
		}

		// Update the interleave cursor so the next sequence entry referencing
		// the same interleave collection resumes from where we left off.
		cursor[entry.Interleave.Collection] = ilStart + ilIdx
	}

	return result
}
