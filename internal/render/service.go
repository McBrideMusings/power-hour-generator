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
	Overlays   []config.OverlayEntry
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
	Progress(segment Segment, pct float64) // pct in 0.0–1.0
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

	if _, missing := tools.ProbeFilters(ctx, ffmpegPath, tools.RequiredFFmpegFilters); len(missing) > 0 {
		return nil, fmt.Errorf("ffmpeg is missing required filters: %s\nThese filters may require additional libraries (e.g. libfreetype for drawtext).\nRun 'powerhour doctor' for details.", strings.Join(missing, ", "))
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
		if opts.Reporter != nil {
			opts.Reporter.Start(seg)
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res := s.renderOne(ctx, seg, opts.Force, opts.Reporter)
			results[i] = res
			if opts.Reporter != nil {
				opts.Reporter.Complete(res)
			}
		}()
	}

	wg.Wait()
	return results
}

func (s *Service) renderOne(ctx context.Context, seg Segment, force bool, reporter ProgressReporter) Result {
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

	// Resolve zero duration (full video) by probing actual length
	if clip.DurationSeconds <= 0 {
		videoDur, err := s.probeVideoDuration(ctx, source)
		if err != nil {
			result.Err = fmt.Errorf("probe video duration for full-length clip: %w", err)
			return result
		}
		startSec := clip.Row.Start.Seconds()
		resolved := int(videoDur - startSec)
		if resolved <= 0 {
			result.Err = fmt.Errorf("start_time %s exceeds video length %s",
				formatDuration(clip.Row.Start), formatSeconds(videoDur))
			return result
		}
		seg.Clip.DurationSeconds = resolved
		seg.Clip.Row.DurationSeconds = resolved
		clip = seg.Clip
		row = clip.Row
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

	// Add -progress flag for real-time progress reporting.
	args = append(args[:len(args)-1], "-progress", "pipe:1", args[len(args)-1])

	runOpts := cache.RunOptions{
		Dir:    s.Paths.Root,
		Stderr: logFile,
	}
	if s.stderr != nil {
		runOpts.Stderr = io.MultiWriter(logFile, s.stderr)
	}

	// Wire up progress parsing if reporter is available.
	if reporter != nil {
		clipDur := float64(clip.DurationSeconds)
		pw := newProgressWriter(clipDur, func(pct float64) {
			reporter.Progress(seg, pct)
		})
		runOpts.Stdout = pw
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
		return fmt.Errorf("start_time %s exceeds video length %s",
			formatDuration(row.Start), formatSeconds(videoDuration))
	}

	// Check if start + duration exceeds video duration (skip when duration is 0 = full video)
	if row.DurationSeconds > 0 {
		requestedDuration := float64(row.DurationSeconds)
		endTime := startSeconds + requestedDuration
		if endTime > videoDuration {
			return fmt.Errorf("start_time %s + %ds duration exceeds video length %s",
				formatDuration(row.Start), row.DurationSeconds, formatSeconds(videoDuration))
		}
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

// formatDuration formats a time.Duration as M:SS or H:MM:SS.
func formatDuration(d time.Duration) string {
	return formatSeconds(d.Seconds())
}

// formatSeconds formats a float64 seconds value as M:SS or H:MM:SS.
func formatSeconds(s float64) string {
	total := int(s)
	hours := total / 3600
	minutes := (total % 3600) / 60
	secs := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
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

	// Validate sample time against clip duration
	clipDuration := float64(seg.Clip.DurationSeconds)
	if clipDuration > 0 && sampleTime >= clipDuration {
		return fmt.Errorf("sample time %s exceeds clip duration %s",
			formatSeconds(sampleTime), formatSeconds(clipDuration))
	}

	// Build the filter graph with all overlays
	filterGraph, err := BuildFilterGraph(seg, s.Config)
	if err != nil {
		return fmt.Errorf("build filter graph: %w", err)
	}

	// Input-seek to the clip start so the filter graph sees t=0 at clip start,
	// matching the overlay enable/alpha expressions. Then output-seek to the
	// desired sample time so we grab the correct frame with overlays visible.
	args := []string{
		"-hide_banner",
		"-y",
	}
	if seg.Clip.SourceKind == project.SourceKindPlan {
		args = append(args, "-ss", fmt.Sprintf("%.3f", seg.Clip.Row.Start.Seconds()))
	}
	args = append(args,
		"-i", source,
		"-vf", filterGraph,
		"-ss", fmt.Sprintf("%.3f", sampleTime),
		"-frames:v", "1",
		"-q:v", "2",
		outputPath,
	)

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

	s.printf("Extracting frame at %.2fs from %s\n", sampleTime, filepath.Base(source))

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
