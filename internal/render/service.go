package render

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/tools"
)

// Service coordinates ffmpeg segment rendering for plan rows.
type Service struct {
	Paths  paths.ProjectPaths
	Config config.Config
	Runner cache.Runner
	stdout io.Writer
	stderr io.Writer

	ffmpegPath string
}

// Options controls render execution behaviour.
type Options struct {
	Concurrency int
	Force       bool
	Reporter    ProgressReporter
}

// Segment encapsulates the information required to render a clip.
type Segment struct {
	Clip       project.Clip
	Profile    project.ResolvedProfile
	Segments   []config.OverlaySegment
	SourcePath string
	CachedPath string
	Entry      cache.Entry
	OutputPath string // Optional: if set, overrides default path calculation
}

// Result captures the outcome of a render attempt.
type Result struct {
	Index      int
	ClipType   project.ClipType
	TypeIndex  int
	Title      string
	OutputPath string
	LogPath    string
	Skipped    bool
	Reason     string // Why the segment was rendered or skipped (from state.Reason* constants)
	Err        error
}

// ProgressReporter receives notifications as segments move through the render pipeline.
type ProgressReporter interface {
	Start(segment Segment)
	Complete(result Result)
}

// NewService prepares a renderer bound to a project.
func NewService(ctx context.Context, pp paths.ProjectPaths, cfg config.Config, runner cache.Runner) (*Service, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runner == nil {
		runner = cache.CmdRunner{}
	}
	if err := pp.EnsureMetaDirs(); err != nil {
		return nil, err
	}

	ctx = tools.WithMinimums(ctx, cfg.ToolMinimums())

	ffStatus, err := tools.Ensure(ctx, "ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ensure ffmpeg: %w", err)
	}
	ffmpegPath := firstNonEmpty(ffStatus.Path, ffStatus.Paths["ffmpeg"])
	if ffmpegPath == "" {
		return nil, errors.New("ffmpeg path not resolved")
	}

	return &Service{
		Paths:      pp,
		Config:     cfg,
		Runner:     runner,
		ffmpegPath: ffmpegPath,
	}, nil
}

// SetWriters configures optional stdout/stderr writers for progress messages.
func (s *Service) SetWriters(stdout, stderr io.Writer) {
	if s == nil {
		return
	}
	s.stdout = stdout
	s.stderr = stderr
}

// Render executes ffmpeg for the provided segments.
func (s *Service) Render(ctx context.Context, segments []Segment, opts Options) []Result {
	if s == nil {
		return []Result{{
			Err: errors.New("render service is nil"),
		}}
	}

	results := make([]Result, len(segments))

	if ctx == nil {
		ctx = context.Background()
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, concurrency)
	)

	for i, seg := range segments {
		i, seg := i, seg
		if opts.Reporter != nil {
			opts.Reporter.Start(seg)
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res := s.renderOne(ctx, seg, opts.Force)
			results[i] = res
			if opts.Reporter != nil {
				opts.Reporter.Complete(res)
			}
		}()
	}

	wg.Wait()
	return results
}

func (s *Service) renderOne(ctx context.Context, seg Segment, force bool) Result {
	clip := seg.Clip
	row := clip.Row
	result := Result{
		Index:     clip.Sequence,
		ClipType:  clip.ClipType,
		TypeIndex: clip.TypeIndex,
		Title:     clipTitle(clip),
	}

	source := strings.TrimSpace(seg.SourcePath)
	if source == "" {
		source = strings.TrimSpace(seg.CachedPath)
	}
	if source == "" {
		result.Err = fmt.Errorf("clip %s#%03d missing source path", clip.ClipType, clip.TypeIndex)
		return result
	}

	// Validate start time and duration against source video duration
	if err := s.validateSegmentTiming(ctx, seg, source); err != nil {
		result.Err = err
		return result
	}

	outputPath, logPath := s.segmentPaths(seg)
	result.OutputPath = outputPath

	if !force {
		if exists, err := paths.FileExists(outputPath); err != nil {
			result.Err = fmt.Errorf("stat segment output: %w", err)
			return result
		} else if exists {
			result.Skipped = true
			s.printf("segment %03d already exists, skipping: %s\n", row.Index, outputPath)
			return result
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		result.Err = fmt.Errorf("ensure segment directory: %w", err)
		return result
	}

	filterGraph, err := BuildFilterGraph(seg, s.Config)
	if err != nil {
		result.Err = fmt.Errorf("build filter graph: %w", err)
		return result
	}

	audioFilters := BuildAudioFilters(s.Config)

	args, err := BuildFFmpegCmd(seg, outputPath, filterGraph, audioFilters, s.Config)
	if err != nil {
		result.Err = err
		return result
	}

	result.LogPath = logPath
	logFile, err := os.Create(logPath)
	if err != nil {
		result.Err = fmt.Errorf("open log file: %w", err)
		return result
	}
	defer logFile.Close()

	s.printf("rendering %s -> %s\n", segmentLabel(seg), filepath.Base(outputPath))

	runOpts := cache.RunOptions{
		Dir:    s.Paths.Root,
		Stderr: logFile,
	}
	if s.stderr != nil {
		runOpts.Stderr = io.MultiWriter(logFile, s.stderr)
	}

	if _, err := s.Runner.Run(ctx, s.ffmpegPath, args, runOpts); err != nil {
		result.Err = fmt.Errorf("ffmpeg failed: %w (see %s)", err, logPath)
		_ = os.Remove(outputPath)
		return result
	}

	return result
}

func (s *Service) segmentPaths(seg Segment) (string, string) {
	// Use explicit OutputPath if provided (e.g., for collections with subdirectories)
	if seg.OutputPath != "" {
		base := strings.TrimSuffix(filepath.Base(seg.OutputPath), filepath.Ext(seg.OutputPath))
		log := filepath.Join(s.Paths.LogsDir, base+".log")
		return seg.OutputPath, log
	}

	// Otherwise compute path from template
	template := s.Config.SegmentFilenameTemplate()
	if template == "" {
		template = config.Default().SegmentFilenameTemplate()
	}
	base := SegmentBaseName(template, seg)
	if base == "" {
		base = fallbackSegmentBase(seg.Clip)
	}
	output := filepath.Join(s.Paths.SegmentsDir, base+".mp4")
	log := filepath.Join(s.Paths.LogsDir, base+".log")
	return output, log
}

func (s *Service) printf(format string, args ...any) {
	if s == nil || s.stdout == nil {
		return
	}
	fmt.Fprintf(s.stdout, format, args...)
}

func clipTitle(clip project.Clip) string {
	if title := strings.TrimSpace(clip.Row.Title); title != "" {
		return title
	}
	if name := strings.TrimSpace(clip.Row.Name); name != "" {
		return name
	}
	if clip.SourceKind == project.SourceKindMedia && strings.TrimSpace(clip.MediaPath) != "" {
		return filepath.Base(clip.MediaPath)
	}
	return string(clip.ClipType)
}

func segmentLabel(seg Segment) string {
	clip := seg.Clip
	label := fmt.Sprintf("%s#%03d", clip.ClipType, clip.TypeIndex)
	if clip.TypeIndex <= 0 {
		label = fmt.Sprintf("%s@%03d", clip.ClipType, clip.Sequence)
	}
	if title := strings.TrimSpace(clipTitle(clip)); title != "" {
		return fmt.Sprintf("%s %s", label, title)
	}
	return label
}

func outputPathName(path string) string {
	base := filepath.Base(path)
	if base != "" {
		return base
	}
	return path
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// validateSegmentTiming checks if the requested start time and duration are valid
// for the source video file.
func (s *Service) validateSegmentTiming(ctx context.Context, seg Segment, sourcePath string) error {
	clip := seg.Clip
	row := clip.Row

	// Get video duration from cache entry probe data if available
	var videoDuration float64
	if seg.Entry.Probe != nil && seg.Entry.Probe.DurationSeconds > 0 {
		videoDuration = seg.Entry.Probe.DurationSeconds
	} else {
		// Probe the video file directly
		duration, err := s.probeVideoDuration(ctx, sourcePath)
		if err != nil {
			// If we can't probe, log a warning but don't fail - ffmpeg will handle it
			s.printf("warning: could not probe video duration for %s: %v\n", sourcePath, err)
			return nil
		}
		videoDuration = duration
	}

	if videoDuration <= 0 {
		// No duration available, can't validate
		return nil
	}

	// Convert start time to seconds
	startSeconds := row.Start.Seconds()

	// Check if start time is beyond video duration
	if startSeconds >= videoDuration {
		return fmt.Errorf("start time %s (%.2fs) is beyond video duration (%.2fs)",
			formatDuration(row.Start), startSeconds, videoDuration)
	}

	// Check if start + duration exceeds video duration
	requestedDuration := float64(row.DurationSeconds)
	endTime := startSeconds + requestedDuration
	if endTime > videoDuration {
		return fmt.Errorf("start time %s (%.2fs) + duration %ds (%.2fs total) exceeds video duration (%.2fs)",
			formatDuration(row.Start), startSeconds, row.DurationSeconds, endTime, videoDuration)
	}

	return nil
}

// probeVideoDuration uses ffprobe to get the duration of a video file in seconds.
func (s *Service) probeVideoDuration(ctx context.Context, videoPath string) (float64, error) {
	// Get ffprobe from tools (comes with ffmpeg)
	ffprobeStatus, err := tools.Ensure(ctx, "ffmpeg")
	if err != nil {
		return 0, fmt.Errorf("ensure ffprobe: %w", err)
	}

	// ffprobe should be in the Paths map
	ffprobePath := ffprobeStatus.Paths["ffprobe"]
	if ffprobePath == "" {
		// Fallback to using "ffprobe" and let the system find it
		ffprobePath = "ffprobe"
	}

	args := []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	}

	result, err := s.Runner.Run(ctx, ffprobePath, args, cache.RunOptions{})
	if err != nil {
		stderr := strings.TrimSpace(string(result.Stderr))
		if stderr != "" {
			return 0, fmt.Errorf("ffprobe failed: %w (stderr: %s)", err, stderr)
		}
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	var duration float64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(result.Stdout)), "%f", &duration); err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}

	return duration, nil
}

// formatDuration formats a time.Duration as MM:SS or HH:MM:SS
func formatDuration(d time.Duration) string {
	totalSeconds := int(d.Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

// RenderSample extracts a single frame at the specified time as a PNG image.
// This is useful for testing overlay configurations without rendering full videos.
func (s *Service) RenderSample(ctx context.Context, seg Segment, sampleTime float64, outputPath string) error {
	if s == nil {
		return errors.New("render service is nil")
	}

	source := strings.TrimSpace(seg.SourcePath)
	if source == "" {
		source = strings.TrimSpace(seg.CachedPath)
	}
	if source == "" {
		return fmt.Errorf("segment missing source path")
	}

	// Build the filter graph with all overlays
	filterGraph, err := BuildFilterGraph(seg, s.Config)
	if err != nil {
		return fmt.Errorf("build filter graph: %w", err)
	}

	// Calculate the absolute time in the source video
	absoluteTime := sampleTime
	if seg.Clip.SourceKind == project.SourceKindPlan {
		absoluteTime += seg.Clip.Row.Start.Seconds()
	}

	// Build ffmpeg command to extract a single frame
	args := []string{
		"-hide_banner",
		"-y",
		"-ss", fmt.Sprintf("%.3f", absoluteTime),
		"-i", source,
		"-vf", filterGraph,
		"-frames:v", "1",
		"-q:v", "2", // High quality JPEG encoding (for PNG this sets compression)
		outputPath,
	}

	// Create a log file for debugging
	logPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".log"
	logFile, err := os.Create(logPath)
	if err != nil {
		s.printf("warning: could not create log file: %v\n", err)
		logFile = nil
	}
	if logFile != nil {
		defer logFile.Close()
	}

	s.printf("Extracting frame at %.2fs (absolute: %.2fs) from %s\n", sampleTime, absoluteTime, filepath.Base(source))
	s.printf("Filter graph: %s\n", filterGraph)

	runOpts := cache.RunOptions{
		Dir: s.Paths.Root,
	}
	if logFile != nil {
		runOpts.Stderr = logFile
		if s.stderr != nil {
			runOpts.Stderr = io.MultiWriter(logFile, s.stderr)
		}
	} else if s.stderr != nil {
		runOpts.Stderr = s.stderr
	}

	if _, err := s.Runner.Run(ctx, s.ffmpegPath, args, runOpts); err != nil {
		if logFile != nil {
			return fmt.Errorf("ffmpeg failed: %w (see %s)", err, logPath)
		}
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	return nil
}
