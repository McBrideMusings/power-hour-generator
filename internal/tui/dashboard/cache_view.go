package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/tui"
)

// cacheHeaderLines is the cache view's own chrome on top of
// dashboardChromeLines: the section label (1 line), column headers (1 line),
// and the unified help row (1 line) = 3 lines of fixed non-data content.
const cacheHeaderLines = 3

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

	// Add-slot state (set by model when modeAddCache is active). Mirrors
	// timelineView's addFocus/addBuffer/addCursor/addHint — a cache add-slot
	// entry is a URL, a bare YouTube ID, or a local file path, so there's no
	// suggestions machinery to carry alongside it.
	addFocus  bool
	addBuffer string
	addCursor int
	addHint   string

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
	// dashboardChromeLines (outer chrome: header/blanks/status/footer) +
	// cacheHeaderLines (section label, column header, help row) = 8 lines
	// of fixed chrome reserved; the remaining termHeight is available for data rows.
	h := max(v.termHeight-dashboardChromeLines-cacheHeaderLines, 1)
	return h
}

// renderHelpRow returns the single inline help row for the cache view,
// picked from the same priority ladder as the collection view so every
// table in the dashboard shares one help-row shape.
func (v cacheView) renderHelpRow() string {
	entries := v.entries()

	var sources []helpRowSource
	sources = append(sources, helpRowSource{v.confirmDelete, confirmStyle})

	if v.editing && v.cursor >= 0 && v.cursor < len(entries) {
		parts := []string{"Edit · " + v.currentEditField(),
			"Enter save", "Esc cancel", "Tab next field"}
		if hint := strings.TrimSpace(v.editHint); hint != "" {
			parts = append(parts, hint)
		}
		sources = append(sources, helpRowSource{strings.Join(parts, " · "), faint})
	}

	if v.cursor >= 0 && v.cursor < len(entries) {
		sources = append(sources, helpRowSource{inlineRowNote(v.rowStatus[entries[v.cursor].Identifier], 0), editStyle})
	}

	defaultText := "a add · e edit · d doctor all · D doctor problematic · f toggle filter · x remove"
	if len(entries) == 0 {
		if v.showAll {
			defaultText = "no cached sources — run 'fetch' to populate"
		} else {
			defaultText = "no cached sources for this project — press f to show all"
		}
	}

	// The focused add-slot can render more than helpRowText's single line
	// (input + trailing hint), so it can't be a plain source — it's the
	// fallback, exactly like collectionView.renderHelpRow. confirmDelete,
	// editing, and addFocus are set by mutually exclusive interaction modes
	// (modeConfirmDelete / modeCacheInlineEdit / modeAddCache), so whenever
	// addFocus is true every source above is already empty and this always
	// wins the fallback slot rather than needing to jump the ladder.
	fallback := func() string { return helpRowText(defaultText, faint, v.termWidth) }
	if v.addFocus {
		fallback = v.renderAddSlot
	}

	return resolveHelpRow(v.termWidth, fallback, sources...)
}

// renderAddSlot renders the focused cache add-slot footer: the rendered
// input with its cursor, plus a trailing hint on the same line — either the
// classification hint (URL / YouTube ID / local file) or the default keys
// hint when the buffer is empty.
func (v cacheView) renderAddSlot() string {
	cursor := cursorStyle.Render("▸ ")
	keysHint := "Enter add · Esc cancel · URL, YouTube ID, or local file path"
	if hint := strings.TrimSpace(v.addHint); hint != "" {
		keysHint = hint
	}

	// renderEditField appends a trailing cursor glyph (one visual column)
	// when the cursor sits at or past the end of the buffer; account for
	// that here so the budget matches what it actually renders.
	cursorGlyphWidth := 0
	if v.addCursor >= len(v.addBuffer) {
		cursorGlyphWidth = 1
	}

	avail := max(v.termWidth-len(helpRowPrefix), 12)
	remaining := avail - runewidth.StringWidth(v.addBuffer) - cursorGlyphWidth

	const gapWidth = 2 // "  " separator before the trailing hint
	keysWidth := gapWidth + runewidth.StringWidth(keysHint)

	fittedKeys := ""
	switch {
	case remaining <= 0:
		// No room for the hint; the buffer alone fills (or exceeds) the budget.
	case keysWidth <= remaining:
		fittedKeys = keysHint
	default:
		fittedKeys = tui.TruncateWithEllipsis(keysHint, remaining-gapWidth)
	}

	line := cursor + "+ " + renderEditField(v.addBuffer, v.addCursor)
	if fittedKeys != "" {
		line += "  " + faint.Render(fittedKeys)
	}
	return line
}

func (v cacheView) currentEditField() string {
	if v.editFieldIdx < 0 || v.editFieldIdx >= len(v.columns) {
		return ""
	}
	return v.columns[v.editFieldIdx]
}

// computeCacheColumnWidths mirrors collectionView.computeColumnWidths: each
// column gets only the width its actual data needs (header vs. widest cell,
// capped at columnMaxWidth), instead of splitting the table evenly — a short
// column no longer steals space a long one (e.g. LINK) needs. Leftover width
// goes to columns whose content was truncated by the cap; under pressure,
// columns shrink proportionally to their slack above columnMinWidth. The
// last column is always FILE, rendered from CachedPath rather than Values.
func computeCacheColumnWidths(headers []string, entries []cacheEntry, tableWidth, baseWidth int) []int {
	n := len(headers)
	if n == 0 {
		return nil
	}

	cellValue := func(row cacheEntry, col int) string {
		if col == n-1 {
			return filepath.Base(row.CachedPath)
		}
		if col < len(row.Values) {
			return row.Values[col]
		}
		return ""
	}

	uncapped := make([]int, n)
	natural := make([]int, n)
	total := 0
	for i, h := range headers {
		w := runewidth.StringWidth(h)
		for _, e := range entries {
			if vw := runewidth.StringWidth(cellValue(e, i)); vw > w {
				w = vw
			}
		}
		uncapped[i] = w
		if w > columnMaxWidth {
			w = columnMaxWidth
		}
		if w < columnMinWidth {
			w = columnMinWidth
		}
		natural[i] = w
		total += w
	}

	available := tableWidth - baseWidth
	widths := make([]int, n)
	copy(widths, natural)

	if total <= available {
		leftover := available - total
		if leftover <= 0 {
			return widths
		}
		var growable []int
		for i := range headers {
			if uncapped[i] > natural[i] {
				growable = append(growable, i)
			}
		}
		if len(growable) == 0 {
			return widths
		}
		share := leftover / len(growable)
		rem := leftover % len(growable)
		for k, i := range growable {
			widths[i] += share
			if k < rem {
				widths[i]++
			}
		}
		return widths
	}

	deficit := total - available
	totalSlack := 0
	slack := make([]int, n)
	for i, w := range widths {
		s := w - columnMinWidth
		if s > 0 {
			slack[i] = s
			totalSlack += s
		}
	}
	if totalSlack == 0 {
		return widths
	}
	for i := range widths {
		cut := deficit * slack[i] / totalSlack
		widths[i] -= cut
		if widths[i] < columnMinWidth {
			widths[i] = columnMinWidth
		}
	}
	return widths
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

	tableWidth := max(v.termWidth-20, baseWidth)

	headers := make([]string, dataColCount)
	for i, col := range v.columns {
		headers[i] = strings.ToUpper(col)
	}
	headers[dataColCount-1] = "FILE"
	widths := computeCacheColumnWidths(headers, entries, tableWidth, baseWidth)

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
	endRow := min(startRow+visible, len(entries))
	if endRow < len(entries) {
		visible--
		visible = max(visible, 0)
		endRow = min(startRow+visible, len(entries))
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

		if isEditRow {
			gutter = editRowBgOnly.Width(gutterWidth).Render(gutter)
		}

		cells := make([]string, 0, dataColCount)
		// skipUntil is the last column swallowed by an inline-edit cell that
		// grew past its own column. Columns after it still render.
		skipUntil := -1
		for j, val := range e.Values {
			if j <= skipUntil {
				continue
			}
			if isEditRow && j == v.editFieldIdx {
				_, cellWidth, covered := editOverflowExtent(widths, j, gutterWidth, gutterGapWidth, columnGapWidth, v.termWidth, runewidth.StringWidth(v.editValue))
				cells = append(cells, renderEditCell(v.editValue, v.editCursor, cellWidth))
				skipUntil = j + covered
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
		// The trailing file column is rendered outside the value loop, so it
		// needs the same skip check: an edit cell wide enough to swallow it
		// has already drawn over its space.
		if skipUntil < dataColCount-1 {
			fileStyle := faint
			if isEditRow {
				fileStyle = editRowStyle
			}
			cells = append(cells, renderCell(filepath.Base(e.CachedPath), widths[dataColCount-1], fileStyle))
		}

		b.WriteString(gutter)
		gap := strings.Repeat(" ", gutterGapWidth)
		if isEditRow {
			gap = editRowBgOnly.Render(gap)
		}
		b.WriteString(gap)
		row := renderRow(cells...)
		if isEditRow {
			// Re-render the cells with background-tinted separators
			joined := ""
			for k, cell := range cells {
				if k > 0 {
					joined += editRowBgOnly.Render("  ")
				}
				joined += cell
			}
			row = joined
		}
		b.WriteString(row)
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
