package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"powerhour/internal/config"
	"powerhour/internal/playback"
	"powerhour/internal/project"
)

// timelineHeaderLines is the timeline view's own chrome on top of
// dashboardChromeLines: panel/section-label overhead plus one line for the
// unified help row at the bottom.
const timelineHeaderLines = 8

// minPlaybackPanelLines is the floor the playback order panel keeps when the
// sequence list is long enough to compete for the content budget. The
// sequence panel is capped to whatever is left above it.
const minPlaybackPanelLines = 5

// timelineView holds the state for the timeline view with output at the top,
// sequence entries in the middle, and resolved playback order at the bottom.
//
// The sequence panel is read-only: a sequence entry has seven fields
// (collection, slice, interleave, file, fade, fade_in, fade_out) and this
// view can display exactly one of them, order — editing lives in
// powerhour.yaml. The playback order panel is the mutable one: every
// gesture calls internal/playback directly (ADR 0003), never reimplementing
// swap/set/lock/shuffle logic here.
type timelineView struct {
	sequence []config.SequenceEntry
	resolved []project.TimelineEntry

	// order mirrors the playback order 1:1 against resolved (playback.Placements
	// guarantees one placement per slot, in slot order) so the panel can render
	// lock state and mutate by index without holding any resolution logic of
	// its own.
	order playback.Order

	// Data references for rendering labels.
	collections     map[string]project.Collection
	collectionNames []string

	// Cursor for the read-only sequence entries panel. Fixed height, no scroll.
	seqCursor int

	// Cursor and scroll for the playback order panel.
	resCursor    int
	resScrollTop int

	// Which panel has focus: 0 = sequence entries, 1 = playback order.
	focusPanel int

	// Concat output.
	concatPath     string // path to the concat output file
	concatExists   bool   // whether the file exists on disk
	concatSize     int64  // file size in bytes
	concatModTime  time.Time
	concatFocus    bool // cursor is on the concat row
	seqStatus      map[int]string
	seqStatusUntil map[int]int

	// Inline confirm prompt rendered beneath the cursor sequence row (set by
	// model when modeConfirmDelete is active). Empty = no pending confirm.
	confirmDelete string

	// cycling reports whether cycleSlot is live. It is a separate flag rather
	// than a -1 sentinel because a timelineView built as a zero value would
	// otherwise read as "cycling slot 0" and draw the ‹ › arrows unasked.
	cycling   bool
	cycleSlot int

	// cycleBackup is the order as it stood when cycle mode was armed. It is
	// what Esc restores, and diffing it against the live order is how the
	// panel marks which rows a pending edit would move — including the far
	// side of a swap, which is not the row under the cursor.
	cycleBackup []playback.Slot

	// cycleOffset is how far the pending edit has walked from the occupant
	// cycleBackup holds. Every step re-applies this single offset to a fresh
	// copy of the backup rather than stepping the already-stepped order:
	// composing swaps would strand each intermediate partner holding a row it
	// never originally had, instead of returning it to the one it did.
	cycleOffset int

	// orderNote is a transient one-line result of the last playback-order
	// gesture (swap confirmed, shuffle count, "file entries have no pool").
	// Rendered through the footer ladder, never as a second status line.
	orderNote string

	// reconcileNote summarizes what playback.Reconcile changed the last time
	// the order was resolved (dropped/added/filled slots). Empty when the
	// stored order needed no reconciliation.
	reconcileNote string

	// Terminal dimensions for viewport calculation.
	termWidth  int
	termHeight int
}

func newTimelineView(cfg config.Config, resolved []project.TimelineEntry, collections map[string]project.Collection, collectionNames []string, projectRoot string) timelineView {
	concatPath, concatExists, concatSize, concatModTime := findConcatOutput(projectRoot)
	return timelineView{
		sequence:        cfg.Timeline.Sequence,
		resolved:        resolved,
		collections:     collections,
		collectionNames: collectionNames,
		concatPath:      concatPath,
		concatExists:    concatExists,
		concatSize:      concatSize,
		concatModTime:   concatModTime,
		seqStatus:       make(map[int]string),
		seqStatusUntil:  make(map[int]int),
	}
}

// cyclePending reports whether slot i holds a different row than it did when
// cycle mode was armed — an uncommitted change the panel marks as pending.
func (v timelineView) cyclePending(i int) bool {
	if !v.cycling || i >= len(v.cycleBackup) || i >= len(v.order.Slots) {
		return false
	}
	return v.cycleBackup[i].RowID != v.order.Slots[i].RowID
}

// findConcatOutput looks for the concat output file in the project root.
func findConcatOutput(projectRoot string) (string, bool, int64, time.Time) {
	for _, ext := range []string{".mp4", ".mkv", ".mov"} {
		p := filepath.Join(projectRoot, "powerhour"+ext)
		if info, err := os.Stat(p); err == nil {
			return p, true, info.Size(), info.ModTime()
		}
	}
	return filepath.Join(projectRoot, "powerhour.mp4"), false, 0, time.Time{}
}

// contentHeight returns total height available for the sequence and preview panels.
// dashboardChromeLines (outer chrome) + timelineHeaderLines reserves panel
// overhead plus one line for the unified help row at the bottom.
func (v timelineView) contentHeight() int {
	h := max(v.termHeight-dashboardChromeLines-timelineHeaderLines, 4)
	return h
}

func (v timelineView) sequenceLinesNeeded() int {
	lines := len(v.sequence)
	if lines == 0 {
		lines = 1
	}
	return lines
}

// seqPanelHeight returns height for the read-only sequence entries panel: it
// always fits the sequence list exactly, with no scrolling and no cap — a
// real project's sequence is a handful of entries. The playback order panel
// gets whatever height remains.
func (v timelineView) seqPanelHeight() int {
	// The sequence list is short in any real project, so it normally fits
	// exactly and this cap never binds. It exists so a long sequence — or a
	// short terminal — cannot take the whole content budget: resPanelHeight
	// is contentHeight minus this, and at zero the playback order panel
	// vanishes with the footer under it.
	budget := max(v.contentHeight()-minPlaybackPanelLines, 1)
	return min(v.sequenceLinesNeeded(), budget)
}

// sequenceOverflow reports how many sequence entries the panel cannot show.
// Non-zero only when the cap in seqPanelHeight binds.
func (v timelineView) sequenceOverflow() int {
	return max(len(v.sequence)-v.seqPanelHeight(), 0)
}

// resPanelHeight returns height for the playback order panel — the content
// budget left over after the fixed-height sequence panel.
func (v timelineView) resPanelHeight() int {
	return v.contentHeight() - v.seqPanelHeight()
}

func (v timelineView) view(cacheStatus map[string]string) string {
	var b strings.Builder

	// --- Output ---
	b.WriteString(sectionLabel.Render("POWER HOUR"))
	b.WriteByte('\n')

	cursor := "  "
	if v.concatFocus {
		cursor = cursorStyle.Render("▸ ")
	}
	if v.concatExists {
		name := filepath.Base(v.concatPath)
		size := formatFileSize(v.concatSize)
		exportedAt := faint.Render("exported " + v.concatModTime.Local().Format("2006-01-02 15:04"))
		fmt.Fprintf(&b, "%s%s  %s  %s",
			cursor,
			countGreen.Render(name),
			faint.Render(size),
			exportedAt)
	} else {
		b.WriteString(cursor + faint.Render("not yet exported — press r to finalize"))
	}
	b.WriteByte('\n')
	b.WriteByte('\n')

	// --- Sequence entries panel (read-only, fixed height) ---
	b.WriteString(sectionLabel.Render("TIMELINE SEQUENCE"))
	b.WriteByte('\n')

	shown := v.sequence
	if over := v.sequenceOverflow(); over > 0 {
		// Reserve the last line for the count, so a clipped list says so
		// rather than silently ending early. Still not scrolling: the
		// sequence is edited in powerhour.yaml, not here.
		shown = v.sequence[:max(v.seqPanelHeight()-1, 0)]
	}

	for i, entry := range shown {
		cursor := "  "
		if i == v.seqCursor && v.focusPanel == 0 && !v.concatFocus {
			cursor = cursorStyle.Render("▸ ")
		}

		b.WriteString(cursor)
		b.WriteString(faint.Render(fmt.Sprintf("%d. ", i+1)))

		if entry.File != "" {
			b.WriteString(typeBadgeFile.Render("file: "))
			b.WriteString(filepath.Base(entry.File))
		} else {
			b.WriteString(typeBadgeColl.Render(entry.Collection))
			b.WriteString(fadeDim.Render(" · " + timelineSliceLabel(entry.Slice)))
			if entry.Interleave != nil {
				b.WriteString(fadeDim.Render(fmt.Sprintf(" · interleave: %s every %d", entry.Interleave.Collection, entry.Interleave.Every)))
			}
		}

		// Fade info, right side.
		fade := formatFade(entry.Fade, entry.FadeIn, entry.FadeOut)
		if fade != "" {
			b.WriteString(fadeDim.Render("  " + fade))
		}
		b.WriteByte('\n')
	}

	if over := v.sequenceOverflow(); over > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  + %d more — edit powerhour.yaml to see them all", over)))
		b.WriteByte('\n')
	}

	if len(v.sequence) == 0 {
		b.WriteByte('\n')
	}

	// --- Playback order panel ---
	totalDuration := 0
	for _, e := range v.resolved {
		totalDuration += v.entryDuration(e)
	}
	b.WriteString(sectionLabel.Render(fmt.Sprintf("PLAYBACK ORDER · %d clips · ~%s", len(v.resolved), formatDuration(totalDuration))))
	b.WriteByte('\n')

	if v.reconcileNote != "" {
		b.WriteString(faint.Render("  " + v.reconcileNote))
		b.WriteByte('\n')
	}

	resH := v.resPanelHeight()
	visibleRes := max(resH, 1)
	startRes := v.resScrollTop

	// Reserve a line for the up indicator if scrolled, and a line for the
	// down indicator if there will be entries below — so that indicators
	// don't push content past the footer.
	if startRes > 0 {
		visibleRes--
	}
	endRes := min(startRes+visibleRes, len(v.resolved))
	if endRes < len(v.resolved) {
		visibleRes--
		visibleRes = max(visibleRes, 0)
		endRes = min(startRes+visibleRes, len(v.resolved))
	}

	if startRes > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  ↑ %d more above", startRes)))
		b.WriteByte('\n')
	}

	for i := startRes; i < endRes; i++ {
		e := v.resolved[i]
		label := v.entryLabel(e)
		source := v.entrySource(e)
		dur := v.entryDuration(e)
		cursor := "  "
		if i == v.resCursor && v.focusPanel == 1 {
			cursor = cursorStyle.Render("▸ ")
		}

		// Playback-readiness dot: green = rendered segment ready at this
		// position, yellow half-dot = rendered at an older position so only
		// its burned-in number is stale, amber = falls back to the raw/uncut
		// source, red = nothing playable yet.
		key := cacheKeyForEntry(e)
		dot := dotUnavailable
		switch cacheStatus[key] {
		case "rendered":
			dot = dotCached
		case "misnumbered":
			dot = dotMisnumbered
		case "cached":
			dot = dotFallback
		}

		locked := i < len(v.order.Slots) && v.order.Slots[i].Locked
		lockCol := " "
		if locked {
			lockCol = lockGlyphStyle.Render("L")
		}

		displayLabel := label
		switch {
		case v.cycling && i == v.cycleSlot:
			// Arrows on both sides say ←/→ change this row.
			displayLabel = cycleArrowStyle.Render("‹ ") + cycleSlotStyle.Render(label) + cycleArrowStyle.Render(" ›")
		case v.cyclePending(i):
			// The other end of a pending swap. Italic says "not saved yet",
			// so the row does not read as a change that already happened.
			displayLabel = cyclePendingStyle.Render(label)
		case locked:
			displayLabel = lockedRowStyle.Render(label)
		}

		seqNum := faint.Render(fmt.Sprintf("%02d", e.Sequence))
		sourceLabel := faint.Render(source)
		durLabel := faint.Render(formatDuration(dur))

		fmt.Fprintf(&b, "%s%s %s %s %s", cursor, dot, lockCol, seqNum, displayLabel)

		// Right-align source and duration.
		rightPart := fmt.Sprintf("%s · %s", sourceLabel, durLabel)
		fixedWidth := lipgloss.Width(cursor) + lipgloss.Width(dot) + 1 + lipgloss.Width(lockCol) + 1 + lipgloss.Width(seqNum) + 1
		padding := v.termWidth - fixedWidth - lipgloss.Width(displayLabel) - lipgloss.Width(rightPart) - 2
		if padding > 0 {
			b.WriteString(strings.Repeat(" ", padding))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(rightPart)
		b.WriteByte('\n')
	}

	if endRes < len(v.resolved) {
		b.WriteString(faint.Render(fmt.Sprintf("  ↓ %d more below", len(v.resolved)-endRes)))
		b.WriteByte('\n')
	}

	b.WriteString(v.renderHelpRow())
	b.WriteByte('\n')

	return b.String()
}

// renderHelpRow returns the single inline help row for the timeline view.
// Priority: confirm-delete, then a transient gesture note, then the
// reconcile summary, then a panel-aware default hint. The sequence panel is
// read-only, so its default advertises no mutation keys; the playback order
// panel advertises the gestures.
func (v timelineView) renderHelpRow() string {
	var sources []helpRowSource
	sources = append(sources, helpRowSource{v.confirmDelete, confirmStyle})
	sources = append(sources, helpRowSource{v.orderNote, editStyle})
	sources = append(sources, helpRowSource{v.reconcileNote, faint})

	defaultText := "s change · l lock · S shuffle"
	if v.focusPanel == 0 && !v.concatFocus {
		defaultText = "read-only — edit timeline.sequence in powerhour.yaml (e)"
	}
	sources = append(sources, helpRowSource{defaultText, faint})

	return resolveHelpRow(v.termWidth, nil, sources...)
}

func timelineSliceLabel(raw string) string {
	slice := config.NormalizeTimelineSlice(raw)
	if slice == "" || slice == "start:end" {
		return "to end"
	}
	return slice
}

func (v timelineView) entryLabel(e project.TimelineEntry) string {
	return sanitize(project.TimelineEntryLabel(e, v.collections))
}

func (v timelineView) entrySource(e project.TimelineEntry) string {
	if e.SourceFile != "" {
		return "file"
	}
	return e.Collection
}

func (v timelineView) entryDuration(e project.TimelineEntry) int {
	if e.SourceFile != "" {
		return 0 // unknown for inline files without probing
	}
	if c, ok := v.collections[e.Collection]; ok && e.Index >= 1 && e.Index <= len(c.Rows) {
		row := c.Rows[e.Index-1]
		if row.DurationSeconds > 0 {
			return row.DurationSeconds
		}
		return c.Config.Duration
	}
	return 0
}

func (v *timelineView) autoScrollRes() {
	visible := scrollAutoScrollBudget(v.resPanelHeight())
	if v.resCursor < v.resScrollTop {
		v.resScrollTop = v.resCursor
	} else if v.resCursor >= v.resScrollTop+visible {
		v.resScrollTop = v.resCursor - visible + 1
	}
}

func cacheKeyForEntry(e project.TimelineEntry) string {
	if e.SourceFile != "" {
		return "file:" + e.SourceFile
	}
	return fmt.Sprintf("%s:%d", e.Collection, e.Index)
}

func formatFade(fade, fadeIn, fadeOut float64) string {
	if fade > 0 {
		return fmt.Sprintf("fade: %.1f", fade)
	}
	parts := []string{}
	if fadeIn > 0 {
		parts = append(parts, fmt.Sprintf("in: %.1f", fadeIn))
	}
	if fadeOut > 0 {
		parts = append(parts, fmt.Sprintf("out: %.1f", fadeOut))
	}
	if len(parts) > 0 {
		return "fade " + strings.Join(parts, " ")
	}
	return ""
}

func formatDuration(seconds int) string {
	if seconds <= 0 {
		return "—"
	}
	m := seconds / 60
	s := seconds % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatFileSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\t\n\r"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
