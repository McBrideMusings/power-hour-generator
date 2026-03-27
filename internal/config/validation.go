package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	results = append(results, c.validatePlanPaths(projectRoot)...)
	results = append(results, c.validateSegmentTemplate(knownSegmentTokens)...)
	results = append(results, c.validateTimeline(projectRoot)...)
	return results
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

		// Inline file entry: count and interleave are not valid; file must exist.
		if hasFile {
			if entry.Fade < 0 || entry.FadeIn < 0 || entry.FadeOut < 0 {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (file %q): fade values must be >= 0", i, entry.File),
				})
			}
			if entry.Count > 0 {
				results = append(results, ValidationResult{
					Level:   "error",
					Message: fmt.Sprintf("timeline sequence[%d] (file %q): count is not valid for file entries", i, entry.File),
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
		if entry.Count < 0 {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("timeline sequence[%d] (%q): count must be >= 0", i, entry.Collection),
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
