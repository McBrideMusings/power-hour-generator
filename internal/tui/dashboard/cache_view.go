package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"

	"powerhour/internal/cache"
	"powerhour/internal/tui"
)

// cacheEntry is a flattened cache entry for display.
type cacheEntry struct {
	Identifier string
	Title      string
	Artist     string
	Source     string
	CachedPath string
	Collection string // which collection uses this, empty if not referenced
}

// cacheView shows cached source files, filtered to this project by default.
type cacheView struct {
	allEntries      []cacheEntry
	filteredEntries []cacheEntry
	showAll         bool // false = filtered to project, true = all cached
	activity        string
	rowStatus       map[string]string
	cursor          int
	scrollTop       int

	termWidth  int
	termHeight int
}

// collectionURLs maps URL → collection name for entries that were fetched (not local paths).
func buildCollectionURLs(collectionLinks map[string]string) map[string]string {
	urls := make(map[string]string, len(collectionLinks))
	for link, coll := range collectionLinks {
		if isURL(link) {
			urls[link] = coll
		}
	}
	return urls
}

func newCacheView(idx *cache.Index, collectionLinks map[string]string) cacheView {
	urls := buildCollectionURLs(collectionLinks)
	var allEntries, filteredEntries []cacheEntry

	if idx != nil {
		for _, entry := range idx.Entries {
			if entry.CachedPath == "" {
				continue
			}

			// Determine which collection references this entry.
			collName := ""
			if entry.Source != "" {
				if c, ok := urls[entry.Source]; ok {
					collName = c
				}
			}
			for _, link := range entry.Links {
				if c, ok := urls[link]; ok {
					collName = c
					break
				}
			}

			title := entry.Title
			if title == "" {
				title = filepath.Base(entry.CachedPath)
			}

			ce := cacheEntry{
				Identifier: entry.Identifier,
				Title:      title,
				Artist:     entry.Artist,
				Source:     entry.Source,
				CachedPath: entry.CachedPath,
				Collection: collName,
			}

			allEntries = append(allEntries, ce)
			if collName != "" {
				filteredEntries = append(filteredEntries, ce)
			}
		}
	}

	return cacheView{
		allEntries:      allEntries,
		filteredEntries: filteredEntries,
		rowStatus:       make(map[string]string),
	}
}

func (v cacheView) entries() []cacheEntry {
	if v.showAll {
		return v.allEntries
	}
	return v.filteredEntries
}

func (v *cacheView) toggle() {
	v.showAll = !v.showAll
	v.cursor = 0
	v.scrollTop = 0
}

func (v cacheView) visibleRowCount() int {
	h := v.termHeight - 9
	if h < 1 {
		h = 1
	}
	return h
}

func (v cacheView) view() string {
	var b strings.Builder
	entries := v.entries()

	filterLabel := "project only"
	if v.showAll {
		filterLabel = "all cached"
	}
	header := fmt.Sprintf("CACHE · %d sources · %s  [f to toggle]", len(entries), filterLabel)
	if strings.TrimSpace(v.activity) != "" {
		header += " · " + v.activity
	}
	b.WriteString(sectionLabel.Render(header))
	b.WriteByte('\n')

	statusWidth := 10
	fixedWidth := 4 + statusWidth + 14 + 5*2
	flexWidth := 0
	if v.termWidth > fixedWidth+30 {
		flexWidth = (v.termWidth - fixedWidth) / 3
	} else {
		flexWidth = 12
	}

	b.WriteString(colHeader.Render(
		fmt.Sprintf("%-4s  %-*s  %-*s  %-*s  %-14s  %-*s",
			"#", statusWidth, "STATUS", flexWidth, "TITLE", flexWidth, "ARTIST", "COLLECTION", flexWidth, "FILE")))
	b.WriteByte('\n')

	visible := v.visibleRowCount()
	startRow := v.scrollTop
	endRow := startRow + visible
	if endRow > len(entries) {
		endRow = len(entries)
	}

	if startRow > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  ↑ %d more above", startRow)))
		b.WriteByte('\n')
	}

	for i := startRow; i < endRow; i++ {
		e := entries[i]

		cursor := "  "
		idx := fmt.Sprintf("%02d", i+1)
		if i == v.cursor {
			cursor = cursorStyle.Render("▸ ")
			idx = cursorStyle.Render(idx)
		} else {
			idx = faint.Render(idx)
		}

		title := tui.TruncateWithEllipsis(e.Title, flexWidth)
		artist := tui.TruncateWithEllipsis(e.Artist, flexWidth)
		coll := tui.TruncateWithEllipsis(e.Collection, 14)
		if coll == "" {
			coll = faint.Render("—")
		}
		status := tui.TruncateWithEllipsis(v.rowStatus[e.Identifier], statusWidth)
		if status == "" {
			status = faint.Render("—")
		} else {
			status = faint.Render(status)
		}
		file := tui.TruncateWithEllipsis(filepath.Base(e.CachedPath), flexWidth)
		b.WriteString(fmt.Sprintf("%s%s  %-*s  %-*s  %s  %-14s  %s",
			cursor, idx, statusWidth, status, flexWidth, title, faint.Render(fmt.Sprintf("%-*s", flexWidth, artist)), coll, faint.Render(fmt.Sprintf("%-*s", flexWidth, file))))
		b.WriteByte('\n')
	}

	if endRow < len(entries) {
		b.WriteString(faint.Render(fmt.Sprintf("  ↓ %d more below", len(entries)-endRow)))
		b.WriteByte('\n')
	}

	if len(entries) == 0 {
		if v.showAll {
			b.WriteString(faint.Render("  No cached sources. Run 'fetch' to populate."))
		} else {
			b.WriteString(faint.Render("  No cached sources for this project. Press 'f' to show all."))
		}
		b.WriteByte('\n')
	}

	return b.String()
}
