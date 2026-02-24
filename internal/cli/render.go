package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
)

var (
	renderConcurrency  int
	renderForce        bool
	renderDryRun       bool
	renderIndexArg     []string
	renderNoProgress   bool
	renderSampleTime   string
	renderSampleOutput string
)

var errMissingCachedSource = errors.New("missing cached source")

type missingCachedSourceError struct {
	msg string
}

func (e missingCachedSourceError) Error() string {
	return e.msg
}

func (e missingCachedSourceError) Is(target error) bool {
	return target == errMissingCachedSource
}

func newRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render cached clips into individual segment files",
		RunE:  runRender,
	}

	defaultConcurrency := runtime.NumCPU()
	if defaultConcurrency < 1 {
		defaultConcurrency = 1
	}

	cmd.Flags().IntVar(&renderConcurrency, "concurrency", defaultConcurrency, "Concurrent ffmpeg processes")
	cmd.Flags().BoolVar(&renderForce, "force", false, "Re-render even if segment output already exists")
	cmd.Flags().BoolVar(&renderDryRun, "dry-run", false, "Show what would change without rendering")
	cmd.Flags().BoolVar(&renderNoProgress, "no-progress", false, "Disable interactive progress output")
	cmd.Flags().StringSliceVar(&renderIndexArg, "index", nil, "Limit render to specific 1-based row index or range like 5-10 (repeat flag for multiple)")
	cmd.Flags().StringVar(&renderSampleTime, "sample-time", "", "Extract a single frame at the specified time (e.g., '5s', '1m30s', '0:30') for testing overlays")
	cmd.Flags().StringVar(&renderSampleOutput, "sample-output", "", "Output path for the sample frame (default: <segment_name>_sample_<time>.png)")
	addCollectionRenderFlags(cmd)

	return cmd
}

func runRender(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyGlobalCache(pp, cfg.GlobalCacheEnabled())

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	return runCollectionRender(ctx, cmd, pp, cfg)
}

func renderPreflightResult(clip project.Clip, err error) render.Result {
	return render.Result{
		Index:     clip.Sequence,
		ClipType:  clip.ClipType,
		TypeIndex: clip.TypeIndex,
		Title:     clipDisplayTitle(clip),
		Err:       err,
	}
}

func clipDisplayTitle(clip project.Clip) string {
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

func runRenderSample(ctx context.Context, cmd *cobra.Command, svc *render.Service, segments []render.Segment, timeline []project.Clip) error {
	if len(segments) == 0 {
		return fmt.Errorf("no segments to sample")
	}

	seg := segments[0]

	sampleTime, err := parseSampleTime(renderSampleTime)
	if err != nil {
		return fmt.Errorf("invalid sample time %q: %w", renderSampleTime, err)
	}

	outputPath := renderSampleOutput
	if outputPath == "" {
		base := render.SegmentBaseName(svc.Config.SegmentFilenameTemplate(), seg)
		if base == "" {
			base = fmt.Sprintf("segment_%03d", seg.Clip.TypeIndex)
		}
		timeStr := strings.ReplaceAll(renderSampleTime, ":", "_")
		timeStr = strings.ReplaceAll(timeStr, ".", "_")
		outputPath = fmt.Sprintf("%s_sample_%s.png", base, timeStr)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Extracting sample frame at %s to %s\n", renderSampleTime, outputPath)

	if err := svc.RenderSample(ctx, seg, sampleTime, outputPath); err != nil {
		return fmt.Errorf("failed to render sample: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Sample frame saved to: %s\n", outputPath)
	return nil
}

func parseSampleTime(timeStr string) (float64, error) {
	timeStr = strings.TrimSpace(timeStr)
	if timeStr == "" {
		return 0, fmt.Errorf("empty time string")
	}

	if d, err := parseDuration(timeStr); err == nil {
		return d.Seconds(), nil
	}

	if seconds, err := parseTimecode(timeStr); err == nil {
		return seconds, nil
	}

	if seconds, err := strconv.ParseFloat(timeStr, 64); err == nil {
		return seconds, nil
	}

	return 0, fmt.Errorf("could not parse as duration, timecode, or seconds")
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	return 0, fmt.Errorf("invalid duration format")
}

func parseTimecode(s string) (float64, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid timecode format")
	}

	var totalSeconds float64
	for i, part := range parts {
		val, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid timecode component %q: %w", part, err)
		}

		if len(parts) == 2 {
			if i == 0 {
				totalSeconds += val * 60
			} else {
				totalSeconds += val
			}
		} else if len(parts) == 3 {
			if i == 0 {
				totalSeconds += val * 3600
			} else if i == 1 {
				totalSeconds += val * 60
			} else {
				totalSeconds += val
			}
		} else {
			return 0, fmt.Errorf("timecode must have 2 or 3 components")
		}
	}

	return totalSeconds, nil
}
