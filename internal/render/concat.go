package render

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/tools"
	"powerhour/pkg/csvplan"
)

// TimelineSegmentPath holds a resolved output path for a single segment in
// timeline order.
type TimelineSegmentPath struct {
	CollectionName string
	Index          int
	Path           string
}

// ResolveTimelineSegments returns the ordered segment output paths by walking
// the timeline config. If no timeline sequence is configured or collections is
// empty, it falls back to a sorted glob of *.mp4 files under pp.SegmentsDir.
func ResolveTimelineSegments(pp paths.ProjectPaths, cfg config.Config, collections map[string]project.Collection) ([]TimelineSegmentPath, error) {
	if len(cfg.Timeline.Sequence) == 0 || len(collections) == 0 {
		return resolveSegmentsFallback(pp)
	}

	// Pre-build per-collection ordered segment paths.
	collPaths := make(map[string][]TimelineSegmentPath, len(collections))
	for name, coll := range collections {
		ordered, err := buildCollectionPaths(pp, cfg, name, coll)
		if err != nil {
			return nil, err
		}
		collPaths[name] = ordered
	}

	// Walk timeline sequence, consuming clips according to Count and Interleave.
	consumed := make(map[string]int, len(collections))
	var result []TimelineSegmentPath

	for _, entry := range cfg.Timeline.Sequence {
		mainPaths, ok := collPaths[entry.Collection]
		if !ok {
			return nil, fmt.Errorf("timeline references unknown collection %q", entry.Collection)
		}

		mainStart := consumed[entry.Collection]
		mainCount := len(mainPaths) - mainStart
		if entry.Count > 0 && entry.Count < mainCount {
			mainCount = entry.Count
		}
		mainSlice := mainPaths[mainStart : mainStart+mainCount]
		consumed[entry.Collection] += mainCount

		if entry.Interleave == nil {
			result = append(result, mainSlice...)
			continue
		}

		// Interleave: splice one clip from the interleave collection after
		// every `every` main clips.
		il := entry.Interleave
		ilPaths, ok := collPaths[il.Collection]
		if !ok {
			return nil, fmt.Errorf("timeline interleave references unknown collection %q", il.Collection)
		}
		ilStart := consumed[il.Collection]
		every := il.Every
		if every <= 0 {
			every = 1
		}

		ilAvail := len(ilPaths) - ilStart
		if ilAvail <= 0 {
			// All interleave clips already consumed; cycle from the beginning.
			ilStart = 0
			ilAvail = len(ilPaths)
		}
		if ilAvail == 0 {
			// No interleave clips at all — just append main clips.
			result = append(result, mainSlice...)
			continue
		}

		ilIdx := 0
		for mainIdx, seg := range mainSlice {
			result = append(result, seg)
			if (mainIdx+1)%every == 0 {
				absIdx := ilStart + (ilIdx % ilAvail)
				result = append(result, ilPaths[absIdx])
				ilIdx++
			}
		}
		consumed[il.Collection] = ilStart + (ilIdx % ilAvail)
	}

	return result, nil
}

// buildCollectionPaths returns the expected output paths for all rows in a
// collection, sorted by row index.
func buildCollectionPaths(pp paths.ProjectPaths, cfg config.Config, name string, coll project.Collection) ([]TimelineSegmentPath, error) {
	outputDir := coll.OutputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(pp.SegmentsDir, outputDir)
	}

	tmpl := cfg.SegmentFilenameTemplate()

	// Sort rows by index for stable ordering.
	rows := make([]csvplan.CollectionRow, len(coll.Rows))
	copy(rows, coll.Rows)
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Index < rows[j].Index
	})

	segPaths := make([]TimelineSegmentPath, 0, len(rows))
	for seq, collRow := range rows {
		row := collRow.ToRow()
		clip := project.Clip{
			Sequence:  seq + 1,
			ClipType:  project.ClipType(name),
			TypeIndex: row.Index,
			Row:       row,
		}
		seg := Segment{Clip: clip}
		baseName := SegmentBaseName(tmpl, seg)
		outputPath := filepath.Join(outputDir, baseName+".mp4")
		segPaths = append(segPaths, TimelineSegmentPath{
			CollectionName: name,
			Index:          row.Index,
			Path:           outputPath,
		})
	}
	return segPaths, nil
}

// resolveSegmentsFallback returns all *.mp4 files under pp.SegmentsDir sorted
// by path when no timeline configuration is available.
func resolveSegmentsFallback(pp paths.ProjectPaths) ([]TimelineSegmentPath, error) {
	var result []TimelineSegmentPath

	err := filepath.WalkDir(pp.SegmentsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(d.Name())) == ".mp4" {
			result = append(result, TimelineSegmentPath{Path: p})
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("scan segments dir: %w", err)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result, nil
}

// WriteConcatList writes an ffmpeg concat demuxer list to concatFile.
// It verifies each segment path exists before writing.
func WriteConcatList(concatFile string, segments []TimelineSegmentPath) error {
	var missing []string
	for _, seg := range segments {
		if _, err := os.Stat(seg.Path); os.IsNotExist(err) {
			missing = append(missing, seg.Path)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %d segment file(s):\n  %s", len(missing), strings.Join(missing, "\n  "))
	}

	f, err := os.Create(concatFile)
	if err != nil {
		return fmt.Errorf("create concat list: %w", err)
	}
	defer f.Close()

	for _, seg := range segments {
		abs, err := filepath.Abs(seg.Path)
		if err != nil {
			abs = seg.Path
		}
		// Escape single quotes in paths for the concat file format.
		escaped := strings.ReplaceAll(abs, "'", "'\\''")
		fmt.Fprintf(f, "file '%s'\n", escaped)
	}
	return nil
}

// ConcatResult holds the outcome of a concat run.
type ConcatResult struct {
	OutputPath string
	Method     string // "stream_copy" or "re-encode"
}

// RunConcat concatenates segments using the ffmpeg concat demuxer. It tries
// stream copy first; if that fails it automatically re-encodes using enc.
func RunConcat(ctx context.Context, concatFile, outputPath string, enc tools.ResolvedEncoding, stdout, stderr io.Writer) (ConcatResult, error) {
	ffmpegPath, err := tools.Lookup("ffmpeg")
	if err != nil {
		return ConcatResult{}, fmt.Errorf("locate ffmpeg: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return ConcatResult{}, fmt.Errorf("prepare output dir: %w", err)
	}

	// Try stream copy first (always works when all segments share the same codec).
	streamArgs := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", concatFile,
		"-c", "copy",
		outputPath,
	}
	if err := runFFmpeg(ctx, ffmpegPath, streamArgs, stdout, stderr); err == nil {
		return ConcatResult{OutputPath: outputPath, Method: "stream_copy"}, nil
	}

	// Stream copy failed — fall back to re-encode using the resolved encoding.
	reencodeArgs := buildReencodeArgs(concatFile, outputPath, enc)
	if err := runFFmpeg(ctx, ffmpegPath, reencodeArgs, stdout, stderr); err != nil {
		return ConcatResult{}, fmt.Errorf("concat re-encode failed: %w", err)
	}
	return ConcatResult{OutputPath: outputPath, Method: "re-encode"}, nil
}

func buildReencodeArgs(concatFile, outputPath string, enc tools.ResolvedEncoding) []string {
	args := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", concatFile,
		"-c:v", enc.VideoCodec,
		"-b:v", enc.VideoBitrate,
		"-c:a", enc.AudioCodec,
		"-b:a", enc.AudioBitrate,
	}
	if enc.SampleRate > 0 {
		args = append(args, "-ar", fmt.Sprintf("%d", enc.SampleRate))
	}
	if enc.Channels > 0 {
		args = append(args, "-ac", fmt.Sprintf("%d", enc.Channels))
	}
	if enc.Preset != "" && enc.VideoCodec == "libx264" {
		args = append(args, "-preset", enc.Preset)
	}
	args = append(args, outputPath)
	return args
}

func runFFmpeg(ctx context.Context, ffmpegPath string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if stderr != nil {
		cmd.Stderr = stderr
	}
	return cmd.Run()
}
