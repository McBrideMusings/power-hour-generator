package project

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

// RenderRowTemplate expands a {token} template against a row's standard
// fields and custom fields. This is the single {token} rendering engine used
// by both overlay text (render.renderOverlayTemplate delegates here) and
// collection display labels — kept in this package because render imports
// project, not the reverse.
func RenderRowTemplate(tmpl string, row csvplan.Row) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}

	replacements := []string{
		"{title}", row.Title,
		"{artist}", row.Artist,
		"{name}", row.Name,
		"{index}", strconv.Itoa(row.Index),
	}

	if row.CustomFields != nil {
		for key, value := range row.CustomFields {
			replacements = append(replacements, "{"+key+"}", value)
			lowerKey := strings.ToLower(key)
			if lowerKey != key {
				replacements = append(replacements, "{"+lowerKey+"}", value)
			}
		}
	}

	replacer := strings.NewReplacer(replacements...)
	return strings.TrimSpace(replacer.Replace(tmpl))
}

// releaseTagPattern matches common scene/release-group noise tokens found in
// downloaded media filenames (resolution, codec, audio format, source, and
// bracketed language/group tags), each preceded by a separator.
var releaseTagPattern = regexp.MustCompile(`(?i)[.\s_-]+(` +
	`\d{3,4}p|` +
	`web[-.]?dl|webrip|bluray|blu-ray|hdtv|dvdrip|brrip|hdrip|` +
	`x264|x265|h264|h265|hevc|avc|` +
	`aac|ac3|eac3|dts(-hd)?|flac|mp3|` +
	`5\.1|7\.1|2\.0|` +
	`proper|repack|extended|remastered|uncut|` +
	`\[[^\]]*\]` +
	`)\b.*$`)

// FallbackLabel derives a presentational label from a link/filename when no
// display template is configured or the template renders empty. It strips
// the directory and extension, then strips trailing release-tag noise
// (resolution, codec, audio format, bracketed group tags), leaving the
// human-readable title remainder. If stripping leaves nothing usable, it
// falls back to the plain basename.
func FallbackLabel(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	base := filepath.Base(link)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	cleaned := releaseTagPattern.ReplaceAllString(stem, "")
	cleaned = strings.ReplaceAll(cleaned, ".", " ")
	cleaned = strings.ReplaceAll(cleaned, "_", " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = strings.Trim(cleaned, " -–—")

	if cleaned == "" {
		return base
	}
	return cleaned
}

// CollectionRowLabel returns the presentational label for a single collection
// row: the collection's display template when it renders non-empty,
// otherwise a cleaned fallback derived from the row's link.
func CollectionRowLabel(cc config.CollectionConfig, row csvplan.CollectionRow) string {
	if tmpl := strings.TrimSpace(cc.Display); tmpl != "" {
		if s := strings.TrimSpace(RenderRowTemplate(tmpl, row.ToRow())); s != "" {
			return s
		}
	}
	return FallbackLabel(row.Link)
}

// TimelineEntryLabel returns the presentational label for a resolved
// timeline entry: an inline file entry's cleaned basename, a collection
// row's display-driven label, or the bare collection name when the row is
// out of range.
func TimelineEntryLabel(e TimelineEntry, collections map[string]Collection) string {
	if e.SourceFile != "" {
		return FallbackLabel(e.SourceFile)
	}
	if c, ok := collections[e.Collection]; ok && e.Index >= 1 && e.Index <= len(c.Rows) {
		return CollectionRowLabel(c.Config, c.Rows[e.Index-1])
	}
	return e.Collection
}
