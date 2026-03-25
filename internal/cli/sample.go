package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
)

var (
	sampleIndex      int
	sampleCollection string
	sampleOutput     string
)

func newSampleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sample <time>",
		Short: "Extract a single frame for previewing overlays",
		Long: `Extract a single frame from the rendered timeline at a given time.

Without --index, the time is treated as an absolute position in the
concatenated timeline (e.g. "10m" shows what would be at the 10-minute mark).

With --index, the time is relative to a specific clip. If --collection is
also provided, --index refers to the row number within that collection.
Otherwise, --index refers to the position in the full timeline order
(including interstitials).`,
		Args: cobra.ExactArgs(1),
		RunE: runSample,
	}

	cmd.Flags().IntVar(&sampleIndex, "index", 0, "Target a specific clip (timeline slot, or collection row if --collection is set)")
	cmd.Flags().StringVar(&sampleCollection, "collection", "", "Narrow --index to a specific collection's rows (requires --index)")
	cmd.Flags().StringVar(&sampleOutput, "output", "", "Output file path (default: auto-generated PNG)")

	return cmd
}

func runSample(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	timeArg := args[0]
	sampleTime, err := parseSampleTime(timeArg)
	if err != nil {
		return fmt.Errorf("invalid time %q: %w", timeArg, err)
	}

	if sampleCollection != "" && sampleIndex == 0 {
		return fmt.Errorf("--collection requires --index")
	}

	glogf, gcloser := logx.StartCommand("sample")
	defer gcloser.Close()
	glogf("sample started")

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		return err
	}

	collectionClips, err := resolver.BuildCollectionClips(collections)
	if err != nil {
		return err
	}

	svc, err := render.NewService(ctx, pp, cfg, nil)
	if err != nil {
		return err
	}
	svc.SetWriters(cmd.OutOrStdout(), nil)

	// Resolve which clip to sample based on flags.
	var targetClip project.CollectionClip
	var clipOffset float64

	if sampleIndex > 0 {
		// Clip-relative mode: find the specific clip, use sampleTime as offset within it.
		clipOffset = sampleTime
		if sampleCollection != "" {
			// --collection + --index: index into that collection's rows
			found := false
			for _, cc := range collectionClips {
				if cc.CollectionName == sampleCollection && cc.Clip.Row.Index == sampleIndex {
					targetClip = cc
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("collection %q row %d not found", sampleCollection, sampleIndex)
			}
		} else {
			// --index only: index into the full timeline order
			timeline, tlErr := render.ResolveTimelineClips(cfg, collectionClips)
			if tlErr != nil {
				return fmt.Errorf("resolve timeline: %w", tlErr)
			}
			if sampleIndex < 1 || sampleIndex > len(timeline) {
				return fmt.Errorf("timeline index %d out of range (1-%d)", sampleIndex, len(timeline))
			}
			targetClip = timeline[sampleIndex-1].CollectionClip
		}
	} else {
		// Timeline-absolute mode: find which clip is at the given time.
		timeline, tlErr := render.ResolveTimelineClips(cfg, collectionClips)
		if tlErr != nil {
			return fmt.Errorf("resolve timeline: %w", tlErr)
		}

		tc, offset, findErr := findClipAtTime(timeline, sampleTime)
		if findErr != nil {
			return findErr
		}
		targetClip = tc.CollectionClip
		clipOffset = offset

		title := clipDisplayTitle(targetClip.Clip)
		fmt.Fprintf(cmd.OutOrStdout(), "Timeline %s → %s #%d %q at %s\n",
			formatSampleTime(sampleTime),
			targetClip.CollectionName,
			targetClip.Clip.Row.Index,
			title,
			formatSampleTime(clipOffset))
	}

	// Build the render segment for the target clip.
	seg, err := buildCollectionRenderSegment(pp, cfg, idx, resolver, targetClip)
	if err != nil {
		return fmt.Errorf("build segment: %w", err)
	}

	// Generate output path.
	outputPath := sampleOutput
	if outputPath == "" {
		base := render.SegmentBaseName(cfg.SegmentFilenameTemplate(), seg)
		if base == "" {
			base = fmt.Sprintf("segment_%03d", targetClip.Clip.Row.Index)
		}
		timeStr := strings.ReplaceAll(timeArg, ":", "_")
		timeStr = strings.ReplaceAll(timeStr, ".", "_")
		outputPath = fmt.Sprintf("%s_sample_%s.png", base, timeStr)
	}

	if err := svc.RenderSample(ctx, seg, clipOffset, outputPath); err != nil {
		return fmt.Errorf("sample failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Sample saved to: %s\n", outputPath)
	return nil
}

func findClipAtTime(timeline []render.TimelineClip, absoluteTime float64) (render.TimelineClip, float64, error) {
	var cumulative float64
	for _, tc := range timeline {
		duration := float64(tc.CollectionClip.Clip.DurationSeconds)
		if duration <= 0 {
			duration = 60 // fallback
		}
		if absoluteTime < cumulative+duration {
			return tc, absoluteTime - cumulative, nil
		}
		cumulative += duration
	}
	return render.TimelineClip{}, 0, fmt.Errorf("time %s exceeds total timeline duration %s",
		formatSampleTime(absoluteTime), formatSampleTime(cumulative))
}

func formatSampleTime(seconds float64) string {
	total := int(seconds)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

