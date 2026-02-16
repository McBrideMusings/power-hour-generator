package render

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"powerhour/internal/project"
)

func SegmentBaseName(template string, seg Segment) string {
	template = strings.TrimSpace(template)
	values := segmentTemplateValues(seg)
	if template == "" {
		return sanitizeSegment(fallbackSegmentBase(seg.Clip))
	}
	rendered := applySegmentTemplate(template, values)
	base := sanitizeSegment(rendered)
	if base == "" {
		return sanitizeSegment(fallbackSegmentBase(seg.Clip))
	}
	return base
}

func fallbackSegmentBase(clip project.Clip) string {
	row := clip.Row
	name := safeFileSlug(row.Title)
	if name == "" {
		name = safeFileSlug(row.Name)
	}
	if name == "" && clip.SourceKind == project.SourceKindMedia && strings.TrimSpace(clip.MediaPath) != "" {
		base := strings.TrimSuffix(filepath.Base(clip.MediaPath), filepath.Ext(clip.MediaPath))
		name = safeFileSlug(base)
	}
	if name == "" {
		name = fmt.Sprintf("clip_%03d", clip.TypeIndex)
	}
	index := clip.TypeIndex
	if index <= 0 {
		index = clip.Sequence
	}
	return fmt.Sprintf("%s_%03d_%s", clip.ClipType, index, name)
}

func segmentTemplateValues(seg Segment) map[string]string {
	clip := seg.Clip
	row := clip.Row
	entry := seg.Entry

	duration := ""
	durationSeconds := clip.DurationSeconds
	if durationSeconds <= 0 {
		durationSeconds = row.DurationSeconds
	}
	if durationSeconds > 0 {
		duration = strconv.Itoa(durationSeconds)
	}

	start := strings.TrimSpace(row.StartRaw)
	if start == "" && row.Start > 0 {
		start = row.Start.String()
	}

	typeIndex := clip.TypeIndex
	if typeIndex <= 0 {
		typeIndex = row.Index
	}

	indexValue := row.Index
	if indexValue <= 0 {
		indexValue = typeIndex
	}
	if indexValue <= 0 {
		indexValue = clip.Sequence
	}

	values := map[string]string{
		"INDEX":      fmt.Sprintf("%03d", indexValue),
		"INDEX_PAD2": fmt.Sprintf("%02d", indexValue),
		"INDEX_PAD3": fmt.Sprintf("%03d", indexValue),
		"INDEX_PAD4": fmt.Sprintf("%04d", indexValue),
		"INDEX_RAW":  strconv.Itoa(indexValue),
		"ROW_ID":     strconv.Itoa(indexValue),

		"TITLE":    sanitizeSegment(row.Title),
		"ARTIST":   sanitizeSegment(row.Artist),
		"NAME":     sanitizeSegment(row.Name),
		"START":    sanitizeSegment(start),
		"DURATION": sanitizeSegment(duration),

		"SAFE_TITLE":  safeFileSlug(row.Title),
		"SAFE_ARTIST": safeFileSlug(row.Artist),
		"SAFE_NAME":   safeFileSlug(row.Name),

		"PLAN_TITLE":    sanitizeSegment(row.Title),
		"PLAN_ARTIST":   sanitizeSegment(row.Artist),
		"PLAN_NAME":     sanitizeSegment(row.Name),
		"PLAN_START":    sanitizeSegment(start),
		"PLAN_DURATION": sanitizeSegment(duration),

		"CLIP_TYPE":        sanitizeSegment(string(clip.ClipType)),
		"CLIP_INDEX":       fmt.Sprintf("%03d", typeIndex),
		"CLIP_INDEX_RAW":   strconv.Itoa(typeIndex),
		"SEQUENCE":         fmt.Sprintf("%03d", clip.Sequence),
		"SEQUENCE_RAW":     strconv.Itoa(clip.Sequence),
		"SOURCE_KIND":      sanitizeSegment(string(clip.SourceKind)),
		"SOURCE_PATH":      sanitizeSegment(seg.SourcePath),
		"SAFE_SOURCE_PATH": safeFileSlug(seg.SourcePath),
	}

	if entry.Key != "" {
		values["ID"] = sanitizeSegment(entry.Key)
		values["SAFE_ID"] = safeFileSlug(entry.Key)
	}

	if entry.Source != "" {
		values["SOURCE"] = sanitizeSegment(entry.Source)
	}

	if entry.CachedPath != "" {
		base := strings.TrimSuffix(filepath.Base(entry.CachedPath), filepath.Ext(entry.CachedPath))
		values["SOURCE_BASENAME"] = sanitizeSegment(base)
		values["SAFE_SOURCE_BASENAME"] = safeFileSlug(base)
	}

	if seg.CachedPath != "" {
		base := strings.TrimSuffix(filepath.Base(seg.CachedPath), filepath.Ext(seg.CachedPath))
		values["CACHE_BASENAME"] = sanitizeSegment(base)
		values["SAFE_CACHE_BASENAME"] = safeFileSlug(base)
	}

	// Add custom fields from Row.CustomFields
	if row.CustomFields != nil {
		for key, value := range row.CustomFields {
			// Add both raw and safe versions of custom fields
			upperKey := strings.ToUpper(key)
			values[upperKey] = sanitizeSegment(value)
			values["SAFE_"+upperKey] = safeFileSlug(value)
		}
	}

	return values
}

func applySegmentTemplate(template string, values map[string]string) string {
	var builder strings.Builder
	for i := 0; i < len(template); {
		ch := template[i]
		if ch != '$' {
			builder.WriteByte(ch)
			i++
			continue
		}

		if i+1 < len(template) && template[i+1] == '$' {
			builder.WriteByte('$')
			i += 2
			continue
		}

		j := i + 1
		for j < len(template) {
			c := template[j]
			switch {
			case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
				j++
				continue
			case c == '_':
				if j+1 < len(template) {
					next := template[j+1]
					if (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') {
						j++
						continue
					}
				}
				fallthrough
			default:
				break
			}
			break
		}

		if j == i+1 {
			builder.WriteByte('$')
			i++
			continue
		}

		token := template[i+1 : j]
		if val, ok := values[token]; ok {
			builder.WriteString(val)
		}
		i = j
	}
	return builder.String()
}

func sanitizeSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		case r == '-' || r == '.':
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	result := builder.String()
	result = strings.Trim(result, "_.-")
	if len(result) > 150 {
		result = result[:150]
	}
	return result
}
