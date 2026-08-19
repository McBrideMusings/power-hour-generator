package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/state"
	"powerhour/internal/tools"
	"powerhour/internal/tui"
	"powerhour/pkg/csvplan"
)

var (
	concatOut    string
	concatDryRun bool
	concatForce  bool
)

func newConcatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "concat",
		Short: "Concatenate rendered segments into a final video",
		RunE:  runConcat,
	}

	cmd.Flags().StringVar(&concatOut, "out", "", "Output file path (default: <project>/powerhour.mp4)")
	cmd.Flags().BoolVar(&concatDryRun, "dry-run", false, "Print the resolved segment list without running ffmpeg")
	cmd.Flags().BoolVar(&concatForce, "force", false, "Re-render inline file segments even if they already exist")

	return cmd
}

func runConcat(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("concat")
	defer gcloser.Close()
	glogf("concat started")

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}
	glogf("project resolved: %s", pp.Root)

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	glogf("config loaded")

	outWriter := cmd.OutOrStdout()
	sw := tui.NewStatusWriter(outWriter)

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
	defer cancel()

	// Ensure tools are available.
	sw.Update("Checking tools...")
	ctx2 := tools.WithMinimums(ctx, cfg.ToolMinimums())
	if _, err := tools.EnsureAll(ctx2, tools.RequiredTools(), func(msg string) {
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

	glogf("resolved %d segments", len(segments))

	if len(segments) == 0 {
		return fmt.Errorf("no segments found; run `powerhour render` first")
	}

	// Check for missing or stale segments and auto-render if needed.
	if !concatDryRun && hasMissingSegments(segments) {
		sw.Update("Rendering missing segments...")
		glogf("auto-render: missing segments detected, triggering render")
		savedConcurrency := renderConcurrency
		savedNoProgress := renderNoProgress
		renderConcurrency = runtime.NumCPU()
		renderNoProgress = true
		renderErr := runCollectionRender(ctx, cmd, pp, cfg)
		renderConcurrency = savedConcurrency
		renderNoProgress = savedNoProgress
		if renderErr != nil {
			return fmt.Errorf("auto-render: %w", renderErr)
		}
		if still := listMissingSegments(segments); len(still) > 0 {
			return fmt.Errorf("render completed but %d segment(s) still missing:\n  %s", len(still), strings.Join(still, "\n  "))
		}
	}

	if concatDryRun {
		sw.Stop()

		// Build hash info for inline file entries so their status can show
		// stale (hash mismatch) vs. rendered (✓), mirroring `status`'s
		// timeline section.
		rs, _ := state.Load(pp.RenderStateFile)
		inlineHashes := buildInlineHashes(pp, cfg, rs)

		green := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Inline(true)
		yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Inline(true)
		red := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Inline(true)

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

			statusLabel := green.Render(segStatusOK)
			switch segmentDryRunStatus(seg.Path, inlineHashes) {
			case segStatusMissing:
				statusLabel = red.Render(segStatusMissing)
			case segStatusStale:
				statusLabel = yellow.Render(segStatusStale)
			}

			fmt.Fprintf(outWriter, "  %3d  %-15s %-8s %s\n", i+1, col, statusLabel, rel)
		}
		return nil
	}

	// Re-encode any inline file entries to normalized MP4 segments.
	for _, entry := range cfg.Timeline.Sequence {
		if entry.File != "" {
			sw.Update("Rendering inline files...")
			svc, err := render.NewService(ctx2, pp, cfg, nil)
			if err != nil {
				return fmt.Errorf("init render service: %w", err)
			}
			if err := renderInlineFiles(ctx2, pp, cfg, svc, concatForce); err != nil {
				return err
			}
			break
		}
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
		outputPath = filepath.Join(pp.Root, "powerhour"+containerExt(enc.Container))
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
	glogf("concat finished: %s (method=%s)", result.OutputPath, result.Method)

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

// inlineHashInfo carries the stored (render-state) and freshly-computed
// input hash for an inline file entry's segment, used to detect staleness
// in `concat --dry-run`.
type inlineHashInfo struct {
	stored   string
	computed string
}

// Dry-run status tokens, mirroring the per-row status labels `status` shows
// for the timeline section.
const (
	segStatusOK      = "✓"
	segStatusStale   = "stale"
	segStatusMissing = "missing"
)

// buildInlineHashes computes, for every inline `file:` timeline entry, the
// current input hash and the hash last recorded in render state — keyed by
// the entry's resolved segment output path. This mirrors the construction
// in status.go's runStatus and collections_render.go's renderInlineFiles.
func buildInlineHashes(pp paths.ProjectPaths, cfg config.Config, rs *state.RenderState) map[string]inlineHashInfo {
	tmpl := cfg.SegmentFilenameTemplate()
	inlineHashes := make(map[string]inlineHashInfo)
	for seqIdx, entry := range cfg.Timeline.Sequence {
		if entry.File == "" {
			continue
		}
		sourcePath := entry.File
		if !filepath.IsAbs(sourcePath) {
			sourcePath = filepath.Join(pp.Root, sourcePath)
		}
		segPath := render.InlineSegmentPath(pp.SegmentsDir, seqIdx, sourcePath)
		fadeIn, fadeOut := config.ResolveFade(entry.Fade, entry.FadeIn, entry.FadeOut)
		inlineSeg := render.Segment{
			Clip: project.Clip{
				Sequence:       seqIdx + 1,
				ClipType:       project.ClipType("__inline__"),
				TypeIndex:      seqIdx,
				SourceKind:     project.SourceKindPlan,
				FadeInSeconds:  fadeIn,
				FadeOutSeconds: fadeOut,
				Row: csvplan.Row{
					Index: seqIdx + 1,
					Link:  sourcePath,
				},
			},
			OutputPath: segPath,
		}
		info := inlineHashInfo{computed: state.SegmentInputHash(inlineSeg, tmpl)}
		if rs != nil {
			if prior, ok := rs.Segments[segPath]; ok {
				info.stored = prior.InputHash
			}
		}
		inlineHashes[segPath] = info
	}
	return inlineHashes
}

// segmentDryRunStatus reports whether a resolved segment path is missing,
// stale (inline file entry whose stored hash doesn't match the current
// computed hash), or ok. Only inline file entries carry hash-based drift
// detection; collection-rendered segments only get an existence check.
func segmentDryRunStatus(path string, inlineHashes map[string]inlineHashInfo) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return segStatusMissing
	}
	if info, ok := inlineHashes[path]; ok && info.stored != "" && info.stored != info.computed {
		return segStatusStale
	}
	return segStatusOK
}

func hasMissingSegments(segments []render.TimelineSegmentPath) bool {
	for _, seg := range segments {
		if _, err := os.Stat(seg.Path); os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func listMissingSegments(segments []render.TimelineSegmentPath) []string {
	var missing []string
	for _, seg := range segments {
		if _, err := os.Stat(seg.Path); os.IsNotExist(err) {
			missing = append(missing, seg.Path)
		}
	}
	return missing
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
