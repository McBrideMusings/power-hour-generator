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
	// -10 reserves one line for the persistent help row at the bottom plus
	// surrounding chrome (headers, section label).
	h := v.termHeight - 10
	// The focused add-slot is the only help row that can exceed a single
	// line: it optionally adds suggestion rows and a dynamic hint line.
	if v.addFocus {
		h -= v.addSlotExtraLines()
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
		headerParts = append(headerParts, renderCell(col.header, widths[i], colHeader))
	}
	b.WriteString(renderCell("#", gutterWidth, colHeader))
	if len(headerParts) > 0 {
		b.WriteString(strings.Repeat(" ", gutterGapWidth))
		b.WriteString(strings.Join(headerParts, "  "))
	}
	b.WriteByte('\n')

	// Visible rows. Reserve lines for scroll indicators before computing
	// endRow so indicators don't push content past the footer.
	visible := v.visibleRowCount()
	startRow := v.scrollTop
	if startRow > 0 {
		visible--
	}
	endRow := startRow + visible
	if endRow > len(v.rows) {
		endRow = len(v.rows)
	}
	if endRow < len(v.rows) {
		visible--
		if visible < 0 {
			visible = 0
		}
		endRow = startRow + visible
		if endRow > len(v.rows) {
			endRow = len(v.rows)
		}
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
		rawIdx := fmt.Sprintf("%-*s", idxWidth-2, fmt.Sprintf("%02d", row.Index))
		var idx string
		if i == v.cursor && !v.addFocus {
			cursor = cursorStyle.Render("▸ ")
			idx = cursorStyle.Render(rawIdx)
		} else if state != rowRendered {
			idx = stateStyle.Render(rawIdx)
		} else {
			idx = faint.Render(rawIdx)
		}

		isEditRow := v.editing && i == v.cursor

		rawStatus := v.rowStatus[row.Index]
		status := compactRowStatus(rawStatus, v.tick)
		gutter := fmt.Sprintf("%s%s %-*s", cursor, idx, statusWidth, tui.TruncateWithEllipsis(status, statusWidth))
		parts := []string{gutter}
		for j, col := range v.columns {
			val := sanitize(row.CustomFields[col.field])
			w := widths[j]

			// Inline edit: show edit buffer with cursor on the active field.
			// The edit cell overflows into adjacent columns: compute X offset of
			// this column and stretch to the terminal right margin, then stop
			// rendering further columns (they fall within the overflow region).
			if isEditRow && j == v.editFieldIdx {
				xOffset := gutterWidth + gutterGapWidth
				for k := 0; k < j; k++ {
					xOffset += widths[k] + columnGapWidth
				}
				overflowWidth := max(w, v.termWidth-xOffset-2)
				parts = append(parts, renderEditCell(v.editValue, v.editCursor, overflowWidth))
				break
			}
			// Inline edit: highlight other fields on the edit row (before the edit field).
			if isEditRow {
				parts = append(parts, renderCell(val, w, editRowStyle))
				continue
			}

			if state != rowRendered {
				parts = append(parts, renderCell(val, w, stateStyle))
			} else if col.field == "title" {
				parts = append(parts, renderCell(val, w, lipgloss.NewStyle()))
			} else {
				parts = append(parts, renderCell(val, w, faint))
			}
		}
		b.WriteString(parts[0])
		if len(parts) > 1 {
			b.WriteString(strings.Repeat(" ", gutterGapWidth))
			b.WriteString(strings.Join(parts[1:], "  "))
		}
		b.WriteByte('\n')
	}

	if endRow < len(v.rows) {
		b.WriteString(faint.Render(fmt.Sprintf("  ↓ %d more below", len(v.rows)-endRow)))
		b.WriteByte('\n')
	}

	// Unified inline help row. All contextual messages (confirm-delete, edit
	// mode, transient notes like "removed row", the focused add slot, and the
	// default "press a to add a clip") render through the same footer element
	// in a fixed priority order. Only one help row is ever visible.
	b.WriteString(v.renderHelpRow())
	b.WriteByte('\n')

	return b.String()
}

func editContextNote(v collectionView, row csvplan.CollectionRow) string {
	header := fmt.Sprintf("Edit row %02d", row.Index)
	if v.editFieldIdx >= 0 && v.editFieldIdx < len(v.columns) {
		header += " · " + v.columns[v.editFieldIdx].field
	}

	parts := []string{header, "Enter save", "Esc cancel", "Tab next field"}
	if hint := strings.TrimSpace(v.editHint); hint != "" {
		parts = append(parts, hint)
	}
	return strings.Join(parts, " · ")
}

// renderHelpRow returns the single inline help row for this view, picked
// from a fixed priority ladder. The highest-priority populated source wins
// and replaces every lower-priority default. The order matches what the
// user is currently doing:
//
//  1. confirm-delete prompt (Y/N)
//  2. inline-edit context (field being edited, keys)
//  3. transient note / status on the cursor row ("removed row", "probing", …)
//  4. focused add-slot (input + keys hint + suggestions)
//  5. default action hint ("press a to add a clip")
//
// Only the add-slot branch can produce multiple lines (input + suggestions +
// dynamic hint); all others render exactly one line via helpRowText.
func (v collectionView) renderHelpRow() string {
	// 1. Confirm-delete.
	if v.confirmDelete != "" {
		return helpRowText(v.confirmDelete, confirmStyle, v.termWidth)
	}

	// 2. Inline-edit context. If the edit row also carries a transient note,
	// the note wins (so "saved" / "probing" are visible during the edit lull
	// between keystrokes).
	if v.editing && v.cursor >= 0 && v.cursor < len(v.rows) {
		row := v.rows[v.cursor]
		rawStatus := v.rowStatus[row.Index]
		if note := inlineRowNote(rawStatus, v.tick); note != "" {
			return helpRowText(note, editStyle, v.termWidth)
		}
		return helpRowText(editContextNote(v, row), faint, v.termWidth)
	}

	// 3. Transient note on the cursor row.
	if v.cursor >= 0 && v.cursor < len(v.rows) {
		row := v.rows[v.cursor]
		if note := inlineRowNote(v.rowStatus[row.Index], v.tick); note != "" {
			return helpRowText(note, editStyle, v.termWidth)
		}
	}

	// 4. Focused add slot.
	if v.addFocus {
		return v.renderAddSlot()
	}

	// 5. Default.
	return helpRowText("press a to add a clip", faint, v.termWidth)
}

// renderAddSlot renders the focused add-clip footer: the rendered input
// with its cursor, a trailing keys hint on the same line, and optional
// suggestions / dynamic hint on subsequent lines. This is the one help-row
// branch that can span multiple lines; it still shares the same column and
// "+ " marker as every other help row via helpRowRaw.
func (v collectionView) renderAddSlot() string {
	cursor := cursorStyle.Render("▸ ")

	buf := v.addBuffer
	body, detect := classifyAddBuffer(buf)

	var rendered string
	if strings.Contains(body, "\n") {
		rendered = editStyle.Render(body) + editStyle.Render("█")
	} else {
		rendered = renderEditField(body, v.addCursor)
	}
	if detect != "" {
		rendered += "  " + faint.Render("· "+detect)
	}

	keysHint := "Enter save · Esc cancel · paste URL or search cache"
	if len(v.addSuggestions) > 0 {
		keysHint = "↑/↓ select · Tab/Enter save selected · Esc cancel"
	}

	// The "▸" cursor replaces the leading two spaces of helpRowPrefix so the
	// focused slot reads the same width as the idle/default help row.
	line := cursor + "+ " + rendered + "  " + faint.Render(keysHint)

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
		b.WriteString(strings.Repeat(" ", len(helpRowPrefix)+2))
		b.WriteString(tui.TruncateWithEllipsis(label, suggestionWidth))
	}
	if strings.TrimSpace(v.addHint) != "" {
		noteWidth := max(12, v.termWidth-len(helpRowPrefix)-2)
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(" ", len(helpRowPrefix)+2))
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
