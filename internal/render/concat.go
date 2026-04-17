package render

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	placements, err := project.BuildTimelinePlacements(cfg.Timeline, collections)
	if err != nil {
		return nil, err
	}

	collPaths := make(map[string]map[int]TimelineSegmentPath, len(collections))
	for name, coll := range collections {
		ordered, err := buildCollectionPaths(pp, cfg, name, coll)
		if err != nil {
			return nil, err
		}
		byIndex := make(map[int]TimelineSegmentPath, len(ordered))
		for _, path := range ordered {
			byIndex[path.Index] = path
		}
		collPaths[name] = byIndex
	}

	var result []TimelineSegmentPath
	for _, placement := range placements {
		if placement.SourceFile != "" {
			resolvedFile := resolveInlineFilePath(pp.Root, placement.SourceFile)
			result = append(result, TimelineSegmentPath{
				CollectionName: "__inline__",
				Path:           InlineSegmentPath(pp.SegmentsDir, placement.SequenceEntryIndex, resolvedFile),
			})
			continue
		}
		pathsByIndex, ok := collPaths[placement.Collection]
		if !ok {
			return nil, fmt.Errorf("timeline references unknown collection %q", placement.Collection)
		}
		path, ok := pathsByIndex[placement.RowIndex]
		if !ok {
			return nil, fmt.Errorf("timeline references missing row %d in collection %q", placement.RowIndex, placement.Collection)
		}
		result = append(result, path)
	}

	return result, nil
}

// TimelineClip represents a clip in timeline order with full metadata.
type TimelineClip struct {
	CollectionName string
	CollectionClip project.CollectionClip
}

// ResolveTimelineClips returns collection clips in timeline order by walking
// the timeline config with interleave logic. Unlike ResolveTimelineSegments,
// this preserves full clip data (duration, overlays, etc.).
func ResolveTimelineClips(cfg config.Config, collClips []project.CollectionClip) ([]TimelineClip, error) {
	if len(cfg.Timeline.Sequence) == 0 {
		return nil, fmt.Errorf("no timeline sequence configured")
	}

	byCollection := make(map[string]map[int]project.CollectionClip)
	for _, cc := range collClips {
		if byCollection[cc.CollectionName] == nil {
			byCollection[cc.CollectionName] = make(map[int]project.CollectionClip)
		}
		byCollection[cc.CollectionName][cc.Clip.Row.Index] = cc
	}

	collections := make(map[string]project.Collection, len(byCollection))
	for name, clips := range byCollection {
		rows := make([]csvplan.CollectionRow, 0, len(clips))
		for rowIndex := range clips {
			rows = append(rows, csvplan.CollectionRow{Index: rowIndex})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Index < rows[j].Index
		})
		collections[name] = project.Collection{Name: name, Rows: rows}
	}

	placements, err := project.BuildTimelinePlacements(cfg.Timeline, collections)
	if err != nil {
		return nil, err
	}

	var result []TimelineClip
	for _, placement := range placements {
		if placement.SourceFile != "" {
			continue
		}
		clipsByIndex, ok := byCollection[placement.Collection]
		if !ok {
			return nil, fmt.Errorf("timeline references unknown collection %q", placement.Collection)
		}
		cc, ok := clipsByIndex[placement.RowIndex]
		if !ok {
			return nil, fmt.Errorf("timeline references missing row %d in collection %q", placement.RowIndex, placement.Collection)
		}
		result = append(result, TimelineClip{CollectionName: cc.CollectionName, CollectionClip: cc})
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
	Method     string // "single_copy", "stream_copy", or "re-encode"
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

	segments, err := readConcatList(concatFile)
	if err != nil {
		return ConcatResult{}, err
	}
	if len(segments) == 1 {
		if err := copyFile(outputPath, segments[0]); err != nil {
			return ConcatResult{}, fmt.Errorf("copy single segment: %w", err)
		}
		return ConcatResult{OutputPath: outputPath, Method: "single_copy"}, nil
	}

	// Try stream copy first (always works when all segments share the same codec).
	// -fflags +genpts regenerates presentation timestamps so discontinuous
	// per-segment timestamps don't accumulate into a broken output duration.
	streamArgs := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-fflags", "+genpts",
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

// resolveInlineFilePath resolves a file path from a sequence entry relative to
// the project root when the path is not absolute.
func resolveInlineFilePath(root, file string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(root, file)
}

// InlineSegmentPath returns the normalized output path for an inline file entry
// at the given sequence index. Both ResolveTimelineSegments and renderInlineFiles
// must use this to ensure the paths match.
func InlineSegmentPath(segmentsDir string, seqIdx int, sourceFile string) string {
	basename := strings.TrimSuffix(filepath.Base(sourceFile), filepath.Ext(sourceFile))
	return filepath.Join(segmentsDir, "__inline__", fmt.Sprintf("%03d-%s.mp4", seqIdx, sanitizeSegment(basename)))
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

func readConcatList(concatFile string) ([]string, error) {
	data, err := os.ReadFile(concatFile)
	if err != nil {
		return nil, fmt.Errorf("read concat list: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	segments := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "file '") || !strings.HasSuffix(line, "'") {
			return nil, fmt.Errorf("parse concat list: unsupported line %q", line)
		}
		path := strings.TrimSuffix(strings.TrimPrefix(line, "file '"), "'")
		path = strings.ReplaceAll(path, "'\\''", "'")
		segments = append(segments, path)
	}

	return segments, nil
}

func copyFile(dst, src string) error {
	if sameFilePath(dst, src) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func sameFilePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return absA == absB
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
