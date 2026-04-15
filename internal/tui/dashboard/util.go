package dashboard

import (
	"strings"

	"powerhour/internal/tui"
)

// isURL returns true if the string looks like a URL (http, https, or youtube shortlink).
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "youtu")
}

func truncateCollectionValue(value string, max int) string {
	value = strings.TrimSpace(value)
	if value == "" || max <= 0 {
		return ""
	}
	if len(value) <= max {
		return value
	}
	if !looksLikeFilesystemPath(value) || isURL(value) {
		return tui.TruncateWithEllipsis(value, max)
	}

	lastSep := strings.LastIndexAny(value, `/\`)
	if lastSep < 0 || lastSep >= len(value)-1 {
		return tui.TruncateWithEllipsis(value, max)
	}

	dir := value[:lastSep+1]
	base := value[lastSep+1:]
	if len(base) >= max {
		return tui.TruncateWithEllipsis(base, max)
	}

	remaining := max - len(base)
	if len(dir) <= remaining {
		return dir + base
	}
	if remaining <= 3 {
		return tui.TruncateWithEllipsis(base, max)
	}

	suffix := trailingPathSuffix(dir, remaining-3)
	return "..." + suffix + base
}

func looksLikeFilesystemPath(s string) bool {
	if strings.ContainsAny(s, `/\`) {
		return true
	}
	return strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

func trailingPathSuffix(dir string, max int) string {
	if max <= 0 || dir == "" {
		return ""
	}
	if len(dir) <= max {
		return dir
	}

	parts := strings.FieldsFunc(dir, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		if max > len(dir) {
			return dir
		}
		return dir[len(dir)-max:]
	}

	suffix := string(dir[len(dir)-1])
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := string(dir[len(dir)-1]) + parts[i] + suffix
		if len(candidate) > max {
			break
		}
		suffix = candidate
	}
	if len(suffix) > max {
		return suffix[len(suffix)-max:]
	}
	return suffix
}
