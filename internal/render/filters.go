package render

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

// BuildFilterGraph constructs the ffmpeg video filter graph for a segment.
func BuildFilterGraph(seg Segment, cfg config.Config) (string, error) {
	width := cfg.Video.Width
	height := cfg.Video.Height
	if width <= 0 || height <= 0 {
		return "", errors.New("invalid video dimensions")
	}
	if cfg.Video.FPS <= 0 {
		return "", errors.New("invalid video fps")
	}

	clip := seg.Clip
	clipDuration := float64(clip.DurationSeconds)
	if clipDuration <= 0 {
		return "", fmt.Errorf("clip %s#%d missing duration", clip.ClipType, clip.TypeIndex)
	}

	filters := []string{
		fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=1:flags=lanczos", width, height),
		fmt.Sprintf("pad=w=%d:h=%d:x=(ow-iw)/2:y=(oh-ih)/2:color=black", width, height),
		"setsar=1",
		fmt.Sprintf("fps=%d", cfg.Video.FPS),
	}

	if fadeIn := math.Min(clipDuration, clip.FadeInSeconds); fadeIn > 0 {
		filters = append(filters, fmt.Sprintf("fade=t=in:st=0:d=%s", formatFloat(fadeIn)))
	}
	if fadeOut := math.Min(clipDuration, clip.FadeOutSeconds); fadeOut > 0 {
		start := math.Max(clipDuration-fadeOut, 0)
		filters = append(filters, fmt.Sprintf("fade=t=out:st=%s:d=%s", formatFloat(start), formatFloat(fadeOut)))
	}

	overlays := buildOverlayFilters(seg, cfg, clipDuration)
	filters = append(filters, overlays...)

	return strings.Join(filters, ","), nil
}

// BuildAudioFilters builds the ffmpeg audio filter chain.
func BuildAudioFilters(cfg config.Config) string {
	filters := []string{}

	if cfg.Audio.Loudnorm.EnabledValue() {
		loudnorm := cfg.Audio.Loudnorm
		params := []string{
			fmt.Sprintf("I=%s", formatFloat(loudnorm.IntegratedLUFSValue())),
			fmt.Sprintf("TP=%s", formatFloat(loudnorm.TruePeakValue())),
			fmt.Sprintf("LRA=%s", formatFloat(loudnorm.LRAValue())),
		}
		filters = append(filters, "loudnorm="+strings.Join(params, ":"))
	}

	if cfg.Audio.SampleRate > 0 {
		filters = append(filters, fmt.Sprintf("aresample=%d", cfg.Audio.SampleRate))
	}

	return strings.Join(filters, ",")
}

// BuildFFmpegCmd assembles the ffmpeg CLI arguments for the segment render.
func BuildFFmpegCmd(seg Segment, outputPath, videoFilters, audioFilters string, cfg config.Config) ([]string, error) {
	sourcePath := strings.TrimSpace(seg.SourcePath)
	if sourcePath == "" {
		sourcePath = strings.TrimSpace(seg.CachedPath)
	}
	if sourcePath == "" {
		return nil, errors.New("source path is empty")
	}
	if strings.TrimSpace(outputPath) == "" {
		return nil, errors.New("output path is empty")
	}
	if strings.TrimSpace(videoFilters) == "" {
		return nil, errors.New("video filter graph is empty")
	}

	clip := seg.Clip
	duration := clip.DurationSeconds
	if duration <= 0 {
		return nil, fmt.Errorf("clip %s#%d missing duration", clip.ClipType, clip.TypeIndex)
	}

	args := []string{
		"-hide_banner",
		"-y",
	}

	if clip.SourceKind == project.SourceKindPlan {
		args = append(args, "-ss", formatTimecode(clip.Row.Start))
	}

	args = append(args,
		"-i", sourcePath,
		"-t", strconv.Itoa(duration),
		"-vf", videoFilters,
	)

	if strings.TrimSpace(audioFilters) != "" {
		args = append(args, "-af", audioFilters)
	}

	videoCodec := strings.TrimSpace(cfg.Video.Codec)
	if videoCodec == "" {
		videoCodec = "libx264"
	}
	args = append(args, "-c:v", videoCodec)

	if preset := strings.TrimSpace(cfg.Video.Preset); preset != "" {
		args = append(args, "-preset", preset)
	}

	if cfg.Video.CRF >= 0 {
		args = append(args, "-crf", strconv.Itoa(cfg.Video.CRF))
	}

	args = append(args, "-pix_fmt", "yuv420p")

	if acodec := strings.TrimSpace(cfg.Audio.ACodec); acodec != "" {
		args = append(args, "-c:a", acodec)
	}
	if cfg.Audio.BitrateKbps > 0 {
		args = append(args, "-b:a", fmt.Sprintf("%dk", cfg.Audio.BitrateKbps))
	}
	if cfg.Audio.SampleRate > 0 {
		args = append(args, "-ar", strconv.Itoa(cfg.Audio.SampleRate))
	}
	if cfg.Audio.Channels > 0 {
		args = append(args, "-ac", strconv.Itoa(cfg.Audio.Channels))
	}

	args = append(args,
		"-movflags", "+faststart",
		outputPath,
	)

	return args, nil
}

func buildOverlayFilters(seg Segment, cfg config.Config, clipDuration float64) []string {
	var filters []string

	clip := seg.Clip
	row := clip.Row

	baseStyle := seg.Profile.DefaultStyle

	segments := seg.Segments
	if len(segments) == 0 {
		segments = seg.Profile.Segments
	}

	// Sort segments by z_index (if set), otherwise preserve array order
	sortedSegments := sortSegmentsByZIndex(segments)

	for _, segment := range sortedSegments {
		if segment.Disabled {
			continue
		}

		text := renderOverlayTemplate(segment.Template, row)
		text = applyTextTransform(text, segment.Transform)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		timing, ok := resolveTiming(segment.Timing, clipDuration)
		if !ok {
			continue
		}

		style := resolveTextStyle(baseStyle, segment.Style)
		position := resolvePosition(segment.Position)

		filters = append(filters, buildDrawText(drawTextOptions{
			Text:          text,
			Start:         timing.Start,
			End:           timing.End,
			FadeIn:        timing.FadeIn,
			FadeOut:       timing.FadeOut,
			FontSize:      style.FontSize,
			FontFile:      style.FontFile,
			FontColor:     style.FontColor,
			OutlineColor:  style.OutlineColor,
			OutlineWidth:  style.OutlineWidth,
			LineSpacing:   style.LineSpacing,
			LetterSpacing: style.LetterSpacing,
			XExpr:         position.X,
			YExpr:         position.Y,
			Persistent:    timing.Persistent,
		}))
	}

	return filters
}

type drawTextOptions struct {
	Text          string
	Start         float64
	End           float64
	FadeIn        float64
	FadeOut       float64
	FontSize      int
	FontFile      string
	FontColor     string
	OutlineColor  string
	OutlineWidth  int
	LineSpacing   int
	LetterSpacing int
	XExpr         string
	YExpr         string
	Persistent    bool
}

func buildDrawText(opts drawTextOptions) string {
	duration := opts.End - opts.Start
	if duration <= 0 {
		return ""
	}

	outlineWidth := opts.OutlineWidth
	if outlineWidth < 0 {
		outlineWidth = 0
	}

	values := []string{
		fmt.Sprintf("text='%s'", escapeDrawText(opts.Text)),
		fmt.Sprintf("fontsize=%d", max(opts.FontSize, 12)),
		fmt.Sprintf("fontcolor=%s", fallback(opts.FontColor, "white")),
		fmt.Sprintf("bordercolor=%s", fallback(opts.OutlineColor, "black")),
		fmt.Sprintf("borderw=%d", outlineWidth),
		fmt.Sprintf("x=%s", fallback(opts.XExpr, "40")),
		fmt.Sprintf("y=%s", fallback(opts.YExpr, "h-text_h-40")),
	}

	if opts.LineSpacing != 0 {
		values = append(values, fmt.Sprintf("line_spacing=%d", opts.LineSpacing))
	}

	if opts.LetterSpacing != 0 {
		values = append(values, fmt.Sprintf("letter_spacing=%d", opts.LetterSpacing))
	}

	if strings.TrimSpace(opts.FontFile) != "" {
		values = append(values, fmt.Sprintf("fontfile='%s'", escapeFFmpegPath(opts.FontFile)))
	}

	if !opts.Persistent {
		enable := fmt.Sprintf("between(t,%s,%s)", formatFloat(opts.Start), formatFloat(opts.End))
		values = append(values, fmt.Sprintf("enable='%s'", escapeFilterValue(enable)))
		alpha := alphaExpression(opts.Start, opts.End, opts.FadeIn, opts.FadeOut)
		values = append(values, fmt.Sprintf("alpha='%s'", escapeFilterValue(alpha)))
	}

	return "drawtext=" + strings.Join(values, ":")
}

type resolvedTextStyle struct {
	FontFile      string
	FontSize      int
	FontColor     string
	OutlineColor  string
	OutlineWidth  int
	LineSpacing   int
	LetterSpacing int
}

type resolvedPosition struct {
	X string
	Y string
}

type resolvedTiming struct {
	Start      float64
	End        float64
	FadeIn     float64
	FadeOut    float64
	Persistent bool
}

func resolveTextStyle(defaults config.TextStyle, override config.TextStyle) resolvedTextStyle {
	const (
		baseFontSize     = 42
		baseOutlineWidth = 2
		baseLineSpacing  = 4
	)

	resolved := resolvedTextStyle{
		FontFile:      strings.TrimSpace(defaults.FontFile),
		FontColor:     fallback(defaults.FontColor, "white"),
		OutlineColor:  fallback(defaults.OutlineColor, "black"),
		OutlineWidth:  baseOutlineWidth,
		LineSpacing:   baseLineSpacing,
		LetterSpacing: 0,
		FontSize:      baseFontSize,
	}

	if defaults.FontSize != nil && *defaults.FontSize > 0 {
		resolved.FontSize = *defaults.FontSize
	}
	if defaults.OutlineWidth != nil {
		resolved.OutlineWidth = *defaults.OutlineWidth
	}
	if defaults.LineSpacing != nil {
		resolved.LineSpacing = *defaults.LineSpacing
	}
	if defaults.LetterSpacing != nil {
		resolved.LetterSpacing = *defaults.LetterSpacing
	}

	if strings.TrimSpace(override.FontFile) != "" {
		resolved.FontFile = strings.TrimSpace(override.FontFile)
	}
	if override.FontSize != nil && *override.FontSize > 0 {
		resolved.FontSize = *override.FontSize
	}
	if strings.TrimSpace(override.FontColor) != "" {
		resolved.FontColor = override.FontColor
	}
	if strings.TrimSpace(override.OutlineColor) != "" {
		resolved.OutlineColor = override.OutlineColor
	}
	if override.OutlineWidth != nil {
		resolved.OutlineWidth = *override.OutlineWidth
	}
	if override.LineSpacing != nil {
		resolved.LineSpacing = *override.LineSpacing
	}
	if override.LetterSpacing != nil {
		resolved.LetterSpacing = *override.LetterSpacing
	}

	if strings.TrimSpace(resolved.FontColor) == "" {
		resolved.FontColor = "white"
	}
	if strings.TrimSpace(resolved.OutlineColor) == "" {
		resolved.OutlineColor = "black"
	}

	return resolved
}

func resolvePosition(pos config.PositionSpec) resolvedPosition {
	x := strings.TrimSpace(pos.XExpr)
	y := strings.TrimSpace(pos.YExpr)

	origin := strings.ToLower(strings.TrimSpace(pos.Origin))
	if origin == "" {
		origin = "bottom-left"
	}

	if x == "" || y == "" {
		switch origin {
		case "bottom-left":
			if x == "" {
				x = formatFloat(pos.OffsetX)
			}
			if y == "" {
				y = subtractOffset("h-text_h", pos.OffsetY)
			}
		case "bottom-right":
			if x == "" {
				x = subtractOffset("w-text_w", pos.OffsetX)
			}
			if y == "" {
				y = subtractOffset("h-text_h", pos.OffsetY)
			}
		case "top-left":
			if x == "" {
				x = formatFloat(pos.OffsetX)
			}
			if y == "" {
				y = formatFloat(pos.OffsetY)
			}
		case "top-right":
			if x == "" {
				x = subtractOffset("w-text_w", pos.OffsetX)
			}
			if y == "" {
				y = formatFloat(pos.OffsetY)
			}
		case "center":
			if x == "" {
				x = addOffset("(w-text_w)/2", pos.OffsetX)
			}
			if y == "" {
				y = addOffset("(h-text_h)/2", pos.OffsetY)
			}
		}
	}

	if strings.TrimSpace(x) == "" {
		x = "40"
	}
	if strings.TrimSpace(y) == "" {
		y = "h-text_h-40"
	}

	return resolvedPosition{X: x, Y: y}
}

func resolveTiming(spec config.TimingSpec, clipDuration float64) (resolvedTiming, bool) {
	start := resolveStartPoint(spec.Start, clipDuration)
	end, persistent := resolveEndPoint(spec.End, clipDuration)

	if persistent {
		if start >= clipDuration {
			return resolvedTiming{}, false
		}
		end = clipDuration
	}

	start = clamp(start, 0, clipDuration)
	end = clamp(end, 0, clipDuration)

	if end <= start {
		return resolvedTiming{}, false
	}

	fadeIn := clamp(spec.FadeIn, 0, clipDuration)
	fadeOut := clamp(spec.FadeOut, 0, clipDuration)

	result := resolvedTiming{
		Start:   start,
		End:     end,
		FadeIn:  fadeIn,
		FadeOut: fadeOut,
	}

	if persistent && start <= 0 && end >= clipDuration && fadeIn == 0 && fadeOut == 0 {
		result.Persistent = true
	}

	return result, true
}

func resolveStartPoint(point config.TimePointSpec, clipDuration float64) float64 {
	offset := point.OffsetSec
	switch strings.ToLower(strings.TrimSpace(point.Type)) {
	case "from_end":
		return clipDuration - offset
	case "absolute":
		return offset
	default:
		return offset
	}
}

func resolveEndPoint(point config.TimePointSpec, clipDuration float64) (float64, bool) {
	offset := point.OffsetSec
	switch strings.ToLower(strings.TrimSpace(point.Type)) {
	case "from_end":
		return clipDuration - offset, false
	case "absolute":
		return offset, false
	case "persistent":
		return clipDuration, true
	default:
		return offset, false
	}
}

func applyTextTransform(value, transform string) string {
	switch strings.ToLower(strings.TrimSpace(transform)) {
	case "uppercase":
		return strings.ToUpper(value)
	case "lowercase":
		return strings.ToLower(value)
	default:
		return value
	}
}

func addOffset(base string, offset float64) string {
	if math.Abs(offset) < 1e-6 {
		return base
	}
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	return fmt.Sprintf("%s%s%s", base, sign, formatFloat(offset))
}

func subtractOffset(base string, offset float64) string {
	if math.Abs(offset) < 1e-6 {
		return base
	}
	if offset < 0 {
		return addOffset(base, -offset)
	}
	return fmt.Sprintf("%s-%s", base, formatFloat(offset))
}

func renderOverlayTemplate(tmpl string, row csvplan.Row) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}

	// Start with standard fields
	replacements := []string{
		"{title}", row.Title,
		"{artist}", row.Artist,
		"{name}", row.Name,
		"{index}", strconv.Itoa(row.Index),
	}

	// Add custom fields from Row.CustomFields
	if row.CustomFields != nil {
		for key, value := range row.CustomFields {
			// Support both lowercase and original case
			replacements = append(replacements, "{"+key+"}", value)
			lowerKey := strings.ToLower(key)
			if lowerKey != key {
				replacements = append(replacements, "{"+lowerKey+"}", value)
			}
		}
	}

	replacer := strings.NewReplacer(replacements...)
	rendered := strings.TrimSpace(replacer.Replace(tmpl))
	return rendered
}

func alphaExpression(start, end, fadeIn, fadeOut float64) string {
	duration := end - start
	if duration <= 0 {
		return "0"
	}
	fadeIn = clamp(fadeIn, 0, duration)
	fadeOut = clamp(fadeOut, 0, duration)

	startStr := formatFloat(start)
	endStr := formatFloat(end)

	var builder strings.Builder
	builder.WriteString("if(lt(t,")
	builder.WriteString(startStr)
	builder.WriteString("),0,")

	if fadeIn > 0 {
		builder.WriteString("if(lt(t,")
		builder.WriteString(formatFloat(start + fadeIn))
		builder.WriteString("),(t-")
		builder.WriteString(startStr)
		builder.WriteString(")/")
		builder.WriteString(formatFloat(fadeIn))
		builder.WriteString(",")
	}

	if fadeOut > 0 {
		builder.WriteString("if(lt(t,")
		builder.WriteString(formatFloat(end - fadeOut))
		builder.WriteString("),1,if(lt(t,")
		builder.WriteString(endStr)
		builder.WriteString("),(")
		builder.WriteString(endStr)
		builder.WriteString("-t)/")
		builder.WriteString(formatFloat(fadeOut))
		builder.WriteString(",0))")
	} else {
		builder.WriteString("if(lt(t,")
		builder.WriteString(endStr)
		builder.WriteString("),1,0)")
	}

	if fadeIn > 0 {
		builder.WriteString(")")
	}
	builder.WriteString(")")

	return builder.String()
}

func formatTimecode(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := d.Seconds()
	hours := int(totalSeconds) / 3600
	minutes := (int(totalSeconds) % 3600) / 60
	seconds := totalSeconds - float64(hours*3600+minutes*60)

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%06.3f", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%06.3f", minutes, seconds)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func clamp(value, minVal, maxVal float64) float64 {
	return math.Max(minVal, math.Min(maxVal, value))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

func escapeDrawText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")

	const newlinePlaceholder = "\u0000"
	value = strings.ReplaceAll(value, "\n", newlinePlaceholder)

	value = escapeFilterValueNoQuotes(value)
	value = strings.ReplaceAll(value, newlinePlaceholder, `\n`)
	value = strings.ReplaceAll(value, "'", "''")
	return value
}

func escapeFFmpegPath(value string) string {
	value = filepath.Clean(value)
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ":", `\:`)
	value = strings.ReplaceAll(value, "'", `\'`)
	return value
}

func escapeFilterValue(value string) string {
	value = escapeFilterValueNoQuotes(value)
	value = strings.ReplaceAll(value, "'", `\'`)
	return value
}

func escapeFilterValueNoQuotes(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ":", `\:`)
	value = strings.ReplaceAll(value, ",", `\,`)
	return value
}

// sortSegmentsByZIndex sorts overlay segments by their z_index field.
// Segments without z_index preserve their original array order (stable sort).
// Lower z_index = drawn first (behind), higher z_index = drawn last (on top).
func sortSegmentsByZIndex(segments []config.OverlaySegment) []config.OverlaySegment {
	// Create a copy to avoid modifying the original
	sorted := make([]config.OverlaySegment, len(segments))
	copy(sorted, segments)

	// Create a slice of indices with their original positions for stable sorting
	type indexedSegment struct {
		segment       config.OverlaySegment
		originalIndex int
	}

	indexed := make([]indexedSegment, len(sorted))
	for i, seg := range sorted {
		indexed[i] = indexedSegment{segment: seg, originalIndex: i}
	}

	// Stable sort by z_index, then by original index
	sort.SliceStable(indexed, func(i, j int) bool {
		a, b := indexed[i], indexed[j]

		// If both have z_index, sort by z_index
		if a.segment.ZIndex != nil && b.segment.ZIndex != nil {
			if *a.segment.ZIndex != *b.segment.ZIndex {
				return *a.segment.ZIndex < *b.segment.ZIndex
			}
			// If z_index is equal, preserve original order
			return a.originalIndex < b.originalIndex
		}

		// If only a has z_index, it should be sorted by its value
		if a.segment.ZIndex != nil {
			// Treat missing z_index as 0
			return *a.segment.ZIndex < 0
		}

		// If only b has z_index
		if b.segment.ZIndex != nil {
			// Treat missing z_index as 0
			return 0 < *b.segment.ZIndex
		}

		// If neither has z_index, preserve original order
		return a.originalIndex < b.originalIndex
	})

	// Extract sorted segments
	for i, item := range indexed {
		sorted[i] = item.segment
	}

	return sorted
}
