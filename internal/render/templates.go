package render

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"powerhour/pkg/csvplan"
)

func SegmentBaseName(template string, seg Segment) string {
	template = strings.TrimSpace(template)
	values := segmentTemplateValues(seg)
	if template == "" {
		return sanitizeSegment(fallbackSegmentBase(seg.Row))
	}
	rendered := applySegmentTemplate(template, values)
	base := sanitizeSegment(rendered)
	if base == "" {
		return sanitizeSegment(fallbackSegmentBase(seg.Row))
	}
	return base
}

func fallbackSegmentBase(row csvplan.Row) string {
	name := safeFileSlug(row.Title)
	if name == "" {
		name = fmt.Sprintf("segment_%03d", row.Index)
	}
	return fmt.Sprintf("%03d_%s", row.Index, name)
}

func segmentTemplateValues(seg Segment) map[string]string {
	row := seg.Row
	entry := seg.Entry

	duration := ""
	if row.DurationSeconds > 0 {
		duration = strconv.Itoa(row.DurationSeconds)
	}

	start := strings.TrimSpace(row.StartRaw)
	if start == "" && row.Start > 0 {
		start = row.Start.String()
	}

	values := map[string]string{
		"INDEX":      fmt.Sprintf("%03d", row.Index),
		"INDEX_PAD2": fmt.Sprintf("%02d", row.Index),
		"INDEX_PAD3": fmt.Sprintf("%03d", row.Index),
		"INDEX_PAD4": fmt.Sprintf("%04d", row.Index),
		"INDEX_RAW":  strconv.Itoa(row.Index),
		"ROW_ID":     strconv.Itoa(row.Index),

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
