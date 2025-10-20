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

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/tools"
	"powerhour/pkg/csvplan"
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
}

// Segment encapsulates the information required to render a clip.
type Segment struct {
	Row        csvplan.Row
	CachedPath string
	Entry      cache.Entry
}

// Result captures the outcome of a render attempt.
type Result struct {
	Index      int
	Title      string
	OutputPath string
	LogPath    string
	Err        error
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
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = s.renderOne(ctx, seg, opts.Force)
		}()
	}

	wg.Wait()
	return results
}

func (s *Service) renderOne(ctx context.Context, seg Segment, force bool) Result {
	row := seg.Row
	result := Result{
		Index: row.Index,
		Title: row.Title,
	}

	source := strings.TrimSpace(seg.CachedPath)
	if source == "" {
		result.Err = fmt.Errorf("row %03d %q missing cached source path", row.Index, row.Title)
		return result
	}

	outputPath, logPath := s.segmentPaths(seg)
	result.OutputPath = outputPath

	if !force {
		if exists, err := paths.FileExists(outputPath); err != nil {
			result.Err = fmt.Errorf("stat segment output: %w", err)
			return result
		} else if exists {
			s.printf("segment %03d already exists, skipping: %s\n", row.Index, outputPath)
			return result
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		result.Err = fmt.Errorf("ensure segment directory: %w", err)
		return result
	}

	filterGraph, err := BuildFilterGraph(row, s.Config)
	if err != nil {
		result.Err = fmt.Errorf("build filter graph: %w", err)
		return result
	}

	audioFilters := BuildAudioFilters(s.Config)

	args, err := BuildFFmpegCmd(row, source, outputPath, filterGraph, audioFilters, s.Config)
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

	s.printf("rendering %03d %s -> %s\n", row.Index, outputPathName(outputPath), filepath.Base(outputPath))

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
	template := s.Config.SegmentFilenameTemplate()
	if template == "" {
		template = config.Default().SegmentFilenameTemplate()
	}
	base := SegmentBaseName(template, seg)
	if base == "" {
		base = fallbackSegmentBase(seg.Row)
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
