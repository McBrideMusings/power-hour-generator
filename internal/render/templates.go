package render

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

// SegmentBaseName renders a segment's output filename (without extension)
// from the template. The result is guaranteed to identify the row, not just
// its position: see withRowIdentity.
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
	return withRowIdentity(base, template, seg)
}

// withRowIdentity appends the row id when the template rendered a name that
// owes nothing to the row itself.
//
// A collection with no title or name column — an interstitials plan, say —
// renders $INDEX_PAD3_$SAFE_TITLE down to "001", a name made entirely of
// playback position. Two such rows are then distinguished only by where they
// happen to sit in the order, so reordering silently repoints a row at
// another row's file, and anything matching segments by anything other than
// exact position matches all of them equally.
//
// Whether the row contributed is decided by rendering the template a second
// time against a blank row: an identical result means every character of the
// name came from somewhere other than the row.
func withRowIdentity(base, template string, seg Segment) string {
	id := strings.TrimSpace(seg.Clip.Row.RowID)
	if id == "" {
		return base
	}
	blank := seg
	blank.Clip.Row = csvplan.Row{Index: seg.Clip.Row.Index}
	blank.Clip.MediaPath = ""
	if sanitizeSegment(applySegmentTemplate(template, segmentTemplateValues(blank))) != base {
		return base
	}
	return base + "_" + id
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
	// EffectiveRow substitutes the playback position for the plan row index,
	// so $INDEX and friends name where the clip plays, not which line of the
	// plan file it came from.
	row := EffectiveRow(clip)
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

// ValidSegmentTokens returns the list of statically-known $TOKEN names
// available in segment templates. Dynamic tokens from CSV CustomFields
// are not included since they vary per plan file.
func ValidSegmentTokens() []string {
	return []string{
		"INDEX", "INDEX_PAD2", "INDEX_PAD3", "INDEX_PAD4", "INDEX_RAW", "ROW_ID",
		"TITLE", "ARTIST", "NAME", "START", "DURATION",
		"SAFE_TITLE", "SAFE_ARTIST", "SAFE_NAME",
		"PLAN_TITLE", "PLAN_ARTIST", "PLAN_NAME", "PLAN_START", "PLAN_DURATION",
		"CLIP_TYPE", "CLIP_INDEX", "CLIP_INDEX_RAW",
		"SEQUENCE", "SEQUENCE_RAW",
		"SOURCE_KIND", "SOURCE_PATH", "SAFE_SOURCE_PATH",
		"ID", "SAFE_ID",
		"SOURCE",
		"SOURCE_BASENAME", "SAFE_SOURCE_BASENAME",
		"CACHE_BASENAME", "SAFE_CACHE_BASENAME",
	}
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

// SegmentNamePattern splits the basename tmpl produces for seg into the parts
// that do not depend on the clip's playback position and the part that does.
// varies is false when the template embeds no position at all, in which case
// prefix and suffix are meaningless.
//
// It exists so a caller can find a segment that was rendered while the row sat
// at a different position in the playback order: the file is still on disk
// under its old number, and prefix+suffix identify it. Rather than parse the
// template, this renders the name at several positions and keeps only what
// none of them moved — so it stays correct for any template, including one
// that repeats the position or omits it.
func SegmentNamePattern(tmpl string, seg Segment) (prefix, suffix string, varies bool) {
	// The probes must disagree in every digit column a padded number can use.
	// Two adjacent values are not enough: $INDEX_PAD3 renders 1 and 2 as
	// "001"/"002", whose shared "00" prefix would then exclude position 10.
	probes := []int{1, 2, 10, 20, 100, 200}

	names := make([]string, 0, len(probes))
	for _, p := range probes {
		probe := seg
		probe.Clip.PlaybackPosition = p
		names = append(names, SegmentBaseName(tmpl, probe))
	}

	same := true
	for _, n := range names[1:] {
		if n != names[0] {
			same = false
			break
		}
	}
	if same {
		return "", "", false
	}

	prefix, suffix = names[0], names[0]
	for _, n := range names[1:] {
		prefix = commonPrefix(prefix, n)
		suffix = commonSuffix(suffix, n)
	}
	// A short name could let the two halves overlap and double-count bytes.
	if len(prefix)+len(suffix) > len(names[0]) {
		suffix = suffix[len(prefix)+len(suffix)-len(names[0]):]
	}
	return prefix, suffix, true
}

func commonPrefix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return a[:i]
}

func commonSuffix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return a[len(a)-i:]
}
