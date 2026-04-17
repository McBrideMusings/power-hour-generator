package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/tui"
	"powerhour/pkg/csvplan"
)

// rowState describes the cache/render state of a collection row.
type rowState int

const (
	rowRendered    rowState = iota // segment exists on disk
	rowNotRendered                 // cached but segment not rendered
	rowNotCached                   // source not in cache
)

var rowStateStyles = map[rowState]lipgloss.Style{
	rowRendered:    lipgloss.NewStyle(),                                   // default
	rowNotRendered: lipgloss.NewStyle().Foreground(lipgloss.Color("214")), // amber
	rowNotCached:   lipgloss.NewStyle().Foreground(lipgloss.Color("9")),   // bright red
}

// collectionColumn describes a dynamic column in the collection table.
type collectionColumn struct {
	header string
	field  string // custom fields key
	width  int    // 0 = flex
	fixed  bool   // true = fixed width, false = flex
}

// collectionView holds the state for a single collection's plan data table.
type collectionView struct {
	name           string
	planPath       string
	rows           []csvplan.CollectionRow
	collCfg        project.Collection
	columns        []collectionColumn
	states         []rowState // per-row cache/render state
	rowStatus      map[int]string
	rowStatusUntil map[int]int
	activity       string
	tick           int
	cursor         int
	scrollTop      int

	// Inline edit state (set by model when modeInlineEdit is active).
	editing      bool
	editFieldIdx int
	editValue    string
	editCursor   int
	editHint     string

	// Add-clip slot state (set by model when modeAddClip is active).
	addFocus       bool
	addBuffer      string
	addCursor      int
	addHint        string
	addSuggestions []songSuggestion
	addSelected    int

	// Inline confirm prompt rendered beneath the cursor row (set by model when
	// modeConfirmDelete is active). Empty string means no pending confirm.
	confirmDelete string

	termWidth  int
	termHeight int
}

// Known fields are shown first when present. Width is computed dynamically for
// all columns so the table stays schema-flexible.
var knownFieldOrder = []struct {
	field string
}{
	{"title"},
	{"artist"},
	{"name"},
	{"start_time"},
	{"duration"},
}

func discoverColumns(rows []csvplan.CollectionRow, declaredColumns []string) []collectionColumn {
	// Gather all field keys that have at least one non-empty value.
	fieldPresent := make(map[string]bool)
	for _, col := range declaredColumns {
		fieldPresent[col] = true
	}
	for _, row := range rows {
		for k, v := range row.CustomFields {
			if strings.TrimSpace(v) != "" {
				fieldPresent[k] = true
			}
		}
	}

	var cols []collectionColumn
	seen := make(map[string]bool)

	// Add known fields first, in order, if present.
	for _, kf := range knownFieldOrder {
		if fieldPresent[kf.field] {
			cols = append(cols, collectionColumn{
				header: strings.ToUpper(kf.field),
				field:  kf.field,
			})
			seen[kf.field] = true
		}
	}

	// Collect and sort remaining fields alphabetically.
	var extras []string
	for k := range fieldPresent {
		if !seen[k] {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)

	for _, k := range extras {
		cols = append(cols, collectionColumn{
			header: strings.ToUpper(k),
			field:  k,
		})
	}

	return cols
}

func newCollectionView(coll project.Collection, pp paths.ProjectPaths, cfg config.Config, idx *cache.Index) collectionView {
	states := computeRowStates(coll, pp, cfg, idx)
	return collectionView{
		name:           coll.Name,
		planPath:       coll.Plan,
		rows:           coll.Rows,
		collCfg:        coll,
		columns:        discoverColumns(coll.Rows, coll.Headers),
		states:         states,
		rowStatus:      make(map[int]string),
		rowStatusUntil: make(map[int]int),
	}
}

func computeRowStates(coll project.Collection, pp paths.ProjectPaths, cfg config.Config, idx *cache.Index) []rowState {
	states := make([]rowState, len(coll.Rows))
	for i, row := range coll.Rows {
		link := strings.TrimSpace(row.Link)
		isURL := isURL(link)

		// Check cache status.
		cached := false
		if isURL {
			if idx != nil {
				_, cached = idx.LookupLink(link)
			}
		} else {
			path := strings.Trim(link, "'\"")
			if !filepath.IsAbs(path) {
				path = filepath.Join(pp.Root, path)
			}
			_, err := os.Stat(path)
			cached = err == nil
		}

		if !cached {
			states[i] = rowNotCached
			continue
		}

		// Check rendered segment.
		segPath := resolveRenderedSegmentPath(pp, cfg, coll.Name, coll, row)
		if _, err := os.Stat(segPath); err == nil {
			states[i] = rowRendered
		} else {
			states[i] = rowNotRendered
		}
	}
	return states
}

func (v collectionView) visibleRowCount() int {
	// -10 instead of -9 to reserve a line for the persistent Add Clip slot.
	h := v.termHeight - 10
	h -= v.addSlotExtraLines()
	if v.cursor >= 0 && v.cursor < len(v.rows) {
		if v.confirmDelete != "" || inlineRowNote(v.rowStatus[v.rows[v.cursor].Index], v.tick) != "" {
			h--
		}
	}
	if h < 1 {
		h = 1
	}
	return h
}

func (v collectionView) addSlotExtraLines() int {
	lines := 0
	if len(v.addSuggestions) > 0 {
		lines += len(v.addSuggestions)
	}
	if strings.TrimSpace(v.addHint) != "" {
		lines++
	}
	return lines
}

func (v collectionView) view() string {
	var b strings.Builder

	// Subheader.
	fadeStr := ""
	if v.collCfg.Config.Fade > 0 {
		fadeStr = fmt.Sprintf(" · fade: %.1f", v.collCfg.Config.Fade)
	}
	header := fmt.Sprintf("%s · %s · %d rows%s",
		strings.ToUpper(v.name), v.planPath, len(v.rows), fadeStr)
	if strings.TrimSpace(v.activity) != "" {
		header += " · " + busySpinner(v.tick) + " " + v.activity
	}
	b.WriteString(sectionLabel.Render(header))
	b.WriteByte('\n')

	// Compute column widths. The left gutter holds the row number plus a compact
	// status token; data columns split the remaining width.
	idxWidth := 4 // # column
	statusWidth := 5
	gutterWidth := idxWidth + statusWidth + 1
	columnGapWidth := 2
	gutterGapWidth := 4
	totalGaps := 0
	if len(v.columns) > 0 {
		totalGaps += gutterGapWidth
		totalGaps += (len(v.columns) - 1) * columnGapWidth
	}
	baseWidth := gutterWidth + totalGaps
	widths := make([]int, len(v.columns))
	flexCount := len(v.columns)

	tableWidth := v.termWidth - 20
	if tableWidth < baseWidth {
		tableWidth = baseWidth
	}

	flexWidth := 10
	if flexCount > 0 && tableWidth > baseWidth+flexCount*5 {
		flexWidth = (tableWidth - baseWidth) / flexCount
	}
	for i, col := range v.columns {
		widths[i] = flexWidth
		if widths[i] < len(col.header) {
			widths[i] = len(col.header)
		}
	}

	// Column headers. The row index/status gutter sits to the left of the data.
	headerParts := make([]string, 0, len(v.columns))
	for i, col := range v.columns {
		headerParts = append(headerParts, colHeader.Render(fmt.Sprintf("%-*s", widths[i], col.header)))
	}
	b.WriteString(colHeader.Render(fmt.Sprintf("%-*s", gutterWidth, "#")))
	if len(headerParts) > 0 {
		b.WriteString(strings.Repeat(" ", gutterGapWidth))
		b.WriteString(strings.Join(headerParts, "  "))
	}
	b.WriteByte('\n')

	// Visible rows.
	visible := v.visibleRowCount()
	startRow := v.scrollTop
	endRow := startRow + visible
	if endRow > len(v.rows) {
		endRow = len(v.rows)
	}

	if startRow > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  ↑ %d more above", startRow)))
		b.WriteByte('\n')
	}

	for i := startRow; i < endRow; i++ {
		row := v.rows[i]
		state := rowRendered
		if i < len(v.states) {
			state = v.states[i]
		}
		stateStyle := rowStateStyles[state]

		cursor := "  "
		idx := fmt.Sprintf("%02d", row.Index)
		if i == v.cursor && !v.addFocus {
			cursor = cursorStyle.Render("▸ ")
			idx = cursorStyle.Render(idx)
		} else if state != rowRendered {
			idx = stateStyle.Render(idx)
		} else {
			idx = faint.Render(idx)
		}

		isEditRow := v.editing && i == v.cursor

		rawStatus := v.rowStatus[row.Index]
		status := compactRowStatus(rawStatus, v.tick)
		gutter := fmt.Sprintf("%s%-*s %-*s", cursor, idxWidth-2, idx, statusWidth, tui.TruncateWithEllipsis(status, statusWidth))
		parts := []string{gutter}
		for j, col := range v.columns {
			val := sanitize(row.CustomFields[col.field])
			w := widths[j]

			// Inline edit: show edit buffer with cursor on the active field.
			if isEditRow && j == v.editFieldIdx {
				display := renderCursorField(v.editValue, v.editCursor)
				display = truncateCollectionValue(display, w)
				parts = append(parts, editStyle.Render(fmt.Sprintf("%-*s", w, display)))
				continue
			}
			// Inline edit: highlight other fields on the edit row.
			if isEditRow {
				val = truncateCollectionValue(val, w)
				parts = append(parts, editRowStyle.Render(fmt.Sprintf("%-*s", w, val)))
				continue
			}

			if state != rowRendered {
				parts = append(parts, stateStyle.Render(fmt.Sprintf("%-*s", w, truncateCollectionValue(val, w))))
			} else if col.field == "title" {
				parts = append(parts, fmt.Sprintf("%-*s", w, truncateCollectionValue(val, w)))
			} else {
				parts = append(parts, faint.Render(fmt.Sprintf("%-*s", w, truncateCollectionValue(val, w))))
			}
		}
		b.WriteString(parts[0])
		if len(parts) > 1 {
			b.WriteString(strings.Repeat(" ", gutterGapWidth))
			b.WriteString(strings.Join(parts[1:], "  "))
		}
		b.WriteByte('\n')
		note := ""
		useConfirmStyle := false
		if i == v.cursor && v.confirmDelete != "" {
			note = v.confirmDelete
			useConfirmStyle = true
		} else if isEditRow {
			if statusNote := inlineRowNote(rawStatus, v.tick); statusNote != "" {
				note = statusNote
			} else {
				note = editContextNote(v, row)
			}
		} else {
			note = inlineRowNote(rawStatus, v.tick)
		}
		if note != "" && i == v.cursor {
			b.WriteString(strings.Repeat(" ", gutterWidth+gutterGapWidth))
			noteWidth := max(12, v.termWidth-gutterWidth-gutterGapWidth-2)
			switch {
			case useConfirmStyle:
				b.WriteString(confirmStyle.Render(tui.TruncateWithEllipsis(note, noteWidth)))
			case isEditRow && !strings.HasPrefix(strings.TrimSpace(rawStatus), "note:") && strings.TrimSpace(rawStatus) != "probing":
				b.WriteString(faint.Render(tui.TruncateWithEllipsis("+ "+note, noteWidth)))
			default:
				b.WriteString(editStyle.Render(tui.TruncateWithEllipsis(note, noteWidth)))
			}
			b.WriteByte('\n')
		}
	}

	if endRow < len(v.rows) {
		b.WriteString(faint.Render(fmt.Sprintf("  ↓ %d more below", len(v.rows)-endRow)))
		b.WriteByte('\n')
	}

	// Persistent "Add Clip" slot pinned below the data rows.
	b.WriteString(v.renderAddSlot())
	b.WriteByte('\n')

	return b.String()
}

func editContextNote(v collectionView, row csvplan.CollectionRow) string {
	field := ""
	if v.editFieldIdx >= 0 && v.editFieldIdx < len(v.columns) {
		field = v.columns[v.editFieldIdx].field
	}
	parts := []string{fmt.Sprintf("Edit mode row %02d", row.Index)}
	if field != "" {
		parts = append(parts, "field "+field)
	}
	if strings.TrimSpace(v.editHint) != "" {
		parts = append(parts, strings.TrimSpace(v.editHint))
	}
	parts = append(parts, "Enter save", "Esc cancel")
	return strings.Join(parts, " · ")
}

// renderAddSlot renders the persistent add-clip row at the bottom of the grid.
// Single-line input is shown verbatim with a faint detection hint (· link / · path);
// multi-line pastes collapse to a chip like `[CSV +52 lines]` so the buffer never
// floods the UI. The user always commits with Enter.
func (v collectionView) renderAddSlot() string {
	cursor := "  "
	marker := faint.Render("+ ")
	if v.addFocus {
		cursor = cursorStyle.Render("▸ ")
		marker = cursorStyle.Render("+ ")
	}

	if !v.addFocus && v.addBuffer == "" {
		return faint.Render("  + press a to add a clip")
	}

	buf := v.addBuffer
	body, detect := classifyAddBuffer(buf)

	renderBody := body
	if v.addFocus && !strings.Contains(body, "\n") {
		renderBody = renderCursorField(body, v.addCursor)
	}
	rendered := editStyle.Render(renderBody)
	if v.addFocus {
		if strings.Contains(body, "\n") {
			rendered += editStyle.Render("█")
		}
	}
	if detect != "" {
		rendered += "  " + faint.Render("· "+detect)
	}
	line := cursor + marker + rendered
	if strings.TrimSpace(v.addHint) == "" && len(v.addSuggestions) == 0 {
		return line
	}
	var b strings.Builder
	b.WriteString(line)
	suggestionWidth := max(12, v.termWidth-10)
	query := strings.TrimSpace(v.addBuffer)
	for i, suggestion := range v.addSuggestions {
		b.WriteByte('\n')
		label := renderSuggestionLabel(suggestion, query, i == v.addSelected)
		b.WriteString(strings.Repeat(" ", 6))
		b.WriteString(tui.TruncateWithEllipsis(label, suggestionWidth))
	}
	if strings.TrimSpace(v.addHint) != "" {
		b.WriteByte('\n')
		noteWidth := max(12, v.termWidth-6)
		b.WriteString(strings.Repeat(" ", 6))
		b.WriteString(faint.Render(tui.TruncateWithEllipsis(v.addHint, noteWidth)))
	}
	return b.String()
}

func renderSuggestionLabel(suggestion songSuggestion, query string, selected bool) string {
	title := strings.TrimSpace(suggestion.Title)
	artist := strings.TrimSpace(suggestion.Artist)
	if title == "" && artist == "" {
		title = strings.TrimSpace(suggestion.Link)
	}
	title = highlightMatch(title, query, selected)
	artist = highlightMatch(artist, query, selected)
	label := title
	if artist != "" {
		label += " - " + artist
	}
	return label
}

func highlightMatch(value, query string, selected bool) string {
	value = strings.TrimSpace(value)
	query = strings.TrimSpace(query)
	if value == "" || query == "" {
		return applySuggestionBaseStyle(value, selected)
	}
	lowerValue := strings.ToLower(value)
	lowerQuery := strings.ToLower(query)
	idx := strings.Index(lowerValue, lowerQuery)
	if idx < 0 {
		return applySuggestionBaseStyle(value, selected)
	}
	end := idx + len(query)
	if idx >= len(value) || end > len(value) {
		return applySuggestionBaseStyle(value, selected)
	}
	base := addSuggestionOtherStyle
	if selected {
		base = addSuggestionActiveStyle
	}
	return base.Render(value[:idx]) + matchStyle.Render(value[idx:end]) + base.Render(value[end:])
}

func applySuggestionBaseStyle(value string, selected bool) string {
	if selected {
		return addSuggestionActiveStyle.Render(value)
	}
	return addSuggestionOtherStyle.Render(value)
}

// classifyAddBuffer returns (displayBody, detectionHint).
// displayBody is what to render in the slot — literal text for single lines,
// a compact chip for multi-line pastes. detectionHint is a short label like
// "link", "path", "CSV row", etc. that sits next to the body in faint.
func classifyAddBuffer(buf string) (string, string) {
	if buf == "" {
		return "", ""
	}
	trimmed := strings.TrimSpace(buf)
	if trimmed == "" {
		return buf, ""
	}

	// Count lines (non-empty after trim).
	lines := strings.Split(trimmed, "\n")
	lineCount := len(lines)

	hasTab := strings.Contains(trimmed, "\t")
	hasComma := strings.Contains(trimmed, ",")
	isYAML := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "columns:") || strings.HasPrefix(trimmed, "rows:")

	// Multi-line paste → chip.
	if lineCount > 1 {
		label := "Pasted"
		switch {
		case isYAML:
			label = "YAML"
		case hasTab:
			label = "TSV"
		case hasComma:
			label = "CSV"
		}
		return fmt.Sprintf("[%s +%d lines]", label, lineCount), fmt.Sprintf("%d lines will be imported", lineCount)
	}

	// Single line — show verbatim with a type hint.
	body := trimmed
	if len(body) > 80 {
		body = body[:77] + "…"
	}

	switch {
	case isURL(trimmed):
		return body, "link"
	case hasTab:
		return body, "TSV row"
	case hasComma:
		return body, "CSV row"
	case strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "~") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../"):
		return body, "path"
	default:
		return body, ""
	}
}

func compactRowStatus(raw string, tick int) string {
	status := strings.TrimSpace(raw)
	switch {
	case status == "":
		return ""
	case strings.HasPrefix(status, "note:"):
		return ""
	case status == "queued":
		return "~"
	case status == "cached":
		return "C"
	case status == "rendered":
		return "OK"
	case status == "error":
		return "X"
	case status == "fetching":
		return "F " + busySpinner(tick)
	case status == "rendering":
		return "R " + busySpinner(tick)
	case status == "probing":
		return "P " + busySpinner(tick)
	case strings.HasPrefix(status, "rendering "):
		pct := strings.TrimSpace(strings.TrimPrefix(status, "rendering "))
		return "R " + pct
	case strings.HasPrefix(status, "fetching "):
		pct := strings.TrimSpace(strings.TrimPrefix(status, "fetching "))
		return "F " + pct
	default:
		return status
	}
}

func inlineRowNote(raw string, tick int) string {
	status := strings.TrimSpace(raw)
	if status == "probing" {
		return busySpinner(tick) + " Probing metadata..."
	}
	if !strings.HasPrefix(status, "note:") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(status, "note:"))
}
