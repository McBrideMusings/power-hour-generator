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
	font := defaultFont()
	titleFont := optStr(opts, "title_font", font+"\\:Bold")
	artistFont := optStr(opts, "artist_font", font)
	numberFont := optStr(opts, "number_font", font+"\\:Bold")
	// Legacy "font" option overrides all three if set
	if overrideFont := optStr(opts, "font", ""); overrideFont != "" {
		titleFont = overrideFont
		artistFont = overrideFont
		numberFont = overrideFont
	}
	color := optStr(opts, "color", "white")
	outlineColor := optStr(opts, "outline_color", "black")
	outlineWidth := optInt(opts, "outline_width", 2)
	titleSize := optInt(opts, "title_size", 64)
	artistSize := optInt(opts, "artist_size", 32)
	_ = optInt(opts, "artist_letter_spacing", 0) // reserved for future use
	numberSize := optInt(opts, "number_size", 140)
	numberOutlineWidth := optInt(opts, "number_outline_width", 5)
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
			Font:         titleFont,
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
			Font:          artistFont,
			FontColor:     color,
			OutlineColor:  outlineColor,
			OutlineWidth:  max(outlineWidth-1, 1),
			XExpr:         "40",
			YExpr:         artistY,
		}))
	}

	// "Added by" overlay: bottom-left, appears at end of clip, fade in/out
	nameText := renderOverlayTemplate("{name}", row)
	nameText = strings.TrimSpace(nameText)
	if nameText != "" {
		addedBySize := optInt(opts, "added_by_size", artistSize)
		addedByDuration := optFloat(opts, "added_by_duration", infoDuration)
		addedByStart := clipDuration - addedByDuration
		if addedByStart < 0 {
			addedByStart = 0
		}
		addedByText := "Added by: " + nameText
		addedByY := fmt.Sprintf("h-text_h-%d", bottomMargin)
		filters = append(filters, buildDrawText(drawTextOptions{
			Text:         addedByText,
			Start:        addedByStart,
			End:          clipDuration,
			FadeIn:       fadeDuration,
			FadeOut:      fadeDuration,
			FontSize:     addedBySize,
			Font:         artistFont,
			FontColor:    color,
			OutlineColor: outlineColor,
			OutlineWidth: max(outlineWidth-1, 1),
			XExpr:        "40",
			YExpr:        addedByY,
		}))
	}

	// Number badge: bottom-right, persistent, bottom-aligned with artist
	if showNumber {
		numberText := renderOverlayTemplate("{index}", row)
		numberText = strings.TrimSpace(numberText)
		if numberText != "" {
			numberY := fmt.Sprintf("h-text_h-%d", bottomMargin)
			filters = append(filters, buildDrawText(drawTextOptions{
				Text:         numberText,
				Start:        0,
				End:          clipDuration,
				FontSize:     numberSize,
				Font:         numberFont,
				FontColor:    color,
				OutlineColor: outlineColor,
				OutlineWidth: numberOutlineWidth,
				XExpr:        "w-text_w-40",
				YExpr:        numberY,
				Persistent:   true,
			}))
		}
	}

	return filters
}

func presetDrink(opts map[string]string, row csvplan.Row, clipDuration float64) []string {
	font := optStr(opts, "font", defaultFont()+"\\:Bold")
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
