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
	results = append(results, c.validateTimeline()...)
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
	}
	return results
}

func (c Config) validatePlanPaths(projectRoot string) []ValidationResult {
	var results []ValidationResult
	for name, coll := range c.Collections {
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

func (c Config) validateTimeline() []ValidationResult {
	var results []ValidationResult
	for i, entry := range c.Timeline.Sequence {
		if strings.TrimSpace(entry.Collection) == "" {
			results = append(results, ValidationResult{
				Level:   "error",
				Message: fmt.Sprintf("timeline sequence[%d]: collection name is required", i),
			})
			continue
		}
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
