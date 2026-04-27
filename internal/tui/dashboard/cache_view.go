package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"powerhour/internal/cache"
	"powerhour/internal/config"
)

// cacheEntry is a flattened cache entry for display.
type cacheEntry struct {
	Identifier string
	Source     string
	CachedPath string
	// Values is a parallel slice to the configured cache view columns.
	Values []string
}

// Label returns the first non-empty configured column value, falling back to
// the cached filename. Used for confirm prompts and status messages that need
// a human-readable identifier.
func (e cacheEntry) Label() string {
	for _, v := range e.Values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return filepath.Base(e.CachedPath)
}

// cacheView shows cached source files, filtered to this project by default.
type cacheView struct {
	columns         []string
	allEntries      []cacheEntry
	filteredEntries []cacheEntry
	showAll         bool // false = filtered to project, true = all cached
	activity        string
	rowStatus       map[string]string
	rowStatusUntil  map[string]int
	cursor          int
	scrollTop       int

	// Inline confirm prompt rendered beneath the cursor row (set by model
	// when modeConfirmDelete is active). Empty = no pending confirm.
	confirmDelete string

	// Inline edit state (set by model when modeCacheInlineEdit is active).
	editing      bool
	editFieldIdx int
	editValue    string
	editCursor   int
	editHint     string

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

func newCacheView(cfg config.Config, idx *cache.Index, collectionLinks map[string]string) cacheView {
	urls := buildCollectionURLs(collectionLinks)
	columns := append([]string(nil), cfg.Cache.View.Columns...)
	if len(columns) == 0 {
		columns = []string{"title", "artist"}
	}

	var allEntries, filteredEntries []cacheEntry

	if idx != nil {
		for _, entry := range idx.Entries {
			if entry.CachedPath == "" {
				continue
			}

			// An entry belongs to this project if any of its source identifiers
			// are referenced by a collection row. Used only to populate
			// filteredEntries — never surfaced as a column.
			projectReferenced := false
			if entry.Source != "" {
				if _, ok := urls[entry.Source]; ok {
					projectReferenced = true
				}
			}
			if !projectReferenced {
				for _, link := range entry.Links {
					if _, ok := urls[link]; ok {
						projectReferenced = true
						break
					}
				}
			}

			values := make([]string, len(columns))
			for i, field := range columns {
				values[i] = firstConfiguredCacheValue(entry, []string{field})
			}

			ce := cacheEntry{
				Identifier: entry.Identifier,
				Source:     entry.Source,
				CachedPath: entry.CachedPath,
				Values:     values,
			}

			allEntries = append(allEntries, ce)
			if projectReferenced {
				filteredEntries = append(filteredEntries, ce)
			}
		}
	}

	return cacheView{
		columns:         columns,
		allEntries:      allEntries,
		filteredEntries: filteredEntries,
		rowStatus:       make(map[string]string),
		rowStatusUntil:  make(map[string]int),
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
	// -10 reserves one line for the unified help row at the bottom.
	h := v.termHeight - 10
	if h < 1 {
		h = 1
	}
	return h
}

// renderHelpRow returns the single inline help row for the cache view,
// picked from the same priority ladder as the collection view so every
// table in the dashboard shares one help-row shape.
func (v cacheView) renderHelpRow() string {
	entries := v.entries()

	if v.confirmDelete != "" {
		return helpRowText(v.confirmDelete, confirmStyle, v.termWidth)
	}

	if v.editing && v.cursor >= 0 && v.cursor < len(entries) {
		parts := []string{"Edit · " + v.currentEditField(),
			"Enter save", "Esc cancel", "Tab next field"}
		if hint := strings.TrimSpace(v.editHint); hint != "" {
			parts = append(parts, hint)
		}
		return helpRowText(strings.Join(parts, " · "), faint, v.termWidth)
	}

	if v.cursor >= 0 && v.cursor < len(entries) {
		if note := inlineRowNote(v.rowStatus[entries[v.cursor].Identifier], 0); note != "" {
			return helpRowText(note, editStyle, v.termWidth)
		}
	}

	if len(entries) == 0 {
		if v.showAll {
			return helpRowText("no cached sources — run 'fetch' to populate", faint, v.termWidth)
		}
		return helpRowText("no cached sources for this project — press f to show all", faint, v.termWidth)
	}

	return helpRowText("e edit · D doctor problematic · f toggle filter · x remove", faint, v.termWidth)
}

func (v cacheView) currentEditField() string {
	if v.editFieldIdx < 0 || v.editFieldIdx >= len(v.columns) {
		return ""
	}
	return v.columns[v.editFieldIdx]
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

	// Column widths: fixed gutter (cursor + idx + status), then flex-distribute
	// remaining terminal width across the configured data columns + FILE.
	idxWidth := 4
	statusWidth := 5
	gutterWidth := idxWidth + statusWidth + 1
	columnGapWidth := 2
	gutterGapWidth := 4

	dataColCount := len(v.columns) + 1 // +1 for FILE
	totalGaps := 0
	if dataColCount > 0 {
		totalGaps += gutterGapWidth
		totalGaps += (dataColCount - 1) * columnGapWidth
	}
	baseWidth := gutterWidth + totalGaps

	tableWidth := v.termWidth - 20
	if tableWidth < baseWidth {
		tableWidth = baseWidth
	}

	flexWidth := 10
	if dataColCount > 0 && tableWidth > baseWidth+dataColCount*5 {
		flexWidth = (tableWidth - baseWidth) / dataColCount
	}
	widths := make([]int, dataColCount)
	headers := make([]string, dataColCount)
	for i, col := range v.columns {
		headers[i] = strings.ToUpper(col)
	}
	headers[dataColCount-1] = "FILE"
	for i := range widths {
		widths[i] = flexWidth
		if widths[i] < len(headers[i]) {
			widths[i] = len(headers[i])
		}
	}

	// Header row.
	headerParts := make([]string, 0, dataColCount)
	for i, h := range headers {
		headerParts = append(headerParts, renderCell(h, widths[i], colHeader))
	}

	b.WriteString(renderCell("#", gutterWidth, colHeader))
	if len(headerParts) > 0 {
		b.WriteString(strings.Repeat(" ", gutterGapWidth))
		b.WriteString(renderRow(headerParts...))
	}
	b.WriteByte('\n')

	visible := v.visibleRowCount()
	startRow := v.scrollTop

	// Reserve a line for the up indicator if scrolled, and a line for the
	// down indicator if there will be entries below — so that indicators
	// don't push content past the footer.
	if startRow > 0 {
		visible--
	}
	endRow := startRow + visible
	if endRow > len(entries) {
		endRow = len(entries)
	}
	if endRow < len(entries) {
		visible--
		if visible < 0 {
			visible = 0
		}
		endRow = startRow + visible
		if endRow > len(entries) {
			endRow = len(entries)
		}
	}

	if startRow > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  ↑ %d more above", startRow)))
		b.WriteByte('\n')
	}

	plain := lipgloss.NewStyle()
	for i := startRow; i < endRow; i++ {
		e := entries[i]

		// Gutter: cursor + index + status (compact). idx and status are fixed
		// widths and never exceed them, so ANSI wrapping the bare 2-char /
		// N-char strings is safe without pre-padding.
		cursor := "  "
		idx := fmt.Sprintf("%02d", i+1)
		if i == v.cursor {
			cursor = cursorStyle.Render("▸ ")
			idx = cursorStyle.Render(idx)
		} else {
			idx = faint.Render(idx)
		}
		rawStatus := strings.TrimSpace(v.rowStatus[e.Identifier])
		statusDisplay := rawStatus
		if statusDisplay == "" {
			statusDisplay = "-"
		}
		// Use lipgloss Width for visual-column-accurate padding (fmt %-*s is
		// byte-based and breaks on multi-byte characters like em dash).
		statusCell := faint.Width(statusWidth).Render(truncateCollectionValue(statusDisplay, statusWidth))
		gutter := fmt.Sprintf("%s%s %s", cursor, idx, statusCell)

		isEditRow := v.editing && i == v.cursor

		cells := make([]string, 0, dataColCount)
		for j, val := range e.Values {
			if isEditRow && j == v.editFieldIdx {
				cells = append(cells, renderCell(renderCursorField(v.editValue, v.editCursor), widths[j], editStyle))
				continue
			}
			style := faint
			if j == 0 && !isEditRow {
				// First configured column renders plain like the collection view's title.
				style = plain
			}
			if isEditRow {
				style = editRowStyle
			}
			display := val
			if strings.TrimSpace(display) == "" {
				display = "—"
				if !isEditRow {
					style = faint
				}
			}
			cells = append(cells, renderCell(display, widths[j], style))
		}
		fileStyle := faint
		if isEditRow {
			fileStyle = editRowStyle
		}
		cells = append(cells, renderCell(filepath.Base(e.CachedPath), widths[dataColCount-1], fileStyle))

		b.WriteString(gutter)
		b.WriteString(strings.Repeat(" ", gutterGapWidth))
		b.WriteString(renderRow(cells...))
		b.WriteByte('\n')
	}

	if endRow < len(entries) {
		b.WriteString(faint.Render(fmt.Sprintf("  ↓ %d more below", len(entries)-endRow)))
		b.WriteByte('\n')
	}

	b.WriteString(v.renderHelpRow())
	b.WriteByte('\n')

	return b.String()
}
