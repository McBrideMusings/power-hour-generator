package render

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

// BuildFilterGraph constructs the ffmpeg video filter graph for a segment.
func BuildFilterGraph(row csvplan.Row, cfg config.Config) (string, error) {
	width := cfg.Video.Width
	height := cfg.Video.Height
	if width <= 0 || height <= 0 {
		return "", errors.New("invalid video dimensions")
	}
	if cfg.Video.FPS <= 0 {
		return "", errors.New("invalid video fps")
	}

	clipDuration := float64(row.DurationSeconds)
	if clipDuration <= 0 {
		return "", fmt.Errorf("row %03d missing duration", row.Index)
	}

	filters := []string{
		fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=1:flags=lanczos", width, height),
		fmt.Sprintf("pad=w=%d:h=%d:x=(ow-iw)/2:y=(oh-ih)/2:color=black", width, height),
		"setsar=1",
		fmt.Sprintf("fps=%d", cfg.Video.FPS),
	}

	fadeDur := math.Min(0.5, clipDuration/2)
	if fadeDur > 0 {
		filters = append(filters, fmt.Sprintf("fade=t=in:st=0:d=%s", formatFloat(fadeDur)))
		fadeOutStart := math.Max(clipDuration-fadeDur, 0)
		filters = append(filters, fmt.Sprintf("fade=t=out:st=%s:d=%s", formatFloat(fadeOutStart), formatFloat(fadeDur)))
	}

	overlays := buildOverlayFilters(row, cfg, clipDuration)
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
func BuildFFmpegCmd(row csvplan.Row, sourcePath, outputPath, videoFilters, audioFilters string, cfg config.Config) ([]string, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return nil, errors.New("source path is empty")
	}
	if strings.TrimSpace(outputPath) == "" {
		return nil, errors.New("output path is empty")
	}
	if strings.TrimSpace(videoFilters) == "" {
		return nil, errors.New("video filter graph is empty")
	}

	start := formatTimecode(row.Start)
	duration := strconv.Itoa(row.DurationSeconds)

	args := []string{
		"-hide_banner",
		"-y",
		"-ss", start,
		"-i", sourcePath,
		"-t", duration,
		"-vf", videoFilters,
	}

	if strings.TrimSpace(audioFilters) != "" {
		args = append(args, "-af", audioFilters)
	}

	args = append(args,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "20",
		"-pix_fmt", "yuv420p",
	)

	if cfg.Audio.ACodec != "" {
		args = append(args, "-c:a", cfg.Audio.ACodec)
	}
	if cfg.Audio.BitrateKbps > 0 {
		args = append(args, "-b:a", fmt.Sprintf("%dk", cfg.Audio.BitrateKbps))
	}
	if cfg.Audio.SampleRate > 0 {
		args = append(args, "-ar", strconv.Itoa(cfg.Audio.SampleRate))
	}

	args = append(args,
		"-movflags", "+faststart",
		outputPath,
	)

	return args, nil
}

func buildOverlayFilters(row csvplan.Row, cfg config.Config, clipDuration float64) []string {
	var filters []string

	beginText := renderOverlayTemplate(cfg.Overlays.BeginText.Template, row)
	if beginText != "" && cfg.Overlays.BeginText.DurationSec > 0 {
		start := 0.0
		end := math.Min(cfg.Overlays.BeginText.DurationSec, clipDuration)
		if end > start {
			filters = append(filters, buildDrawText(drawTextOptions{
				Text:         beginText,
				Start:        start,
				End:          end,
				FadeIn:       cfg.Overlays.BeginText.FadeInSec,
				FadeOut:      cfg.Overlays.BeginText.FadeOutSec,
				FontSize:     cfg.Overlays.FontSizeMain,
				FontFile:     cfg.Overlays.FontFile,
				FontColor:    cfg.Overlays.Color,
				OutlineColor: cfg.Overlays.OutlineColor,
				XExpr:        "40",
				YExpr:        "h-text_h-40",
			}))
		}
	}

	endText := renderOverlayTemplate(cfg.Overlays.EndText.Template, row)
	if endText != "" && cfg.Overlays.EndText.DurationSec > 0 {
		offset := cfg.Overlays.EndText.OffsetFromEndSec
		end := math.Max(clipDuration-offset, 0)
		start := math.Max(end-cfg.Overlays.EndText.DurationSec, 0)
		if end > start {
			filters = append(filters, buildDrawText(drawTextOptions{
				Text:         endText,
				Start:        start,
				End:          end,
				FadeIn:       cfg.Overlays.EndText.FadeInSec,
				FadeOut:      cfg.Overlays.EndText.FadeOutSec,
				FontSize:     cfg.Overlays.FontSizeMain,
				FontFile:     cfg.Overlays.FontFile,
				FontColor:    cfg.Overlays.Color,
				OutlineColor: cfg.Overlays.OutlineColor,
				XExpr:        "40",
				YExpr:        "h-text_h-40",
			}))
		}
	}

	if badge := renderOverlayTemplate(cfg.Overlays.IndexBadge.Template, row); badge != "" {
		filters = append(filters, buildDrawText(drawTextOptions{
			Text:         badge,
			Start:        0,
			End:          clipDuration,
			FontSize:     cfg.Overlays.FontSizeIndex,
			FontFile:     cfg.Overlays.FontFile,
			FontColor:    cfg.Overlays.Color,
			OutlineColor: cfg.Overlays.OutlineColor,
			XExpr:        "w-text_w-40",
			YExpr:        "h-text_h-40",
			Persistent:   cfg.Overlays.IndexBadge.PersistentValue(),
		}))
	}

	return filters
}

type drawTextOptions struct {
	Text         string
	Start        float64
	End          float64
	FadeIn       float64
	FadeOut      float64
	FontSize     int
	FontFile     string
	FontColor    string
	OutlineColor string
	XExpr        string
	YExpr        string
	Persistent   bool
}

func buildDrawText(opts drawTextOptions) string {
	duration := opts.End - opts.Start
	if duration <= 0 {
		return ""
	}

	values := []string{
		fmt.Sprintf("text='%s'", escapeDrawText(opts.Text)),
		fmt.Sprintf("fontsize=%d", max(opts.FontSize, 12)),
		fmt.Sprintf("fontcolor=%s", fallback(opts.FontColor, "white")),
		fmt.Sprintf("bordercolor=%s", fallback(opts.OutlineColor, "black")),
		"borderw=2",
		fmt.Sprintf("x=%s", fallback(opts.XExpr, "40")),
		fmt.Sprintf("y=%s", fallback(opts.YExpr, "h-text_h-40")),
		"line_spacing=4",
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

func renderOverlayTemplate(tmpl string, row csvplan.Row) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"{title}", row.Title,
		"{artist}", row.Artist,
		"{name}", row.Name,
		"{index}", strconv.Itoa(row.Index),
	)
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

	value = escapeFilterValue(value)
	value = strings.ReplaceAll(value, newlinePlaceholder, `\n`)
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
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ":", `\:`)
	value = strings.ReplaceAll(value, ",", `\,`)
	value = strings.ReplaceAll(value, "'", `\'`)
	return value
}
