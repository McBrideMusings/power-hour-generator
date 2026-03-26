package render

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

// PresetFunc generates drawtext filter strings for an overlay preset.
type PresetFunc func(opts map[string]string, row csvplan.Row, clipDuration float64) []string

// OverlayMoment maps an overlay name to a sample timestamp (midpoint of its
// visible window) for use by the sample command.
type OverlayMoment struct {
	Name       string
	SampleTime float64 // midpoint of the overlay's visible window
}

// MomentsFunc returns the named sample moments for a preset.
type MomentsFunc func(opts map[string]string, row csvplan.Row, clipDuration float64) []OverlayMoment

var presetRegistry = map[string]PresetFunc{
	"song-info": presetSongInfo,
	"drink":     presetDrink,
	"custom":    nil, // handled separately via raw filters
	"none":      nil, // no overlays
}

var momentsRegistry = map[string]MomentsFunc{
	"song-info": momentsSongInfo,
	"drink":     momentsDrink,
}

// LookupPreset returns the preset function for a given type name.
func LookupPreset(typeName string) (PresetFunc, bool) {
	fn, ok := presetRegistry[typeName]
	return fn, ok
}

// LookupMoments returns the moments function for a given preset type.
func LookupMoments(typeName string) (MomentsFunc, bool) {
	fn, ok := momentsRegistry[typeName]
	return fn, ok
}

// ResolveOverlayMoments returns all named sample moments for the given overlays.
func ResolveOverlayMoments(overlays []config.OverlayEntry, row csvplan.Row, clipDuration float64) []OverlayMoment {
	var moments []OverlayMoment
	for _, entry := range overlays {
		typeName := strings.TrimSpace(entry.Type)
		fn, ok := momentsRegistry[typeName]
		if !ok || fn == nil {
			continue
		}
		moments = append(moments, fn(entry.Options, row, clipDuration)...)
	}
	return moments
}

// ExpandOverlays converts overlay entries into drawtext filter strings.
func ExpandOverlays(overlays []config.OverlayEntry, row csvplan.Row, clipDuration float64) []string {
	var filters []string
	for _, entry := range overlays {
		typeName := strings.TrimSpace(entry.Type)
		switch typeName {
		case "none":
			continue
		case "custom":
			for _, f := range entry.Filters {
				expanded := renderOverlayTemplate(f, row)
				filters = append(filters, expanded)
			}
		default:
			fn, ok := LookupPreset(typeName)
			if !ok || fn == nil {
				continue
			}
			filters = append(filters, fn(entry.Options, row, clipDuration)...)
		}
	}
	return filters
}

func presetSongInfo(opts map[string]string, row csvplan.Row, clipDuration float64) []string {
	font := defaultFont()
	titleFontPattern := optStr(opts, "title_font", font)
	artistFontPattern := optStr(opts, "artist_font", font)
	numberFontPattern := optStr(opts, "number_font", font+":Bold")
	// Legacy "font" option overrides all three if set
	if overrideFont := optStr(opts, "font", ""); overrideFont != "" {
		titleFontPattern = overrideFont
		artistFontPattern = overrideFont
		numberFontPattern = overrideFont
	}
	// Resolve fontconfig patterns to file paths for reliable weight selection.
	titleFontFile := fontFilePath(titleFontPattern)
	artistFontFile := fontFilePath(artistFontPattern)
	numberFontFile := fontFilePath(numberFontPattern)
	color := optStr(opts, "color", "white")
	outlineColor := optStr(opts, "outline_color", "black")
	outlineWidth := optInt(opts, "outline_width", 2)
	titleSize := optInt(opts, "title_size", 64)
	artistSize := optInt(opts, "artist_size", 32)
	_ = optInt(opts, "artist_letter_spacing", 0) // reserved for future use
	numberSize := optInt(opts, "number_size", 140)
	numberOutlineWidth := optInt(opts, "number_outline_width", 8)
	showNumber := optBool(opts, "show_number", true)
	infoDuration := optFloat(opts, "info_duration", 4.0)
	fadeDuration := optFloat(opts, "fade_duration", 0.5)
	bottomMargin := optInt(opts, "bottom_margin", 40)

	var filters []string

	// Title overlay: bottom-left, positioned above artist line
	// Artist sits at bottom_margin, title sits above artist
	titleText := renderOverlayTemplate("{title}", row)
	titleText = strings.TrimSpace(titleText)
	if titleText != "" {
		// Position title so its bottom edge is just above the artist line
		titleY := fmt.Sprintf("h-text_h-%d-%d", bottomMargin, artistSize+8)
		filters = append(filters, buildDrawText(drawTextOptions{
			Text:         titleText,
			Start:        0,
			End:          infoDuration,
			FadeIn:       fadeDuration,
			FadeOut:      fadeDuration,
			FontSize:     titleSize,
			FontFile:     titleFontFile,
			FontColor:    color,
			OutlineColor: outlineColor,
			OutlineWidth: outlineWidth,
			XExpr:        "40",
			YExpr:        titleY,
		}))
	}

	// Artist overlay: bottom-left, ALL CAPS, bottom-aligned with number badge
	artistText := renderOverlayTemplate("{artist}", row)
	artistText = strings.ToUpper(strings.TrimSpace(artistText))
	if artistText != "" {
		artistY := fmt.Sprintf("h-text_h-%d", bottomMargin)
		filters = append(filters, buildDrawText(drawTextOptions{
			Text:          artistText,
			Start:         0,
			End:           infoDuration,
			FadeIn:        fadeDuration,
			FadeOut:       fadeDuration,
			FontSize:      artistSize,
			FontFile:      artistFontFile,
			FontColor:     color,
			OutlineColor:  outlineColor,
			OutlineWidth:  max(outlineWidth-1, 1),
			XExpr:         "40",
			YExpr:         artistY,
		}))
	}

	// Credit overlay: bottom-left, appears at end of clip, fade in/out
	creditPrefix := optStr(opts, "credit_prefix", "Credit:")
	nameText := renderOverlayTemplate("{name}", row)
	nameText = strings.TrimSpace(nameText)
	if nameText != "" {
		creditSize := optInt(opts, "credit_size", artistSize)
		creditDuration := optFloat(opts, "credit_duration", infoDuration)
		creditStart := clipDuration - creditDuration
		if creditStart < 0 {
			creditStart = 0
		}
		creditText := creditPrefix + " " + nameText
		creditY := fmt.Sprintf("h-text_h-%d", bottomMargin)
		filters = append(filters, buildDrawText(drawTextOptions{
			Text:         creditText,
			Start:        creditStart,
			End:          clipDuration,
			FadeIn:       fadeDuration,
			FadeOut:      fadeDuration,
			FontSize:     creditSize,
			FontFile:     artistFontFile,
			FontColor:    color,
			OutlineColor: outlineColor,
			OutlineWidth: max(outlineWidth-1, 1),
			XExpr:        "40",
			YExpr:        creditY,
		}))
	}

	// Number badge: bottom-right, persistent, bottom-aligned with artist.
	// Two-layer rendering: thick black outline underneath, then white fill on top.
	// This produces the heavy, high-contrast badge seen in reference designs.
	if showNumber {
		numberText := renderOverlayTemplate("{index}", row)
		numberText = strings.TrimSpace(numberText)
		if numberText != "" {
			numberY := fmt.Sprintf("h-text_h-%d", bottomMargin)
			// Layer 1: thick black outline
			filters = append(filters, buildDrawText(drawTextOptions{
				Text:         numberText,
				Start:        0,
				End:          clipDuration,
				FontSize:     numberSize,
				FontFile:     numberFontFile,
				FontColor:    outlineColor,
				OutlineColor: outlineColor,
				OutlineWidth: numberOutlineWidth,
				XExpr:        "w-text_w-40",
				YExpr:        numberY,
				Persistent:   true,
			}))
			// Layer 2: white fill with thin outline for crispness
			filters = append(filters, buildDrawText(drawTextOptions{
				Text:         numberText,
				Start:        0,
				End:          clipDuration,
				FontSize:     numberSize,
				FontFile:     numberFontFile,
				FontColor:    color,
				OutlineColor: outlineColor,
				OutlineWidth: 2,
				XExpr:        "w-text_w-40",
				YExpr:        numberY,
				Persistent:   true,
			}))
		}
	}

	return filters
}

func presetDrink(opts map[string]string, row csvplan.Row, clipDuration float64) []string {
	fontPattern := optStr(opts, "font", defaultFont()+":Bold")
	fontFile := fontFilePath(fontPattern)
	text := optStr(opts, "text", "Drink!")
	color := optStr(opts, "color", "white")
	outlineColor := optStr(opts, "outline_color", "black")
	outlineWidth := optInt(opts, "outline_width", 4)
	shadowColor := optStr(opts, "shadow_color", "yellow")
	shadowOffsetX := optInt(opts, "shadow_offset_x", 3)
	shadowOffsetY := optInt(opts, "shadow_offset_y", 3)
	size := optInt(opts, "size", 120)

	var filters []string

	// Shadow layer
	shadowX := fmt.Sprintf("(w-text_w)/2+%d", shadowOffsetX)
	shadowY := fmt.Sprintf("(h-text_h)/2+%d", shadowOffsetY)
	filters = append(filters, buildDrawText(drawTextOptions{
		Text:         text,
		Start:        0,
		End:          clipDuration,
		FontSize:     size,
		FontFile:     fontFile,
		FontColor:    shadowColor,
		OutlineColor: shadowColor + "@0",
		OutlineWidth: 0,
		XExpr:        shadowX,
		YExpr:        shadowY,
		Persistent:   true,
	}))

	// Text layer
	filters = append(filters, buildDrawText(drawTextOptions{
		Text:         text,
		Start:        0,
		End:          clipDuration,
		FontSize:     size,
		FontFile:     fontFile,
		FontColor:    color,
		OutlineColor: outlineColor,
		OutlineWidth: outlineWidth,
		XExpr:        "(w-text_w)/2",
		YExpr:        "(h-text_h)/2",
		Persistent:   true,
	}))

	return filters
}

func momentsSongInfo(opts map[string]string, row csvplan.Row, clipDuration float64) []OverlayMoment {
	infoDuration := optFloat(opts, "info_duration", 4.0)
	fadeDuration := optFloat(opts, "fade_duration", 0.5)
	creditDuration := optFloat(opts, "credit_duration", infoDuration)

	var moments []OverlayMoment

	// Title/artist: visible from 0 to infoDuration, midpoint after fade-in
	titleMid := (fadeDuration + infoDuration) / 2
	if titleMid > clipDuration {
		titleMid = clipDuration / 2
	}

	titleText := strings.TrimSpace(renderOverlayTemplate("{title}", row))
	if titleText != "" {
		moments = append(moments, OverlayMoment{Name: "title", SampleTime: titleMid})
	}

	artistText := strings.TrimSpace(renderOverlayTemplate("{artist}", row))
	if artistText != "" {
		moments = append(moments, OverlayMoment{Name: "artist", SampleTime: titleMid})
	}

	// Credit: visible from (clipDuration - creditDuration) to clipDuration
	nameText := strings.TrimSpace(renderOverlayTemplate("{name}", row))
	if nameText != "" {
		creditStart := clipDuration - creditDuration
		if creditStart < 0 {
			creditStart = 0
		}
		creditMid := (creditStart + fadeDuration + clipDuration) / 2
		if creditMid >= clipDuration {
			creditMid = clipDuration - fadeDuration
		}
		moments = append(moments, OverlayMoment{Name: "credit", SampleTime: creditMid})
	}

	// Number: persistent, sample at midpoint of clip
	if optBool(opts, "show_number", true) {
		moments = append(moments, OverlayMoment{Name: "number", SampleTime: clipDuration / 2})
	}

	return moments
}

func momentsDrink(opts map[string]string, _ csvplan.Row, clipDuration float64) []OverlayMoment {
	return []OverlayMoment{
		{Name: "drink", SampleTime: clipDuration / 2},
	}
}

func optStr(opts map[string]string, key, fallback string) string {
	if v, ok := opts[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func optInt(opts map[string]string, key string, fallback int) int {
	if v, ok := opts[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func optFloat(opts map[string]string, key string, fallback float64) float64 {
	if v, ok := opts[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func optBool(opts map[string]string, key string, fallback bool) bool {
	if v, ok := opts[key]; ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}

// fontFilePath resolves a fontconfig pattern to an absolute font file path.
func fontFilePath(pattern string) string {
	out, err := exec.Command("fc-match", "--format=%{file}", pattern).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fontAvailable checks whether a font family is known to fontconfig.
func fontAvailable(family string) bool {
	out, err := exec.Command("fc-match", "--format=%{family}", family).Output()
	if err != nil {
		return false
	}
	matched := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.Contains(matched, strings.ToLower(family))
}

var (
	resolvedDefaultFont     string
	resolvedDefaultFontOnce sync.Once
)

// defaultFont returns "Oswald" if installed, otherwise "Futura".
func defaultFont() string {
	resolvedDefaultFontOnce.Do(func() {
		if fontAvailable("Oswald") {
			resolvedDefaultFont = "Oswald"
		} else {
			resolvedDefaultFont = "Futura"
		}
	})
	return resolvedDefaultFont
}
