package dashboard

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/playback"
	"powerhour/internal/project"
	renderstate "powerhour/internal/render/state"
	"powerhour/internal/tui"
	"powerhour/pkg/csvplan"
)

// collectionHeaderLines is the collection view's own chrome on top of
// dashboardChromeLines: the section label (1 line), column headers (1 line),
// and the unified help row (1 line) = 3 lines of fixed non-data content.
const collectionHeaderLines = 3

// rowState describes the cache/render state of a collection row.
type rowState int

const (
	rowRendered    rowState = iota // segment exists on disk
	rowNotRendered                 // cached but segment not rendered
	rowNotCached                   // source not in cache
	rowStale                       // rendered, but input changed since last render
)

const (
	// maxAddSuggestions is the maximum number of song suggestions displayed
	// in the add-clip slot.
	maxAddSuggestions = 3
)

var rowStateStyles = map[rowState]lipgloss.Style{
	rowRendered:    lipgloss.NewStyle(),                                   // default
	rowNotRendered: lipgloss.NewStyle().Foreground(lipgloss.Color("214")), // amber
	rowNotCached:   lipgloss.NewStyle().Foreground(lipgloss.Color("9")),   // bright red
	rowStale:       lipgloss.NewStyle().Foreground(lipgloss.Color("213")), // pink
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
	// label leads: it is the row's human-readable identity, and a column
	// you read to find a row belongs before columns you read once you have.
	{"label"},
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

func newCollectionView(coll project.Collection, pp paths.ProjectPaths, cfg config.Config, pos playback.PositionIndex, idx *cache.Index, rs *renderstate.RenderState, src *sourceCache) collectionView {
	states := computeRowStates(coll, pp, cfg, pos, idx, rs, src)
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

// computeRowStates classifies every row of a collection for the row-colour
// legend. src memoizes source resolution across calls: this runs again after
// every plan write, so without it reordering one row re-stats every row's
// source — and a plan whose links sit on a mounted share pays for that in
// whole seconds. May be nil, which resolves without memoizing.
func computeRowStates(coll project.Collection, pp paths.ProjectPaths, cfg config.Config, pos playback.PositionIndex, idx *cache.Index, rs *renderstate.RenderState, src *sourceCache) []rowState {
	states := make([]rowState, len(coll.Rows))
	filenameTemplate := cfg.SegmentFilenameTemplate()
	fades := project.EffectiveCollectionFades(cfg, coll)
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
			// The same resolution the readiness dot uses, so the two
			// surfaces cannot disagree about whether a source exists.
			cached = src.resolve(idx, pp.Root, row) != ""
		}

		if !cached {
			states[i] = rowNotCached
			continue
		}

		// Check rendered segment.
		seg := resolveRenderedSegment(pp, cfg, pos, coll.Name, coll, row)
		if _, err := os.Stat(seg.OutputPath); err != nil {
			states[i] = rowNotRendered
			continue
		}

		if rs != nil {
			if prior, ok := rs.Segments[renderstate.SegmentKey(seg)]; ok {
				if fadeVals, ok := fades[row.Index]; ok {
					seg.Clip.FadeInSeconds = fadeVals[0]
					seg.Clip.FadeOutSeconds = fadeVals[1]
				}
				currentHash := renderstate.SegmentInputHash(seg, filenameTemplate)
				if currentHash != prior.InputHash {
					states[i] = rowStale
					continue
				}
			}
		}
		states[i] = rowRendered
	}
	return states
}

func (v collectionView) visibleRowCount() int {
	// dashboardChromeLines (outer chrome: header/blanks/status/footer) +
	// collectionHeaderLines (section label, column header, help row) = 8 lines
	// of fixed chrome reserved; the remaining termHeight is available for data rows.
	h := v.termHeight - dashboardChromeLines - collectionHeaderLines
	// The focused add-slot is the only help row that can exceed a single
	// line: it optionally adds suggestion rows and a dynamic hint line.
	if v.addFocus {
		h -= v.addSlotExtraLines()
	}
	// The confirm-delete prompt renders as an inserted row beneath the
	// target row (not in the footer), so it consumes one line of budget
	// just like the add-slot's extra lines do.
	if v.confirmDelete != "" {
		h--
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

// editHeadroom is the typing room an inline-edit cell keeps past the end of
// its buffer: one column for the cursor block sitting after the last
// character, plus a few so the cell does not resize on every keystroke.
const editHeadroom = 4

// editOverflowExtent computes the horizontal position (xOffset), the width
// (cellWidth) and the number of columns to the right that an inline-edit
// cell on column editIdx swallows (covered).
//
// The cell is sized to its content, not to the terminal: a short value stays
// inside its own column and every column to its right keeps rendering, which
// is what makes it possible to read a row's link while typing its label. A
// value longer than its column grows rightwards, but only as far as it
// needs, and it always snaps out to a whole column boundary so the columns
// still on screen stay aligned with their headers. The terminal's right
// margin (less a 2-column pad) is the hard cap.
func editOverflowExtent(widths []int, editIdx, gutterWidth, gutterGapWidth, columnGapWidth, termWidth, valueWidth int) (xOffset, cellWidth, covered int) {
	xOffset = gutterWidth + gutterGapWidth
	for k := range editIdx {
		xOffset += widths[k] + columnGapWidth
	}

	own := widths[editIdx]
	maxWidth := max(own, termWidth-xOffset-2)

	desired := min(max(own, valueWidth+editHeadroom), maxWidth)

	// Grow a whole column at a time so what remains on screen stays aligned.
	cellWidth = own
	for cellWidth < desired && editIdx+covered+1 < len(widths) {
		next := columnGapWidth + widths[editIdx+covered+1]
		if cellWidth+next > maxWidth {
			break
		}
		cellWidth += next
		covered++
	}
	// The last column has nothing to its right to snap to, so it takes the
	// leftover margin directly.
	if editIdx+covered+1 >= len(widths) {
		cellWidth = max(cellWidth, desired)
	}

	return xOffset, cellWidth, covered
}

// columnMaxWidth caps how wide a single flex column (e.g. LINK) is allowed
// to grow even when terminal width and short sibling columns leave room.
const columnMaxWidth = 45

// columnMinWidth is the floor a column is shrunk to under horizontal
// pressure, keeping every column at least minimally readable rather than
// starving one column to zero while others sit at full content width.
const columnMinWidth = 6

// computeColumnWidths gives each column only the width its actual data
// needs (header length vs. widest cell, capped at columnMaxWidth) instead
// of splitting the table evenly — a short START_TIME/DURATION column no
// longer steals space that a long LINK/TITLE column needs. Leftover width
// (available > natural total) is handed to columns whose real content was
// truncated by the cap, so those grow first. Under pressure (available <
// natural total), columns shrink proportionally to their slack above
// columnMinWidth, so wide columns give up more than already-tight ones.
func (v collectionView) computeColumnWidths(tableWidth, baseWidth int) []int {
	n := len(v.columns)
	if n == 0 {
		return nil
	}

	contentWidth := func(col collectionColumn) int {
		w := runewidth.StringWidth(col.header)
		for _, row := range v.rows {
			val := sanitize(row.CustomFields[col.field])
			if vw := runewidth.StringWidth(val); vw > w {
				w = vw
			}
		}
		return w
	}

	uncapped := make([]int, n)
	natural := make([]int, n)
	total := 0
	for i, col := range v.columns {
		w := contentWidth(col)
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
		for i := range v.columns {
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
	tableWidth := max(v.termWidth-20, baseWidth)
	widths := v.computeColumnWidths(tableWidth, baseWidth)

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
	endRow := min(startRow+visible, len(v.rows))
	if endRow < len(v.rows) {
		visible--
		visible = max(visible, 0)
		endRow = min(startRow+visible, len(v.rows))
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
		truncatedStatus := tui.TruncateWithEllipsis(status, statusWidth)
		paddedStatus := lipgloss.NewStyle().Width(statusWidth).Render(truncatedStatus)
		gutter := fmt.Sprintf("%s%s %s", cursor, idx, paddedStatus)
		if isEditRow {
			gutter = editRowBgOnly.Width(gutterWidth).Render(gutter)
		}
		parts := []string{gutter}
		// skipUntil is the last column index swallowed by an inline-edit
		// cell that grew past its own column. Columns after it still render.
		skipUntil := -1
		for j, col := range v.columns {
			if j <= skipUntil {
				continue
			}
			val := sanitize(row.CustomFields[col.field])
			w := widths[j]

			// Inline edit: show the edit buffer with the cursor on the active
			// field, in a cell sized to the buffer rather than to the
			// terminal, so a short edit leaves the rest of the row readable.
			if isEditRow && j == v.editFieldIdx {
				_, cellWidth, covered := editOverflowExtent(widths, j, gutterWidth, gutterGapWidth, columnGapWidth, v.termWidth, runewidth.StringWidth(v.editValue))
				parts = append(parts, renderEditCell(v.editValue, v.editCursor, cellWidth))
				skipUntil = j + covered

				// The hint only makes sense when the cell runs to the end of
				// the row; with columns still to render after it, a mid-row
				// hint would push them out of alignment.
				if j+covered == len(v.columns)-1 && covered > 0 {
					hint := fmt.Sprintf("+%d fields", covered)
					parts = append(parts, editRowBgOnly.Render(faint.Render(hint)))
				}
				continue
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
			gap := strings.Repeat(" ", gutterGapWidth)
			if isEditRow {
				gap = editRowBgOnly.Render(gap)
			}
			b.WriteString(gap)
			cells := parts[1:]
			if isEditRow {
				// Join cells with background-tinted separators
				joined := ""
				for k, cell := range cells {
					if k > 0 {
						joined += editRowBgOnly.Render("  ")
					}
					joined += cell
				}
				b.WriteString(joined)
			} else {
				b.WriteString(strings.Join(cells, "  "))
			}
		}
		b.WriteByte('\n')

		// Confirm-delete prompt: inserted directly beneath the row it targets,
		// for visual proximity to the destructive action (not in the footer).
		if v.confirmDelete != "" && i == v.cursor {
			b.WriteString(helpRowText(v.confirmDelete, confirmStyle, v.termWidth))
			b.WriteByte('\n')
		}
	}

	if endRow < len(v.rows) {
		b.WriteString(faint.Render(fmt.Sprintf("  ↓ %d more below", len(v.rows)-endRow)))
		b.WriteByte('\n')
	}

	// Unified inline help row. Contextual messages (edit mode, transient
	// notes like "removed row", the focused add slot, and the default
	// "press a to add a clip") render through the same footer element in a
	// fixed priority order. Confirm-delete is the one exception: it renders
	// as its own inserted row beneath the target row above, not here.
	b.WriteString(v.renderHelpRow())
	b.WriteByte('\n')

	return b.String()
}

func editContextNote(v collectionView, row csvplan.CollectionRow) string {
	// Build the help text with exit keys first so they survive truncation on narrow terminals.
	// Order: exit keys · navigation · context (row/field).
	parts := []string{"Enter save", "Esc cancel", "Tab next field"}

	// Append context about what row/field is being edited.
	context := fmt.Sprintf("Edit row %02d", row.Index)
	if v.editFieldIdx >= 0 && v.editFieldIdx < len(v.columns) {
		context += " · " + v.columns[v.editFieldIdx].field
	}
	parts = append(parts, context)

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
//  1. inline-edit context (field being edited, keys)
//  2. transient note / status on the cursor row ("removed row", "probing", …)
//  3. focused add-slot (input + keys hint + suggestions)
//  4. default action hint ("press a to add a clip")
//
// The confirm-delete prompt is NOT part of this ladder: it renders as an
// inserted row directly beneath the target row (see view()), not in the
// footer, so it stays visually attached to the row it is about to destroy.
// cacheView and timelineView still show their confirm-delete prompt here in
// the footer — this carve-out is collection-view only.
//
// Only the add-slot branch can produce multiple lines (input + suggestions +
// dynamic hint); all others render exactly one line via helpRowText.
func (v collectionView) renderHelpRow() string {
	var sources []helpRowSource

	// 1. Inline-edit context. If the edit row also carries a transient note,
	// the note wins (so "saved" / "probing" are visible during the edit lull
	// between keystrokes). This branch always contributes a non-empty
	// source, so it always wins the ladder outright.
	if v.editing && v.cursor >= 0 && v.cursor < len(v.rows) {
		row := v.rows[v.cursor]
		rawStatus := v.rowStatus[row.Index]
		if note := inlineRowNote(rawStatus, v.tick); note != "" {
			sources = append(sources, helpRowSource{note, noteStyleFor(rawStatus)})
		} else {
			sources = append(sources, helpRowSource{editContextNote(v, row), faint})
		}
	} else if v.cursor >= 0 && v.cursor < len(v.rows) {
		// 2. Transient note on the cursor row.
		row := v.rows[v.cursor]
		rawStatus := v.rowStatus[row.Index]
		sources = append(sources, helpRowSource{inlineRowNote(rawStatus, v.tick), noteStyleFor(rawStatus)})
	}

	// 3. Focused add slot / 4. default — the add slot can render multiple
	// lines, so it's the fallback rather than a source.
	fallback := func() string { return helpRowText("press a to add a clip", faint, v.termWidth) }
	if v.addFocus {
		fallback = v.renderAddSlot
	}

	return resolveHelpRow(v.termWidth, fallback, sources...)
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

	keysHint := "Enter save · Esc cancel · paste URL or search cache"
	if len(v.addSuggestions) > 0 {
		keysHint = "↑/↓ select · Tab/Enter save selected · Esc cancel"
	}

	// Budget the trailing plain text (detect hint, then keys hint) against
	// what's left after the fixed cursor/marker/body, mirroring
	// helpRowText's budget-then-style order: compute the width on plain
	// text first, then apply lipgloss styling only to what survives. The
	// "▸" cursor replaces the leading two spaces of helpRowPrefix, and "+ "
	// is the marker, so the available budget is computed the same way
	// helpRowText computes it.
	// renderEditField appends a trailing cursor glyph (one visual column)
	// when the cursor sits at or past the end of body, which is one column
	// wider than body's own plain width; the multi-line branch always
	// appends a "█" cursor glyph. Either way, account for it here so the
	// budget matches what renderEditField actually produces below.
	cursorGlyphWidth := 0
	switch {
	case strings.Contains(body, "\n"):
		cursorGlyphWidth = runewidth.RuneWidth('█')
	case v.addCursor >= len(body):
		cursorGlyphWidth = 1
	}

	avail := max(v.termWidth-len(helpRowPrefix), 12)
	remaining := avail - runewidth.StringWidth(body) - cursorGlyphWidth

	const gapWidth = 2 // "  " separator before each trailer
	detectText := ""
	if detect != "" {
		detectText = "· " + detect
	}
	keysWidth := gapWidth + runewidth.StringWidth(keysHint)
	detectWidth := 0
	if detectText != "" {
		detectWidth = gapWidth + runewidth.StringWidth(detectText)
	}

	fittedKeys := ""
	fittedDetect := ""
	switch {
	case remaining <= 0:
		// No room for either trailer; body alone fills (or exceeds) the
		// budget.
	case detectWidth+keysWidth <= remaining:
		// Both trailers fit untouched.
		fittedKeys = keysHint
		fittedDetect = detectText
	case keysWidth <= remaining:
		// keysHint has priority and fits fully; the detect hint shrinks or
		// drops into whatever's left.
		fittedKeys = keysHint
		detectBudget := remaining - keysWidth - gapWidth
		if detectText != "" && detectBudget > 0 {
			fittedDetect = tui.TruncateWithEllipsis(detectText, detectBudget)
		}
	default:
		// Not even keysHint fits fully; drop the detect hint and shrink
		// keysHint into the remaining space.
		keysBudget := remaining - gapWidth
		fittedKeys = tui.TruncateWithEllipsis(keysHint, keysBudget)
	}

	var rendered string
	if strings.Contains(body, "\n") {
		rendered = editStyle.Render(body) + editStyle.Render("█")
	} else {
		rendered = renderEditField(body, v.addCursor)
	}
	if fittedDetect != "" {
		rendered += "  " + faint.Render(fittedDetect)
	}

	line := cursor + "+ " + rendered
	if fittedKeys != "" {
		line += "  " + faint.Render(fittedKeys)
	}

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
	note := strings.TrimSpace(strings.TrimPrefix(status, "note:"))
	if strings.HasPrefix(note, "ERROR - ") {
		return "└─▶ " + note
	}
	return note
}

// isErrorRowNote reports whether raw is a "note:ERROR - ..." row status, so
// callers can style it distinctly from ordinary transient notes.
func isErrorRowNote(raw string) bool {
	note := strings.TrimPrefix(strings.TrimSpace(raw), "note:")
	return strings.HasPrefix(strings.TrimSpace(note), "ERROR - ")
}
