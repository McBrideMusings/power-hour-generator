package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"powerhour/internal/config"
	"powerhour/internal/project"

	"powerhour/internal/tui"
)

// timelineView holds the state for the timeline view with output at the top,
// sequence entries in the middle, and resolved preview at the bottom.
type timelineView struct {
	sequence []config.SequenceEntry
	resolved []project.TimelineEntry

	// Data references for rendering labels.
	collections     map[string]project.Collection
	collectionNames []string

	// Cursor and scroll for sequence entries panel.
	seqCursor    int
	seqScrollTop int

	// Scroll for resolved preview panel.
	resCursor    int
	resScrollTop int

	// Which panel has focus: 0 = sequence entries, 1 = resolved preview.
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
// -13 reserves chrome plus one line for the unified help row at the bottom.
func (v timelineView) contentHeight() int {
	h := v.termHeight - 13
	if h < 4 {
		h = 4
	}
	return h
}

func (v timelineView) sequenceLinesNeeded() int {
	lines := len(v.sequence)
	if lines == 0 {
		lines = 1
	}
	return lines
}

// seqPanelHeight returns height for the sequence entries panel.
func (v timelineView) seqPanelHeight() int {
	total := v.contentHeight()
	h := total / 4
	if h < 2 {
		h = 2
	}
	if needed := v.sequenceLinesNeeded(); needed > h {
		h = needed
	}
	if h > total-1 {
		h = total - 1
	}
	if h < 1 {
		h = 1
	}
	return h
}

// resPanelHeight returns height for the resolved preview panel (~60%).
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
		b.WriteString(fmt.Sprintf("%s%s  %s  %s",
			cursor,
			countGreen.Render(name),
			faint.Render(size),
			exportedAt))
	} else {
		b.WriteString(cursor + faint.Render("not yet exported — press c to concatenate"))
	}
	b.WriteByte('\n')
	b.WriteByte('\n')

	// --- Sequence entries panel ---
	b.WriteString(sectionLabel.Render("TIMELINE SEQUENCE"))
	b.WriteByte('\n')

	seqH := v.seqPanelHeight()
	visibleSeq := seqH
	if visibleSeq < 1 {
		visibleSeq = 1
	}
	startSeq := v.seqScrollTop
	endSeq := startSeq + visibleSeq
	if endSeq > len(v.sequence) {
		endSeq = len(v.sequence)
	}

	if startSeq > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  ↑ %d more above", startSeq)))
		b.WriteByte('\n')
	}
	rendered := endSeq - startSeq
	if startSeq > 0 {
		rendered++
	}

	for i := startSeq; i < endSeq; i++ {
		entry := v.sequence[i]
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

	if len(v.sequence) == 0 {
		b.WriteByte('\n')
	}

	if endSeq < len(v.sequence) {
		b.WriteString(faint.Render(fmt.Sprintf("  ↓ %d more below", len(v.sequence)-endSeq)))
		b.WriteByte('\n')
	}

	// Pad remaining sequence panel lines.
	if endSeq < len(v.sequence) {
		rendered++
	}
	for rendered < seqH {
		b.WriteByte('\n')
		rendered++
	}

	// --- Resolved preview panel ---
	totalDuration := 0
	for _, e := range v.resolved {
		totalDuration += v.entryDuration(e)
	}
	b.WriteString(sectionLabel.Render(fmt.Sprintf("PLAYBACK ORDER · %d clips · ~%s", len(v.resolved), formatDuration(totalDuration))))
	b.WriteByte('\n')

	resH := v.resPanelHeight()
	startRes := v.resScrollTop
	endRes := startRes + resH
	if endRes > len(v.resolved) {
		endRes = len(v.resolved)
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

		// Cache dot.
		key := cacheKeyForEntry(e)
		dot := dotMissing
		if status, ok := cacheStatus[key]; ok && status == "cached" {
			dot = dotCached
		}

		seqNum := faint.Render(fmt.Sprintf("%02d", e.Sequence))
		sourceLabel := faint.Render(source)
		durLabel := faint.Render(formatDuration(dur))

		b.WriteString(fmt.Sprintf("%s%s %s %s", cursor, dot, seqNum, label))

		// Right-align source and duration.
		rightPart := fmt.Sprintf("%s · %s", sourceLabel, durLabel)
		labelLen := 2 + 2 + 1 + 2 + 1 + len(tui.TruncateWithEllipsis(label, 999)) // cursor + dot + space + seq + space + label
		padding := v.termWidth - labelLen - lipgloss.Width(rightPart) - 2
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
// Priority matches the collection/cache views: confirm-delete wins, then any
// transient note on the focused row, then a default action hint. Timeline has
// no inline-edit or add-slot (those happen via modal prompts), so the ladder
// is shorter here but the shape is identical.
func (v timelineView) renderHelpRow() string {
	if v.confirmDelete != "" {
		return helpRowText(v.confirmDelete, confirmStyle, v.termWidth)
	}
	if v.focusPanel == 0 && !v.concatFocus && v.seqCursor >= 0 && v.seqCursor < len(v.sequence) {
		if note := inlineRowNote(v.seqStatus[v.seqCursor], 0); note != "" {
			return helpRowText(note, editStyle, v.termWidth)
		}
	}
	if len(v.sequence) == 0 {
		return helpRowText("no sequence entries — press a to add one", faint, v.termWidth)
	}
	return helpRowText("a add · d delete · J/K reorder · e edit · r render · c concat", faint, v.termWidth)
}

func timelineSliceLabel(raw string) string {
	slice := config.NormalizeTimelineSlice(raw)
	if slice == "" || slice == "start:end" {
		return "to end"
	}
	return slice
}

func (v timelineView) entryLabel(e project.TimelineEntry) string {
	if e.SourceFile != "" {
		return filepath.Base(e.SourceFile)
	}
	if c, ok := v.collections[e.Collection]; ok && e.Index >= 1 && e.Index <= len(c.Rows) {
		row := c.Rows[e.Index-1]
		title := sanitize(row.CustomFields["title"])
		artist := sanitize(row.CustomFields["artist"])
		if title != "" && artist != "" {
			return title + " – " + artist
		}
		if title != "" {
			return title
		}
		return filepath.Base(row.Link)
	}
	return e.Collection
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
	visible := v.resPanelHeight()
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
