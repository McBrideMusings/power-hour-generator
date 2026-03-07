package render

import (
	"fmt"
	"strconv"
	"strings"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

// PresetFunc generates drawtext filter strings for an overlay preset.
type PresetFunc func(opts map[string]string, row csvplan.Row, clipDuration float64) []string

var presetRegistry = map[string]PresetFunc{
	"song-info": presetSongInfo,
	"drink":     presetDrink,
	"custom":    nil, // handled separately via raw filters
	"none":      nil, // no overlays
}

// LookupPreset returns the preset function for a given type name.
func LookupPreset(typeName string) (PresetFunc, bool) {
	fn, ok := presetRegistry[typeName]
	return fn, ok
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
	font := optStr(opts, "font", "Impact")
	color := optStr(opts, "color", "white")
	outlineColor := optStr(opts, "outline_color", "black")
	outlineWidth := optInt(opts, "outline_width", 3)
	titleSize := optInt(opts, "title_size", 64)
	artistSize := optInt(opts, "artist_size", 42)
	numberSize := optInt(opts, "number_size", 140)
	showNumber := optBool(opts, "show_number", true)
	infoDuration := optFloat(opts, "info_duration", 4.0)
	fadeDuration := optFloat(opts, "fade_duration", 0.5)

	var filters []string

	// Title overlay: bottom-left, fade in/out
	titleText := renderOverlayTemplate("{title}", row)
	titleText = strings.TrimSpace(titleText)
	if titleText != "" {
		filters = append(filters, buildDrawText(drawTextOptions{
			Text:         titleText,
			Start:        0,
			End:          infoDuration,
			FadeIn:       fadeDuration,
			FadeOut:      fadeDuration,
			FontSize:     titleSize,
			Font:         font,
			FontColor:    color,
			OutlineColor: outlineColor,
			OutlineWidth: outlineWidth,
			XExpr:        "40",
			YExpr:        "h-text_h-220",
		}))
	}

	// Artist overlay: bottom-left below title, ALL CAPS
	artistText := renderOverlayTemplate("{artist}", row)
	artistText = strings.ToUpper(strings.TrimSpace(artistText))
	if artistText != "" {
		filters = append(filters, buildDrawText(drawTextOptions{
			Text:         artistText,
			Start:        0,
			End:          infoDuration,
			FadeIn:       fadeDuration,
			FadeOut:      fadeDuration,
			FontSize:     artistSize,
			Font:         font,
			FontColor:    color,
			OutlineColor: outlineColor,
			OutlineWidth: max(outlineWidth-1, 1),
			XExpr:        "40",
			YExpr:        "h-text_h-160",
		}))
	}

	// Number badge: bottom-right, persistent
	if showNumber {
		numberText := renderOverlayTemplate("{index}", row)
		numberText = strings.TrimSpace(numberText)
		if numberText != "" {
			numberOutline := optInt(opts, "number_outline_width", 4)
			filters = append(filters, buildDrawText(drawTextOptions{
				Text:         numberText,
				Start:        0,
				End:          clipDuration,
				FontSize:     numberSize,
				Font:         font,
				FontColor:    color,
				OutlineColor: outlineColor,
				OutlineWidth: numberOutline,
				XExpr:        "w-text_w-40",
				YExpr:        "h-text_h-40",
				Persistent:   true,
			}))
		}
	}

	return filters
}

func presetDrink(opts map[string]string, row csvplan.Row, clipDuration float64) []string {
	font := optStr(opts, "font", "Impact")
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
		Font:         font,
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
		Font:         font,
		FontColor:    color,
		OutlineColor: outlineColor,
		OutlineWidth: outlineWidth,
		XExpr:        "(w-text_w)/2",
		YExpr:        "(h-text_h)/2",
		Persistent:   true,
	}))

	return filters
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
