package tools

import (
	"context"
	"fmt"
	"strings"
)

type contextKeyMinimums struct{}

// WithMinimums annotates the context with project-specific minimum version overrides.
func WithMinimums(ctx context.Context, minimums map[string]string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(minimums) == 0 {
		return ctx
	}
	cleaned := make(map[string]string, len(minimums))
	for name, value := range minimums {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		cleaned[strings.ToLower(name)] = trimmed
	}
	if len(cleaned) == 0 {
		return ctx
	}
	return context.WithValue(ctx, contextKeyMinimums{}, cleaned)
}

func minimumOverride(ctx context.Context, tool string) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(contextKeyMinimums{})
	if raw == nil {
		return ""
	}
	overrides, ok := raw.(map[string]string)
	if !ok {
		return ""
	}
	if v, ok := overrides[strings.ToLower(tool)]; ok {
		return v
	}
	return ""
}

func resolveMinimumVersion(ctx context.Context, def ToolDefinition) (string, []string) {
	minimum := strings.TrimSpace(def.MinimumVersion)
	if minimum == "" {
		minimum = def.MinimumVersion
	}

	override := strings.TrimSpace(minimumOverride(ctx, def.Name))
	if override == "" {
		return minimum, nil
	}

	var notes []string
	if strings.EqualFold(override, "latest") {
		spec, ok, err := resolveRelease(ctx, def.Name, "")
		if err != nil {
			notes = append(notes, fmt.Sprintf("latest lookup failed: %v", err))
			return minimum, notes
		}
		if !ok || spec.Version == "" {
			notes = append(notes, "latest release metadata unavailable")
			return minimum, notes
		}
		if meetsMinimum(spec.Version, minimum) {
			if spec.Version != minimum {
				notes = append(notes, fmt.Sprintf("minimum set to latest release %s", spec.Version))
			} else {
				notes = append(notes, fmt.Sprintf("latest release %s matches current minimum", spec.Version))
			}
			return spec.Version, notes
		}
		notes = append(notes, fmt.Sprintf("default minimum %s exceeds latest release %s", minimum, spec.Version))
		return minimum, notes
	}

	if meetsMinimum(override, minimum) {
		if override != minimum {
			notes = append(notes, fmt.Sprintf("minimum overridden by project config (%s)", override))
		}
		return override, notes
	}

	notes = append(notes, fmt.Sprintf("config minimum %s ignored; default minimum %s is higher", override, minimum))
	return minimum, notes
}
