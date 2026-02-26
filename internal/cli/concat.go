package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/tools"
	"powerhour/internal/tui"
)

var (
	concatOut    string
	concatDryRun bool
)

func newConcatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "concat",
		Short: "Concatenate rendered segments into a final video",
		RunE:  runConcat,
	}

	cmd.Flags().StringVar(&concatOut, "out", "", "Output file path (default: <project>/final.mp4)")
	cmd.Flags().BoolVar(&concatDryRun, "dry-run", false, "Print the resolved segment list without running ffmpeg")

	return cmd
}

func runConcat(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}

	outWriter := cmd.OutOrStdout()
	sw := tui.NewStatusWriter(outWriter)

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
	defer cancel()

	// Ensure tools are available.
	sw.Update("Checking tools...")
	ctx2 := tools.WithMinimums(ctx, cfg.ToolMinimums())
	if _, err := tools.EnsureAll(ctx2, tools.KnownTools(), func(msg string) {
		sw.Update(msg)
	}); err != nil {
		return err
	}

	// Load encoding profile (probe if not cached).
	sw.Update("Resolving encoding profile...")
	enc, err := resolveEncodingForConcat(ctx2, cfg)
	if err != nil {
		return err
	}

	// Resolve collections to build the timeline.
	sw.Update("Loading collections...")
	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return err
	}
	collections, err := resolver.LoadCollections()
	if err != nil {
		return err
	}

	// Build ordered segment list from timeline.
	sw.Update("Resolving timeline...")
	segments, err := render.ResolveTimelineSegments(pp, cfg, collections)
	if err != nil {
		return err
	}

	if len(segments) == 0 {
		return fmt.Errorf("no segments found; run `powerhour render` first")
	}

	if concatDryRun {
		sw.Stop()
		fmt.Fprintf(outWriter, "Segment order (%d clips):\n", len(segments))
		for i, seg := range segments {
			rel, rerr := filepath.Rel(pp.Root, seg.Path)
			if rerr != nil {
				rel = seg.Path
			}
			col := seg.CollectionName
			if col == "" {
				col = "-"
			}
			fmt.Fprintf(outWriter, "  %3d  %-15s %s\n", i+1, col, rel)
		}
		return nil
	}

	// Ensure project meta directory exists for the concat list.
	if err := pp.EnsureMetaDirs(); err != nil {
		return err
	}

	// Write the concat list.
	sw.Update("Writing concat list...")
	if err := render.WriteConcatList(pp.ConcatListFile, segments); err != nil {
		return err
	}

	// Determine output path.
	outputPath := concatOut
	if outputPath == "" {
		outputPath = filepath.Join(pp.Root, "final"+containerExt(enc.Container))
	}
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(pp.Root, outputPath)
	}

	sw.Update(fmt.Sprintf("Concatenating %d segments → %s", len(segments), filepath.Base(outputPath)))

	result, err := render.RunConcat(ctx, pp.ConcatListFile, outputPath, enc, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}

	sw.Stop()

	// Report result.
	info, statErr := os.Stat(result.OutputPath)
	sizeStr := ""
	if statErr == nil {
		sizeStr = fmt.Sprintf("  size: %s", formatBytes(info.Size()))
	}

	rel, rerr := filepath.Rel(pp.Root, result.OutputPath)
	if rerr != nil {
		rel = result.OutputPath
	}

	fmt.Fprintf(outWriter, "Done: %s\n", rel)
	fmt.Fprintf(outWriter, "  method: %s\n", result.Method)
	if sizeStr != "" {
		fmt.Fprintln(outWriter, sizeStr)
	}

	return nil
}

// resolveEncodingForConcat returns the merged ResolvedEncoding from cached
// profile + global defaults + project overrides. If no cached profile exists,
// it probes the machine.
func resolveEncodingForConcat(ctx context.Context, cfg config.Config) (tools.ResolvedEncoding, error) {
	profile := tools.LoadEncodingProfile()
	if profile == nil {
		ffmpegPath, err := tools.Lookup("ffmpeg")
		if err != nil {
			return tools.ResolvedEncoding{}, fmt.Errorf("locate ffmpeg for encoding probe: %w", err)
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		p, err := tools.ProbeEncoders(probeCtx, ffmpegPath)
		if err != nil {
			return tools.ResolvedEncoding{}, fmt.Errorf("probe encoders: %w", err)
		}
		_ = tools.SaveEncodingProfile(p)
		profile = &p
	}

	global := tools.LoadEncodingDefaults()
	return tools.ResolveEncoding(profile, global, encodingConfigToDefaults(cfg.Encoding)), nil
}

// encodingConfigToDefaults converts a project EncodingConfig to the tools
// EncodingDefaults type used by ResolveEncoding.
func encodingConfigToDefaults(enc config.EncodingConfig) tools.EncodingDefaults {
	return tools.EncodingDefaults{
		VideoCodec:       enc.VideoCodec,
		Width:            enc.Width,
		Height:           enc.Height,
		FPS:              enc.FPS,
		CRF:              enc.CRF,
		Preset:           enc.Preset,
		VideoBitrate:     enc.VideoBitrate,
		Container:        enc.Container,
		AudioCodec:       enc.AudioCodec,
		AudioBitrate:     enc.AudioBitrate,
		SampleRate:       enc.SampleRate,
		Channels:         enc.Channels,
		LoudnormEnabled:  enc.LoudnormEnabled,
		LoudnormLUFS:     enc.LoudnormLUFS,
		LoudnormTruePeak: enc.LoudnormTruePeak,
		LoudnormLRA:      enc.LoudnormLRA,
	}
}

func containerExt(container string) string {
	switch container {
	case "mkv":
		return ".mkv"
	case "mov":
		return ".mov"
	default:
		return ".mp4"
	}
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n := n / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
