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
	name      string
	planPath  string
	rows      []csvplan.CollectionRow
	collCfg   project.Collection
	columns   []collectionColumn
	states    []rowState // per-row cache/render state
	rowStatus map[int]string
	activity  string
	tick      int
	cursor    int
	scrollTop int

	// Inline edit state (set by model when modeInlineEdit is active).
	editing      bool
	editFieldIdx int
	editValue    string

	// needsReload is set after opening the plan file externally (Shift+E with `open`).
	// The next navigation key in this view triggers a reload from disk.
	needsReload bool

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

func discoverColumns(rows []csvplan.CollectionRow) []collectionColumn {
	// Gather all field keys that have at least one non-empty value.
	fieldPresent := make(map[string]bool)
	for _, row := range rows {
		for k, v := range row.CustomFields {
			if strings.TrimSpace(v) != "" {
				fieldPresent[k] = true
			}
		}
	}

	hiddenFields := map[string]bool{}

	var cols []collectionColumn
	seen := make(map[string]bool)

	// Add known fields first, in order, if present.
	for _, kf := range knownFieldOrder {
		if fieldPresent[kf.field] && !hiddenFields[kf.field] {
			cols = append(cols, collectionColumn{
				header: strings.ToUpper(kf.field),
				field:  kf.field,
			})
			seen[kf.field] = true
		}
	}

	// Add remaining fields alphabetically.
	var extras []string
	for k := range fieldPresent {
		if !seen[k] && !hiddenFields[k] {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	for _, k := range extras {
		cols = append(cols, collectionColumn{
			header: strings.ToUpper(k),
			field:  k,
			fixed:  false,
		})
	}

	return cols
}

func newCollectionView(coll project.Collection, pp paths.ProjectPaths, cfg config.Config, idx *cache.Index) collectionView {
	states := computeRowStates(coll, pp, cfg, idx)
	return collectionView{
		name:      coll.Name,
		planPath:  coll.Plan,
		rows:      coll.Rows,
		collCfg:   coll,
		columns:   discoverColumns(coll.Rows),
		states:    states,
		rowStatus: make(map[int]string),
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
	h := v.termHeight - 9
	if h < 1 {
		h = 1
	}
	return h
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
		if i == v.cursor {
			cursor = cursorStyle.Render("▸ ")
			idx = cursorStyle.Render(idx)
		} else if state != rowRendered {
			idx = stateStyle.Render(idx)
		} else {
			idx = faint.Render(idx)
		}

		isEditRow := v.editing && i == v.cursor

		status := compactRowStatus(v.rowStatus[row.Index], v.tick)
		gutter := fmt.Sprintf("%s%-*s %-*s", cursor, idxWidth-2, idx, statusWidth, tui.TruncateWithEllipsis(status, statusWidth))
		parts := []string{gutter}
		for j, col := range v.columns {
			val := sanitize(row.CustomFields[col.field])
			w := widths[j]

			// Inline edit: show edit buffer with cursor on the active field.
			if isEditRow && j == v.editFieldIdx {
				display := v.editValue + "█"
				display = tui.TruncateWithEllipsis(display, w)
				parts = append(parts, editStyle.Render(fmt.Sprintf("%-*s", w, display)))
				continue
			}
			// Inline edit: highlight other fields on the edit row.
			if isEditRow {
				val = tui.TruncateWithEllipsis(val, w)
				parts = append(parts, editRowStyle.Render(fmt.Sprintf("%-*s", w, val)))
				continue
			}

			if state != rowRendered {
				parts = append(parts, stateStyle.Render(fmt.Sprintf("%-*s", w, tui.TruncateWithEllipsis(val, w))))
			} else if col.field == "title" {
				parts = append(parts, fmt.Sprintf("%-*s", w, tui.TruncateWithEllipsis(val, w)))
			} else {
				parts = append(parts, faint.Render(fmt.Sprintf("%-*s", w, tui.TruncateWithEllipsis(val, w))))
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

	if len(v.rows) == 0 {
		b.WriteString(faint.Render("  No rows. Press 'a' to add a clip."))
		b.WriteByte('\n')
	}

	return b.String()
}

func compactRowStatus(raw string, tick int) string {
	status := strings.TrimSpace(raw)
	switch {
	case status == "":
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
