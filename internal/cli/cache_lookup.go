package cli

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"powerhour/internal/cache"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

func resolveEntryForRow(pp paths.ProjectPaths, idx *cache.Index, row csvplan.Row) (cache.Entry, bool, error) {
	if idx == nil {
		return cache.Entry{}, false, fmt.Errorf("row %03d %q: cache index is nil", row.Index, row.Title)
	}

	link := strings.TrimSpace(row.Link)
	if link == "" {
		return cache.Entry{}, false, fmt.Errorf("row %03d missing link; update the plan and re-run", row.Index)
	}

	if parsed, err := url.Parse(link); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		key, exists := idx.LookupLink(link)
		if !exists {
			return cache.Entry{}, false, nil
		}
		entry, ok := idx.GetByIdentifier(key)
		if !ok || strings.TrimSpace(entry.CachedPath) == "" {
			return cache.Entry{}, false, nil
		}
		return entry, true, nil
	}

	path := link
	if !filepath.IsAbs(path) {
		path = filepath.Join(pp.Root, link)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return cache.Entry{}, false, fmt.Errorf("row %03d %q: resolve source path: %w", row.Index, row.Title, err)
	}

	entry, ok := idx.GetByIdentifier(abs)
	if !ok || strings.TrimSpace(entry.CachedPath) == "" {
		return cache.Entry{}, false, nil
	}

	return entry, true, nil
}
