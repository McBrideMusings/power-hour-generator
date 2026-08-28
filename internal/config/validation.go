package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"powerhour/pkg/csvplan"
)

// ValidationResult captures a single validation finding.
type ValidationResult struct {
	Level   string `json:"level"` // "error" or "warning"
	Message string `json:"message"`
}

// KnownOverlayTypes is the set of built-in overlay preset type names.
var KnownOverlayTypes = map[string]bool{
	"song-info": true,
	"drink":     true,
	"custom":    true,
	"none":      true,
}

// ValidateStrict runs all strict validations against the config and returns
// structured results. knownSegmentTokens is the set of statically-known
// $TOKEN names for segment templates (pass render.ValidSegmentTokens()).
func (c Config) ValidateStrict(projectRoot string, knownSegmentTokens []string) []ValidationResult {
	var results []ValidationResult
	results = append(results, c.validateExternalFiles(projectRoot)...)
	results = append(results, c.validateOverlayEntries()...)
	results = append(results, c.validateCacheConfig()...)
	results = append(results, c.validatePlanPaths(projectRoot)...)
	results = append(results, c.validateDisplayTemplates(projectRoot)...)
	results = append(results, c.validateSegmentTemplate(knownSegmentTokens)...)
	results = append(results, c.validateTimeline(projectRoot)...)
	return results
}

// knownFieldMapKeys is the set of field_map keys actually consumed by the
// cache lookup (internal/tui/dashboard/song_lookup.go). Derived from
// DefaultCollectionFieldMap so the two lists cannot drift — a key added to
// the default map automatically becomes a legal field_map key here too.
var knownFieldMapKeys = func() map[string]bool {
	out := make(map[string]bool)
	for key := range DefaultCollectionFieldMap() {
		out[key] = true
	}
	return out
}()

// knownFieldMapKeyList is the sorted, comma-joined rendering of
// knownFieldMapKeys, precomputed for validation messages.
var knownFieldMapKeyList = strings.Join(sortedKeys(knownFieldMapKeys), ", ")

var knownCacheFields = map[string]bool{
	"title":       true,
	"artist":      true,
	"album":       true,
	"track":       true,
	"uploader":    true,
	"channel":     true,
	"upload_date": true,
	"description": true,
	"source":      true,
	"links":       true,
	"identifier":  true,
	"id":          true,
	"extractor":   true,
	"cached_path": true,
	"duration":    true,
	"disk_usage":  true,
}

func (c Config) validateExternalFiles(projectRoot string) []ValidationResult {
	var results []ValidationResult
	for _, path := range c.CollectionFiles {
		resolved := path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(projectRoot, resolved)
		}
		if _, err := os.Stat(resolved); err != nil {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("collection file %q not found", path),
			})
		}
	}
	return results
}

func (c Config) validateOverlayEntries() []ValidationResult {
	var results []ValidationResult
	for name, coll := range c.Collections {
		for i, entry := range coll.Overlays {
			typeName := strings.TrimSpace(entry.Type)
			if typeName == "" {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("collection %q: overlay[%d] missing type", name, i),
				})
				continue
			}
			if !KnownOverlayTypes[typeName] {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("collection %q: overlay[%d] unknown type %q", name, i, typeName),
				})
				continue
			}
			if typeName == "custom" && len(entry.Filters) == 0 {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("collection %q: overlay[%d] type \"custom\" requires filters", name, i),
				})
			}
			if typeName != "custom" && len(entry.Filters) > 0 {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("collection %q: overlay[%d] type %q does not accept filters", name, i, typeName),
				})
			}
		}
		if coll.Fade < 0 || coll.FadeIn < 0 || coll.FadeOut < 0 {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("collection %q: fade values must be >= 0", name),
			})
		}
	}
	return results
}

func (c Config) validateCacheConfig() []ValidationResult {
	var results []ValidationResult

	validateFields := func(context string, fields []string) {
		for _, field := range fields {
			field = strings.TrimSpace(strings.ToLower(field))
			if field == "" {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("%s: cache field name cannot be empty", context),
				})
				continue
			}
			if !knownCacheFields[field] {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("%s: unknown cache field %q", context, field),
				})
			}
		}
	}

	validateFields("cache.view.columns", c.Cache.View.Columns)
	validateFields("cache.ytdlp.search_fields", c.Cache.Ytdlp.SearchFields)
	for name, coll := range c.Collections {
		for key, fields := range coll.FieldMap {
			normalizedKey := strings.TrimSpace(strings.ToLower(key))
			if normalizedKey == "" || !knownFieldMapKeys[normalizedKey] {
				results = append(results, ValidationResult{
					Level:   "warning",
					Message: fmt.Sprintf("collection %q: field_map key %q is not used (known keys: %s)", name, key, knownFieldMapKeyList),
				})
			}
			validateFields(fmt.Sprintf("collections.%s.field_map.%s", name, key), fields)
		}
	}

	return results
}

// sortedKeys returns the keys of a bool-valued map in sorted order, for
// building deterministic message strings out of set-typed data.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c Config) validatePlanPaths(projectRoot string) []ValidationResult {
	var results []ValidationResult
	for name, coll := range c.Collections {
		if file := strings.TrimSpace(coll.File); file != "" {
			resolved := file
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(projectRoot, resolved)
			}
			if _, err := os.Stat(resolved); err != nil {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("collection %q: file %q not found", name, file),
				})
			}
			continue
		}

		plan := strings.TrimSpace(coll.Plan)
		if plan == "" {
			continue
		}
		resolved := plan
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(projectRoot, resolved)
		}
		if _, err := os.Stat(resolved); err != nil {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("collection %q: plan file %q not found", name, plan),
			})
		}
	}
	return results
}

// validateDisplayTemplates errors when a collection's display template
// references a column the collection never declares (and has no defaults
// value for). Collections using file: instead of plan:, or whose plan file
// is missing/unparseable, are skipped silently — validatePlanPaths already
// reports a missing plan, and double-erroring would make validate noisy.
func (c Config) validateDisplayTemplates(projectRoot string) []ValidationResult {
	var results []ValidationResult
	for name, coll := range c.Collections {
		tmpl := strings.TrimSpace(coll.Display)
		if tmpl == "" {
			continue
		}
		if strings.TrimSpace(coll.File) != "" {
			continue
		}
		plan := strings.TrimSpace(coll.Plan)
		if plan == "" {
			continue
		}
		resolved := plan
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(projectRoot, resolved)
		}

		known := map[string]bool{"index": true}
		switch strings.ToLower(filepath.Ext(resolved)) {
		case ".yaml", ".yml":
			result, err := csvplan.LoadCollectionYAML(resolved, csvplan.CollectionOptions{
				LinkHeader:  coll.LinkHeader,
				StartHeader: coll.StartHeader,
			})
			if err != nil && result.Columns == nil && len(result.Defaults) == 0 {
				continue
			}
			for _, col := range result.Columns {
				known[strings.ToLower(strings.TrimSpace(col))] = true
			}
			for key := range result.Defaults {
				known[strings.ToLower(strings.TrimSpace(key))] = true
			}
		case ".csv", ".tsv":
			headers, _, err := csvplan.ReadHeaders(resolved)
			if err != nil {
				continue
			}
			for _, h := range headers {
				known[strings.ToLower(strings.TrimSpace(h))] = true
			}
		default:
			continue
		}

		for _, tok := range extractBraceTokens(tmpl) {
			normalized := strings.ToLower(tok)
			if !known[normalized] {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("collection %q: display template references unknown column %q", name, tok),
				})
			}
		}
	}
	return results
}

func (c Config) validateSegmentTemplate(knownTokens []string) []ValidationResult {
	tmpl := strings.TrimSpace(c.Outputs.SegmentTemplate)
	if tmpl == "" {
		return nil
	}

	known := make(map[string]bool, len(knownTokens))
	for _, t := range knownTokens {
		known[t] = true
	}

	tokens := extractTemplateTokens(tmpl)
	var results []ValidationResult
	for _, tok := range tokens {
		if !known[tok] {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("segment template contains unknown token $%s (known tokens: %s)", tok, strings.Join(knownTokens, ", ")),
			})
		}
	}
	return results
}

func (c Config) validateTimeline(projectRoot string) []ValidationResult {
	var results []ValidationResult
	for i, entry := range c.Timeline.Sequence {
		hasCollection := strings.TrimSpace(entry.Collection) != ""
		hasFile := strings.TrimSpace(entry.File) != ""

		if hasCollection && hasFile {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("timeline sequence[%d]: collection and file are mutually exclusive", i),
			})
			continue
		}
		if !hasCollection && !hasFile {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("timeline sequence[%d]: collection name or file is required", i),
			})
			continue
		}

		// Inline file entry: slice and interleave are not valid; file must exist.
		if hasFile {
			if entry.Fade < 0 || entry.FadeIn < 0 || entry.FadeOut < 0 {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (file %q): fade values must be >= 0", i, entry.File),
				})
			}
			if strings.TrimSpace(entry.Slice) != "" {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (file %q): slice is not valid for file entries", i, entry.File),
				})
			}
			if entry.Interleave != nil {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (file %q): interleave is not valid for file entries", i, entry.File),
				})
			}
			resolved := entry.File
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(projectRoot, resolved)
			}
			if _, err := os.Stat(resolved); os.IsNotExist(err) {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (file %q): file not found", i, entry.File),
				})
			}
			continue
		}

		// Collection entry validation.
		if _, ok := c.Collections[entry.Collection]; !ok {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("timeline sequence[%d]: collection %q does not exist", i, entry.Collection),
			})
		}
		if _, err := ParseTimelineSlice(entry.Slice); err != nil {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("timeline sequence[%d] (%q): invalid slice: %v", i, entry.Collection, err),
			})
		}
		if entry.Fade < 0 || entry.FadeIn < 0 || entry.FadeOut < 0 {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("timeline sequence[%d] (%q): fade values must be >= 0", i, entry.Collection),
			})
		}
		if entry.Interleave != nil {
			if strings.TrimSpace(entry.Interleave.Collection) == "" {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (%q): interleave collection name is required", i, entry.Collection),
				})
			} else if _, ok := c.Collections[entry.Interleave.Collection]; !ok {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (%q): interleave collection %q does not exist", i, entry.Collection, entry.Interleave.Collection),
				})
			}
			if entry.Interleave.Every <= 0 {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (%q): interleave every must be > 0", i, entry.Collection),
				})
			}
			switch entry.Interleave.Placement {
			case "", "between", "after", "before", "around":
				// valid
			default:
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (%q): interleave placement %q is not valid (use between, after, before, or around)", i, entry.Collection, entry.Interleave.Placement),
				})
			}
		}
	}
	return results
}

// extractTemplateTokens parses $TOKEN patterns from a template string,
// using the same token-boundary rules as the render template engine.
func extractTemplateTokens(template string) []string {
	var tokens []string
	for i := 0; i < len(template); {
		ch := template[i]
		if ch != '$' {
			i++
			continue
		}
		if i+1 < len(template) && template[i+1] == '$' {
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
		if j > i+1 {
			tokens = append(tokens, template[i+1:j])
		}
		i = j
	}
	return tokens
}

// extractBraceTokens parses {name} patterns from a display template string,
// the {token} sibling of extractTemplateTokens' $TOKEN parsing.
func extractBraceTokens(template string) []string {
	var tokens []string
	for i := 0; i < len(template); i++ {
		if template[i] != '{' {
			continue
		}
		end := strings.IndexByte(template[i+1:], '}')
		if end < 0 {
			break
		}
		name := strings.TrimSpace(template[i+1 : i+1+end])
		if name != "" {
			tokens = append(tokens, name)
		}
		i += end + 1
	}
	return tokens
}
