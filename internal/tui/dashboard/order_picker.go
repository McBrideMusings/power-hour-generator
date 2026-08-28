package dashboard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/project"
	"powerhour/internal/tui"
)

// orderPickerItem is one candidate row in the picker: a stable row id (what
// playback.Set writes) and its presentational label (project.CollectionRowLabel).
type orderPickerItem struct {
	rowID string
	label string
}

// orderPickerOverlay is the `repeat`-selection picker: type-to-filter over a
// collection's pool, Enter replaces the target slot's occupant via
// playback.Set, Esc cancels. It renders in the content area, following
// cacheDoctorOverlay (internal/tui/dashboard/cache_doctor.go), not
// full-screen like overlayHelp. It holds no domain logic — filtering is
// presentational only; the mutation itself is a single playback.Set call
// made by the model after the picker reports what was chosen.
type orderPickerOverlay struct {
	slot       int // 0-based playback-order slot this picker is replacing
	collection string
	items      []orderPickerItem
	filter     string
	cursor     int

	termWidth  int
	termHeight int
}

// buildOrderPickerItems lists coll's pool as picker candidates, in plan
// order, skipping any row with no stable id — mirroring playback.Pool but
// carrying the display label alongside each id.
func buildOrderPickerItems(coll project.Collection) []orderPickerItem {
	items := make([]orderPickerItem, 0, len(coll.Rows))
	for _, row := range coll.Rows {
		if row.RowID == "" {
			continue
		}
		items = append(items, orderPickerItem{
			rowID: row.RowID,
			label: project.CollectionRowLabel(coll.Config, row),
		})
	}
	return items
}

func newOrderPickerOverlay(slot int, collection string, items []orderPickerItem, w, h int) *orderPickerOverlay {
	return &orderPickerOverlay{
		slot:       slot,
		collection: collection,
		items:      items,
		termWidth:  w,
		termHeight: h,
	}
}

// filteredItems returns items whose label contains the current filter
// (case-insensitive substring), preserving pool order.
func (o *orderPickerOverlay) filteredItems() []orderPickerItem {
	q := strings.ToLower(strings.TrimSpace(o.filter))
	if q == "" {
		return o.items
	}
	out := make([]orderPickerItem, 0, len(o.items))
	for _, it := range o.items {
		if strings.Contains(strings.ToLower(it.label), q) {
			out = append(out, it)
		}
	}
	return out
}

// selected returns the row id under the cursor within the filtered list.
func (o *orderPickerOverlay) selected() (string, bool) {
	items := o.filteredItems()
	if o.cursor < 0 || o.cursor >= len(items) {
		return "", false
	}
	return items[o.cursor].rowID, true
}

// handleKey drives the picker. done reports the overlay should close;
// apply reports the model should apply the currently selected item (via
// playback.Set) before closing. Arrow keys move the cursor; j/k only move
// the cursor while the filter is empty (letters otherwise extend the
// filter), matching the type-to-filter contract in the issue body.
func (o *orderPickerOverlay) handleKey(msg tea.KeyMsg) (done bool, apply bool) {
	items := o.filteredItems()

	switch msg.Type {
	case tea.KeyEscape, tea.KeyCtrlC:
		return true, false
	case tea.KeyEnter:
		if len(items) == 0 {
			return false, false
		}
		if o.cursor >= len(items) {
			o.cursor = len(items) - 1
		}
		return true, true
	case tea.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return false, false
	case tea.KeyDown:
		if o.cursor < len(items)-1 {
			o.cursor++
		}
		return false, false
	case tea.KeyBackspace:
		if o.filter != "" {
			o.filter = o.filter[:len(o.filter)-1]
			o.cursor = 0
		}
		return false, false
	case tea.KeyCtrlU:
		o.filter = ""
		o.cursor = 0
		return false, false
	case tea.KeySpace:
		o.filter += " "
		o.cursor = 0
		return false, false
	case tea.KeyRunes:
		ch := string(msg.Runes)
		if o.filter == "" && (ch == "j" || ch == "k") {
			if ch == "j" && o.cursor < len(items)-1 {
				o.cursor++
			} else if ch == "k" && o.cursor > 0 {
				o.cursor--
			}
			return false, false
		}
		o.filter += ch
		o.cursor = 0
		return false, false
	}
	return false, false
}

// view renders the picker in the content area: no border, no centering —
// the same plain joined-lines shape as cacheDoctorOverlay.view().
func (o *orderPickerOverlay) view() string {
	var lines []string
	lines = append(lines, sectionLabel.Render(fmt.Sprintf("PICK A CLIP · %s · slot %d", o.collection, o.slot+1)))
	lines = append(lines, faint.Render("Filter: ")+o.filter)
	lines = append(lines, "")

	items := o.filteredItems()
	budget := max(o.termHeight-dashboardChromeLines-4, 3)
	for i, it := range items {
		if i >= budget {
			lines = append(lines, faint.Render(fmt.Sprintf("  … %d more", len(items)-budget)))
			break
		}
		cursor := "  "
		if i == o.cursor {
			cursor = cursorStyle.Render("▸ ")
		}
		label, _ := tui.TruncateToWidth(it.label, max(o.termWidth-6, 8), tui.TruncateOptions{Ellipsis: "…"})
		lines = append(lines, cursor+label)
	}
	if len(items) == 0 {
		lines = append(lines, faint.Render("  no matches"))
	}

	return strings.Join(lines, "\n") + "\n"
}

// pickerFooter returns the footer text for the picker overlay.
func (o *orderPickerOverlay) pickerFooter() string {
	return footerStyle.Render("type to filter  ↑/↓ move  Enter pick  Esc cancel")
}
