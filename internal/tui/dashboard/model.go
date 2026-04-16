package dashboard

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/cache"
	"powerhour/internal/cachedoctor"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/state"
	renderstate "powerhour/internal/render/state"
	"powerhour/pkg/csvplan"
)

type tickMsg time.Time

// interactionMode tracks what the user is doing.
type interactionMode int

const (
	modeNormal        interactionMode = iota
	modeInput                         // text input active
	modeConfirmDelete                 // waiting for y/n
	modeInlineEdit                    // editing a row's fields inline
	modeAddClip                       // add-clip slot focused (paste link/path/CSV)
)

// Model is the top-level bubbletea model for the dashboard.
type Model struct {
	// Data.
	cfg         config.Config
	pp          paths.ProjectPaths
	collections map[string]project.Collection
	timeline    []project.TimelineEntry
	cacheIdx    *cache.Index
	renderState *state.RenderState

	// Sorted collection names (for view ordering).
	collectionNames []string

	// View names: ["Timeline", "Songs", "Interstitials", ...]
	viewNames []string

	// Active view index (0 = timeline).
	activeView int

	// Sub-views.
	timelineView    timelineView
	collectionViews []collectionView
	cacheView       cacheView
	toolsView       toolsView

	// Status summaries per collection.
	summaries map[string]collectionSummary

	// Cache status per row (for timeline dots).
	cacheStatus map[string]string

	// Tool warning text (empty = no warning).
	toolWarning string

	// Terminal size.
	termWidth  int
	termHeight int

	// Animation.
	tick int

	// Interaction state.
	mode       interactionMode
	input      textInput
	deleteDesc string // description of item to delete, shown in confirm prompt
	statusMsg  string // transient status message (e.g. error from write)

	// Inline edit state.
	editFieldIdx int    // which column is being edited (index into collectionView.columns)
	editValue    string // current edit buffer
	editOriginal string // original value before edit started
	editCursor   int    // caret position within editValue

	// Add-clip slot state.
	addBuffer string // paste/typed buffer for the active add-clip slot
	addCvIdx  int    // which collection view owns the active add slot

	// Overlay state.
	overlay       overlayKind
	toolStatuses  []ToolStatus
	doctorOverlay *cacheDoctorOverlay

	job dashboardJobState
}

type dashboardJobState struct {
	active bool
	label  string
	events chan dashboardJobEvent
}

type dashboardJobEvent interface{}

type jobRowStatusEvent struct {
	collectionIdx int
	rowIndex      int
	status        string
}

type jobCollectionStatusEvent struct {
	collectionIdx int
	status        string
}

type jobCacheRowStatusEvent struct {
	identifier string
	status     string
}

type jobCacheStatusEvent struct {
	status string
}

type jobCompletedEvent struct {
	label string
	err   error
}

type collectionSummary struct {
	Total        int
	Cached       int
	CacheMissing int
	Rendered     int
	Stale        int
	Missing      int
}

// NewModel creates the dashboard model from loaded project data.
func NewModel(cfg config.Config, pp paths.ProjectPaths, collections map[string]project.Collection, timeline []project.TimelineEntry, idx *cache.Index, rs *state.RenderState, toolWarning string, toolStatuses []ToolStatus) Model {
	// Sort collection names.
	names := make([]string, 0, len(collections))
	for name := range collections {
		names = append(names, name)
	}
	sort.Strings(names)

	// Build view names: Timeline + collections + Cache + Tools.
	viewNames := make([]string, 0, 3+len(names))
	viewNames = append(viewNames, "timeline")
	viewNames = append(viewNames, names...)
	viewNames = append(viewNames, "cache", "tools")

	// Build collection views.
	collViews := make([]collectionView, len(names))
	for i, name := range names {
		collViews[i] = newCollectionView(collections[name], pp, cfg, idx)
	}

	// Build summaries and cache status.
	summaries := buildSummaries(collections, names, idx, pp)
	cacheStatus := buildCacheStatus(collections, idx, pp)

	m := Model{
		cfg:             cfg,
		pp:              pp,
		collections:     collections,
		timeline:        timeline,
		cacheIdx:        idx,
		renderState:     rs,
		collectionNames: names,
		viewNames:       viewNames,
		timelineView:    newTimelineView(cfg, timeline, collections, names, pp.Root),
		collectionViews: collViews,
		summaries:       summaries,
		cacheStatus:     cacheStatus,
		toolWarning:     toolWarning,
		toolStatuses:    toolStatuses,
		cacheView:       newCacheView(idx, buildCollectionLinks(collections)),
		toolsView:       newToolsView(toolStatuses),
	}

	return m
}

// viewKind returns what type of view is at the given index.
func (m Model) viewKind(idx int) string {
	if idx == 0 {
		return "timeline"
	}
	if idx >= 1 && idx <= len(m.collectionNames) {
		return "collection"
	}
	if idx == len(m.collectionNames)+1 {
		return "cache"
	}
	if idx == len(m.collectionNames)+2 {
		return "tools"
	}
	return ""
}

// collectionViewIndex returns the collection view slice index for the given active view.
func (m Model) collectionViewIndex() int {
	return m.activeView - 1
}

func (m Model) toolStatus(name string) (ToolStatus, bool) {
	for _, status := range m.toolStatuses {
		if strings.EqualFold(status.Name, name) {
			return status, true
		}
	}
	return ToolStatus{}, false
}

func (m Model) vlcPath() string {
	status, ok := m.toolStatus("vlc")
	if !ok {
		return ""
	}
	return status.Path
}

func (m Model) vlcAvailable() bool {
	return m.vlcPath() != ""
}

func (m Model) setCollectionRowNote(cvIdx int, rowIndex int, note string) Model {
	if cvIdx < 0 || cvIdx >= len(m.collectionViews) {
		return m
	}
	if m.collectionViews[cvIdx].rowStatus == nil {
		m.collectionViews[cvIdx].rowStatus = make(map[int]string)
	}
	if m.collectionViews[cvIdx].rowStatusUntil == nil {
		m.collectionViews[cvIdx].rowStatusUntil = make(map[int]int)
	}
	m.collectionViews[cvIdx].rowStatus[rowIndex] = "note:" + note
	m.collectionViews[cvIdx].rowStatusUntil[rowIndex] = m.tick + 14
	return m
}

func (m Model) setCollectionCursorNote(cvIdx int, note string) Model {
	if cvIdx < 0 || cvIdx >= len(m.collectionViews) {
		m.statusMsg = note
		return m
	}
	v := m.collectionViews[cvIdx]
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		m.statusMsg = note
		return m
	}
	return m.setCollectionRowNote(cvIdx, v.rows[v.cursor].Index, note)
}

func (m Model) setCacheRowNote(identifier string, note string) Model {
	if strings.TrimSpace(identifier) == "" {
		return m
	}
	if m.cacheView.rowStatus == nil {
		m.cacheView.rowStatus = make(map[string]string)
	}
	if m.cacheView.rowStatusUntil == nil {
		m.cacheView.rowStatusUntil = make(map[string]int)
	}
	m.cacheView.rowStatus[identifier] = "note:" + note
	m.cacheView.rowStatusUntil[identifier] = m.tick + 14
	return m
}

func (m Model) setCacheCursorNote(note string) Model {
	entries := m.cacheView.entries()
	if m.cacheView.cursor < 0 || m.cacheView.cursor >= len(entries) {
		m.statusMsg = note
		return m
	}
	return m.setCacheRowNote(entries[m.cacheView.cursor].Identifier, note)
}

func (m Model) setTimelineSequenceNote(seqIdx int, note string) Model {
	if seqIdx < 0 || seqIdx >= len(m.timelineView.sequence) {
		m.statusMsg = note
		return m
	}
	if m.timelineView.seqStatus == nil {
		m.timelineView.seqStatus = make(map[int]string)
	}
	if m.timelineView.seqStatusUntil == nil {
		m.timelineView.seqStatusUntil = make(map[int]int)
	}
	m.timelineView.seqStatus[seqIdx] = "note:" + note
	m.timelineView.seqStatusUntil[seqIdx] = m.tick + 14
	return m
}

func (m Model) expireTransientRowNotes() Model {
	for seqIdx, until := range m.timelineView.seqStatusUntil {
		if until > m.tick {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(m.timelineView.seqStatus[seqIdx]), "note:") {
			delete(m.timelineView.seqStatus, seqIdx)
		}
		delete(m.timelineView.seqStatusUntil, seqIdx)
	}
	for i := range m.collectionViews {
		for rowIndex, until := range m.collectionViews[i].rowStatusUntil {
			if until > m.tick {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(m.collectionViews[i].rowStatus[rowIndex]), "note:") {
				delete(m.collectionViews[i].rowStatus, rowIndex)
			}
			delete(m.collectionViews[i].rowStatusUntil, rowIndex)
		}
	}
	for identifier, until := range m.cacheView.rowStatusUntil {
		if until > m.tick {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(m.cacheView.rowStatus[identifier]), "note:") {
			delete(m.cacheView.rowStatus, identifier)
		}
		delete(m.cacheView.rowStatusUntil, identifier)
	}
	return m
}

func scheduleDashTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd {
	return scheduleDashTick()
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.timelineView.termWidth = msg.Width
		m.timelineView.termHeight = msg.Height
		for i := range m.collectionViews {
			m.collectionViews[i].termWidth = msg.Width
			m.collectionViews[i].termHeight = msg.Height
			m.collectionViews[i].tick = m.tick
		}
		m.cacheView.termWidth = msg.Width
		m.cacheView.termHeight = msg.Height
		m.toolsView.termWidth = msg.Width
		if m.doctorOverlay != nil {
			m.doctorOverlay.termWidth = msg.Width
			m.doctorOverlay.termHeight = msg.Height
		}
		return m, nil

	case tickMsg:
		m.tick++
		for i := range m.collectionViews {
			m.collectionViews[i].tick = m.tick
		}
		if m.doctorOverlay != nil {
			m.doctorOverlay.tick = m.tick
		}
		m = m.expireTransientRowNotes()
		m = m.drainJobEvents()
		return m, scheduleDashTick()

	case renderDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Render error: %v", msg.err)
		} else if msg.status != "" {
			m.statusMsg = msg.status
		}
		m = reloadState(m)
		return m, nil

	case concatDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Concat error: %v", msg.err)
		}
		m = reloadState(m)
		m.timelineView.concatPath, m.timelineView.concatExists, m.timelineView.concatSize = findConcatOutput(m.pp.Root)
		return m, nil

	case fetchDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Fetch error: %v", msg.err)
		} else {
			m.statusMsg = msg.status
		}
		m = reloadState(m)
		return m, nil

	case metadataProbeMsg:
		cvIdx := msg.collectionIdx
		if cvIdx >= 0 && cvIdx < len(m.collectionViews) {
			v := m.collectionViews[cvIdx]
			// Find the target row by matching the link URL, not by index.
			// This is safe against add/delete races that shift row positions.
			rowFound := false
			rowIndex := 0
			for ri := range v.rows {
				if strings.TrimSpace(v.rows[ri].Link) == msg.link {
					rowIndex = v.rows[ri].Index
					if msg.err == nil {
						if msg.title != "" {
							v.rows[ri].CustomFields["title"] = msg.title
						}
						if msg.artist != "" {
							v.rows[ri].CustomFields["artist"] = msg.artist
						}
					}
					rowFound = true
					break
				}
			}
			if rowFound && msg.err == nil {
				m.collectionViews[cvIdx] = v
				collName := m.collectionNames[cvIdx]
				m.collectionViews[cvIdx].columns = discoverColumns(v.rows, m.collections[collName].Headers)
				m = writeCollection(m, cvIdx)
			}
			if rowFound {
				if msg.err != nil {
					m = m.setCollectionRowNote(cvIdx, rowIndex, fmt.Sprintf("probe failed: %v", msg.err))
				} else {
					note := strings.Trim(strings.Join([]string{msg.title, msg.artist}, " - "), " -")
					if note == "" {
						note = "metadata updated"
					}
					m = m.setCollectionRowNote(cvIdx, rowIndex, note)
				}
			} else if msg.err != nil {
				m.statusMsg = fmt.Sprintf("Probe failed: %v", msg.err)
			}
		}
		return m, nil

	case doctorRequeryDoneMsg:
		if m.doctorOverlay != nil {
			if msg.err != nil {
				m.doctorOverlay.requerying = false
				m.statusMsg = fmt.Sprintf("Requery failed: %v", msg.err)
			} else {
				normCfg := cache.LoadNormalizationConfig()
				m.doctorOverlay.applyRequery(msg.info, normCfg)
			}
		}
		return m, nil

	case editorDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Editor error: %v", msg.err)
		} else {
			m = reloadCollection(m, msg.collectionIdx)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Clear transient status on any keypress.
	if !m.job.active {
		m.statusMsg = ""
	}

	// Overlay input handling.
	if m.overlay != overlayNone {
		if m.overlay == overlayDoctor && m.doctorOverlay != nil {
			if isRequeryKey(msg) && !m.doctorOverlay.requerying {
				return m.startDoctorRequery()
			}
			done, applyNow := m.doctorOverlay.handleKey(msg)
			if applyNow {
				m = m.applyCurrentDoctorEntry()
			}
			if done {
				m.overlay = overlayNone
				m.doctorOverlay = nil
			}
			return m, nil
		}
		// Non-input overlay (help). q/Esc/Ctrl+C quit so the root-level
		// contract holds anywhere text input is not active; `?` closes it.
		key := msg.String()
		switch key {
		case "ctrl+c", "esc", "escape", "q":
			return m, tea.Quit
		case "?":
			m.overlay = overlayNone
		}
		return m, nil
	}

	// Route based on interaction mode.
	switch m.mode {
	case modeInput:
		return m.handleInputKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmDeleteKey(msg)
	case modeInlineEdit:
		return m.handleInlineEditKey(msg)
	case modeAddClip:
		return m.handleAddClipKey(msg)
	}

	if m.job.active {
		key := msg.String()
		switch key {
		case "ctrl+c", "esc", "q":
			return m, tea.Quit
		default:
			return m, nil
		}
	}

	key := msg.String()

	// Global keys.
	switch key {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "?":
		m.overlay = overlayHelp
		return m, nil
	case "c":
		c := execCommand("powerhour", "concat", "--project", m.pp.Root)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return concatDoneMsg{err: err}
		})
	case "o":
		name, args := revealCommand(m.pp.Root)
		_ = execCommand(name, args...).Start()
		return m, nil
	case "u", "ctrl+r":
		return m.refreshFromDisk(), nil
	}

	// Left/right arrow keys cycle through views.
	switch key {
	case "right", "l":
		m.activeView = (m.activeView + 1) % len(m.viewNames)
		return m, nil
	case "left", "h":
		m.activeView = (m.activeView - 1 + len(m.viewNames)) % len(m.viewNames)
		return m, nil
	}

	// Number key view switching (1-9).
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		idx := int(key[0] - '1')
		if idx < len(m.viewNames) {
			m.activeView = idx
			return m, nil
		}
	}

	// Delegate to active view.
	switch m.viewKind(m.activeView) {
	case "timeline":
		return m.handleTimelineKeyWithMutations(msg)
	case "collection":
		return m.handleCollectionKeyWithMutations(m.collectionViewIndex(), msg)
	case "cache":
		return m.handleCacheKey(msg)
	}

	return m, nil
}

func (m Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ti, result := m.input.update(msg)
	m.input = ti

	if result.cancelled {
		m.mode = modeNormal
		return m, nil
	}

	if result.submitted {
		m.mode = modeNormal
		if m.activeView == 0 {
			return m.processAddTimelineEntry(result.value)
		}
		return m.processAddRow(result.value)
	}

	return m, nil
}

func (m Model) handleConfirmDeleteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "y", "Y", "enter":
		m.mode = modeNormal
		if m.viewKind(m.activeView) == "cache" {
			return m.processDeleteCacheEntry()
		}
		if m.activeView == 0 {
			return m.processDeleteTimelineEntry()
		}
		return m.processDeleteRow()
	default:
		m.mode = modeNormal
		return m, nil
	}
}

func (m Model) handleInlineEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cvIdx := m.activeView - 1
	if cvIdx < 0 || cvIdx >= len(m.collectionViews) {
		m.mode = modeNormal
		return m, nil
	}

	v := m.collectionViews[cvIdx]
	cols := v.columns
	if len(cols) == 0 || len(v.rows) == 0 {
		m.mode = modeNormal
		return m, nil
	}

	// Helper to commit the current field value.
	commitField := func() {
		field := cols[m.editFieldIdx].field
		v.rows[v.cursor].CustomFields[field] = m.editValue
		// Also update StartRaw if editing start_time.
		if field == "start_time" {
			v.rows[v.cursor].StartRaw = m.editValue
		}
		m.collectionViews[cvIdx] = v
	}

	// Helper to load a field into the edit buffer.
	loadField := func() {
		field := cols[m.editFieldIdx].field
		m.editValue = v.rows[v.cursor].CustomFields[field]
		m.editOriginal = m.editValue
		m.editCursor = len(m.editValue)
	}

	switch msg.Type {
	case tea.KeyEscape:
		field := cols[m.editFieldIdx].field
		v.rows[v.cursor].CustomFields[field] = m.editOriginal
		v.editing = false
		m.collectionViews[cvIdx] = v
		m.mode = modeNormal
		return m, nil

	case tea.KeyEnter:
		commitField()
		v.editing = false
		m.collectionViews[cvIdx] = v
		m.mode = modeNormal
		m = writeCollection(m, cvIdx)
		m = reResolve(m)
		m = m.setCollectionRowNote(cvIdx, v.rows[v.cursor].Index, "saved")
		return m, nil

	case tea.KeyRight:
		if m.editCursor < len(m.editValue) {
			m.editCursor++
		}
		v.editFieldIdx = m.editFieldIdx
		v.editValue = m.editValue
		v.editCursor = m.editCursor
		m.collectionViews[cvIdx] = v
		return m, nil

	case tea.KeyLeft:
		if m.editCursor > 0 {
			m.editCursor--
		}
		v.editFieldIdx = m.editFieldIdx
		v.editValue = m.editValue
		v.editCursor = m.editCursor
		m.collectionViews[cvIdx] = v
		return m, nil

	case tea.KeyDown:
		commitField()
		m = writeCollection(m, cvIdx)
		v = m.collectionViews[cvIdx]
		if v.cursor < len(v.rows)-1 {
			v.cursor++
			v.autoScroll()
		}
		loadField()
		v.editFieldIdx = m.editFieldIdx
		v.editValue = m.editValue
		v.editCursor = m.editCursor
		m.collectionViews[cvIdx] = v
		return m, nil

	case tea.KeyUp:
		commitField()
		m = writeCollection(m, cvIdx)
		v = m.collectionViews[cvIdx]
		if v.cursor > 0 {
			v.cursor--
			v.autoScroll()
		}
		loadField()
		v.editFieldIdx = m.editFieldIdx
		v.editValue = m.editValue
		v.editCursor = m.editCursor
		m.collectionViews[cvIdx] = v
		return m, nil

	case tea.KeyBackspace:
		if m.editCursor > 0 && len(m.editValue) > 0 {
			m.editValue = m.editValue[:m.editCursor-1] + m.editValue[m.editCursor:]
			m.editCursor--
		}
		m.collectionViews[cvIdx].editValue = m.editValue
		m.collectionViews[cvIdx].editCursor = m.editCursor
		return m, nil

	case tea.KeyRunes:
		ch := string(msg.Runes)
		m.editValue = m.editValue[:m.editCursor] + ch + m.editValue[m.editCursor:]
		m.editCursor += len(ch)
		m.collectionViews[cvIdx].editValue = m.editValue
		m.collectionViews[cvIdx].editCursor = m.editCursor
		return m, nil

	case tea.KeyTab:
		commitField()
		m.editFieldIdx++
		if m.editFieldIdx >= len(cols) {
			m.editFieldIdx = 0
		}
		loadField()
		v.editFieldIdx = m.editFieldIdx
		v.editValue = m.editValue
		v.editCursor = m.editCursor
		m.collectionViews[cvIdx] = v
		return m, nil

	case tea.KeyShiftTab:
		commitField()
		m.editFieldIdx--
		if m.editFieldIdx < 0 {
			m.editFieldIdx = len(cols) - 1
		}
		loadField()
		v.editFieldIdx = m.editFieldIdx
		v.editValue = m.editValue
		v.editCursor = m.editCursor
		m.collectionViews[cvIdx] = v
		return m, nil
	}

	return m, nil
}

// handleAddClipKey drives the persistent Add Clip slot.
func (m Model) handleAddClipKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cvIdx := m.addCvIdx
	if cvIdx < 0 || cvIdx >= len(m.collectionViews) {
		m.mode = modeNormal
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEscape:
		return m.cancelAddClip(), nil

	case tea.KeyEnter:
		return m.dispatchAddBuffer(cvIdx, m.addBuffer)

	case tea.KeyBackspace:
		if len(m.addBuffer) > 0 {
			r := []rune(m.addBuffer)
			m.addBuffer = string(r[:len(r)-1])
		}
		m.syncAddClipBuffer(cvIdx)
		return m, nil

	case tea.KeyRunes:
		m.addBuffer += string(msg.Runes)
		m.syncAddClipBuffer(cvIdx)
		return m, nil

	case tea.KeySpace:
		m.addBuffer += " "
		m.syncAddClipBuffer(cvIdx)
		return m, nil
	}

	return m, nil
}

func (m Model) syncAddClipBuffer(cvIdx int) {
	if cvIdx >= 0 && cvIdx < len(m.collectionViews) {
		m.collectionViews[cvIdx].addBuffer = m.addBuffer
	}
}

func (m Model) resetAddClipInput(cvIdx int, keepFocus bool) {
	m.addBuffer = ""
	if cvIdx >= 0 && cvIdx < len(m.collectionViews) {
		m.collectionViews[cvIdx].addFocus = keepFocus
		m.collectionViews[cvIdx].addBuffer = ""
	}
}

// cancelAddClip returns to normal mode, clearing the slot focus.
func (m Model) cancelAddClip() Model {
	m.resetAddClipInput(m.addCvIdx, false)
	m.mode = modeNormal
	return m
}

// dispatchAddBuffer inspects the buffered text and routes it to the appropriate
// import path: multi-line / CSV-ish → batch import; single URL or single line → one new row.
func (m Model) dispatchAddBuffer(cvIdx int, value string) (tea.Model, tea.Cmd) {
	value = strings.TrimSpace(value)
	if value == "" {
		return m.cancelAddClip(), nil
	}

	collName := m.collectionNames[cvIdx]
	coll := m.collections[collName]

	// Multi-line or CSV/TSV/YAML batch import.
	if looksLikeBatchImport(value) {
		rows, format, err := csvplan.ImportCollectionText(value, project.CollectionOptionsForConfig(coll))
		if err != nil {
			m = m.setCollectionCursorNote(cvIdx, fmt.Sprintf("import failed: %v", err))
			m.resetAddClipInput(cvIdx, true)
			return m, nil
		}
		coll = project.AppendCollectionRows(coll, rows)
		if err := project.WriteCollectionPlan(coll); err != nil {
			m = m.setCollectionCursorNote(cvIdx, fmt.Sprintf("write error: %v", err))
			m.resetAddClipInput(cvIdx, true)
			return m, nil
		}
		m.collections[collName] = coll
		m = reloadCollection(m, cvIdx)
		if len(rows) > 0 {
			m.collectionViews[cvIdx].cursor = len(m.collectionViews[cvIdx].rows) - 1
			m.collectionViews[cvIdx].autoScroll()
			m = m.setCollectionCursorNote(cvIdx, fmt.Sprintf("imported %d rows from %s", len(rows), format))
		}
		// Stay in modeAddClip with an empty buffer so another paste is ready.
		m.resetAddClipInput(cvIdx, true)
		return m, nil
	}

	// Single-line entry: treat as URL/path.
	value = cleanYouTubeURL(value)

	defaultDur := 60
	if coll.Config.Duration > 0 {
		defaultDur = coll.Config.Duration
	}

	v := m.collectionViews[cvIdx]
	newRow := csvplan.CollectionRow{
		Index:           len(v.rows) + 1,
		Link:            value,
		StartRaw:        "0:00",
		DurationSeconds: defaultDur,
		CustomFields:    make(map[string]string),
	}
	linkHeader := coll.Config.LinkHeader
	if linkHeader == "" {
		linkHeader = "link"
	}
	startHeader := coll.Config.StartHeader
	if startHeader == "" {
		startHeader = "start_time"
	}
	durationHeader := coll.Config.DurationHeader
	if durationHeader == "" {
		durationHeader = "duration"
	}
	newRow.CustomFields[linkHeader] = value
	newRow.CustomFields[startHeader] = "0:00"
	newRow.CustomFields[durationHeader] = fmt.Sprintf("%d", defaultDur)

	v.rows = append(v.rows, newRow)
	v.cursor = len(v.rows) - 1
	v.addFocus = false
	v.addBuffer = ""
	v.autoScroll()
	m.collectionViews[cvIdx] = v

	m = writeCollection(m, cvIdx)
	m = reResolve(m)
	m.addBuffer = ""

	// Drop into inline edit on the new row so the user can adjust columns.
	m = m.enterInlineEditOn(cvIdx, len(v.rows)-1, 0)

	// Kick off async yt-dlp probe for URLs.
	if isURL(value) {
		m = m.setCollectionCursorNote(cvIdx, "probing metadata...")
		return m, probeMetadata(value, cvIdx)
	}
	m = m.setCollectionCursorNote(cvIdx, "added")

	return m, nil
}

// enterInlineEditOn puts the model into modeInlineEdit on the given row + field.
func (m Model) enterInlineEditOn(cvIdx, rowIdx, fieldIdx int) Model {
	if cvIdx < 0 || cvIdx >= len(m.collectionViews) {
		return m
	}
	v := m.collectionViews[cvIdx]
	if rowIdx < 0 || rowIdx >= len(v.rows) {
		return m
	}
	cols := v.columns
	if len(cols) == 0 {
		return m
	}
	if fieldIdx < 0 || fieldIdx >= len(cols) {
		fieldIdx = 0
	}
	v.cursor = rowIdx
	v.autoScroll()
	v.editing = true
	v.editFieldIdx = fieldIdx
	field := cols[fieldIdx].field
	v.editValue = v.rows[rowIdx].CustomFields[field]
	v.editCursor = len(v.editValue)
	m.editFieldIdx = fieldIdx
	m.editValue = v.editValue
	m.editOriginal = v.editValue
	m.editCursor = v.editCursor
	m.collectionViews[cvIdx] = v
	m.mode = modeInlineEdit
	return m
}

func (m Model) handleCollectionKeyWithMutations(cvIdx int, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.collectionViews[cvIdx]
	key := msg.String()

	switch key {
	case "up", "k":
		if v.cursor > 0 {
			v.cursor--
			v.autoScroll()
		}
		m.collectionViews[cvIdx] = v
		return m, nil

	case "down", "j":
		if v.cursor < len(v.rows)-1 {
			v.cursor++
			v.autoScroll()
		}
		m.collectionViews[cvIdx] = v
		return m, nil

	case "J", "shift+down":
		if v.cursor < len(v.rows)-1 {
			v.rows[v.cursor], v.rows[v.cursor+1] = v.rows[v.cursor+1], v.rows[v.cursor]
			v.cursor++
			v.autoScroll()
			m.collectionViews[cvIdx] = v
			m = reindexAndWriteCollection(m, cvIdx)
			return m, nil
		}
		return m, nil

	case "K", "shift+up":
		if v.cursor > 0 {
			v.rows[v.cursor], v.rows[v.cursor-1] = v.rows[v.cursor-1], v.rows[v.cursor]
			v.cursor--
			v.autoScroll()
			m.collectionViews[cvIdx] = v
			m = reindexAndWriteCollection(m, cvIdx)
			return m, nil
		}
		return m, nil

	case "a":
		m.mode = modeAddClip
		m.addCvIdx = cvIdx
		m.resetAddClipInput(cvIdx, true)
		return m, nil

	case "d":
		return m.processDuplicateRow(cvIdx), nil

	case "x":
		if len(v.rows) == 0 {
			return m, nil
		}
		row := v.rows[v.cursor]
		title := sanitize(row.CustomFields["title"])
		if title == "" {
			title = sanitize(row.Link)
		}
		m.deleteDesc = fmt.Sprintf("row %d %q", row.Index, title)
		m.mode = modeConfirmDelete
		return m, nil

	case "e":
		if len(v.rows) == 0 {
			return m, nil
		}
		m.mode = modeInlineEdit
		m.editFieldIdx = 0
		cols := v.columns
		if len(cols) > 0 {
			field := cols[0].field
			m.editValue = v.rows[v.cursor].CustomFields[field]
			m.editOriginal = m.editValue
			m.editCursor = len(m.editValue)
		}
		m.collectionViews[cvIdx].editing = true
		m.collectionViews[cvIdx].editFieldIdx = m.editFieldIdx
		m.collectionViews[cvIdx].editValue = m.editValue
		m.collectionViews[cvIdx].editCursor = m.editCursor
		return m, nil

	case "E":
		collName := m.collectionNames[cvIdx]
		coll := m.collections[collName]
		if coll.Plan == "" {
			return m, nil
		}
		c := execCommand("open", coll.Plan)
		c.Start()
		m.statusMsg = fmt.Sprintf("Opened %s — press u to refresh", filepath.Base(coll.Plan))
		return m, nil

	case "f":
		if len(v.rows) == 0 {
			return m, nil
		}
		row := v.rows[v.cursor]
		return m.startCollectionFetchJob(cvIdx, []csvplan.CollectionRow{row}, false), nil

	case "F":
		return m.startCollectionFetchJob(cvIdx, append([]csvplan.CollectionRow(nil), v.rows...), true), nil

	case "r":
		if len(v.rows) == 0 {
			return m, nil
		}
		row := v.rows[v.cursor]
		return m.startCollectionRenderJob(cvIdx, []csvplan.CollectionRow{row}, false), nil

	case "R":
		return m.startCollectionRenderJob(cvIdx, append([]csvplan.CollectionRow(nil), v.rows...), true), nil

	case "v":
		if !m.vlcAvailable() {
			m.statusMsg = "vlc not found — install VLC to preview clips"
			return m, nil
		}
		if len(v.rows) == 0 {
			return m, nil
		}
		vlcPath := m.vlcPath()
		collName := m.collectionNames[cvIdx]
		coll := m.collections[collName]
		row := v.rows[v.cursor]
		m = m.setCollectionRowNote(cvIdx, row.Index, "opening vlc...")
		segPath := resolveRenderedSegmentPath(m.pp, m.cfg, collName, coll, row)
		if _, err := os.Stat(segPath); err == nil {
			if err := playFileInVLC(vlcPath, segPath); err != nil {
				m.statusMsg = fmt.Sprintf("vlc error: %v", err)
			}
		} else {
			srcPath := m.resolveVideoPath(row)
			if srcPath != "" {
				stopSeconds := row.Start.Seconds()
				if row.DurationSeconds > 0 {
					stopSeconds += float64(row.DurationSeconds)
				}
				if err := playClipInVLC(vlcPath, srcPath, row.Start.Seconds(), stopSeconds); err != nil {
					m.statusMsg = fmt.Sprintf("vlc error: %v", err)
				}
			} else {
				m.statusMsg = "No rendered or cached file found"
			}
		}
		return m, nil

	case "V":
		if !m.vlcAvailable() {
			m.statusMsg = "vlc not found — install VLC to preview clips"
			return m, nil
		}
		if len(v.rows) == 0 {
			return m, nil
		}
		vlcPath := m.vlcPath()
		collName := m.collectionNames[cvIdx]
		coll := m.collections[collName]
		m.collectionViews[cvIdx].activity = "opening vlc..."
		var allPaths []string
		for _, row := range v.rows {
			allPaths = append(allPaths, resolveRenderedSegmentPath(m.pp, m.cfg, collName, coll, row))
		}
		tmpDir := filepath.Join(m.pp.MetaDir, "tmp")
		_, _, err := playPlaylistInVLC(vlcPath, allPaths, tmpDir)
		if err != nil {
			m.statusMsg = fmt.Sprintf("vlc error: %v", err)
		}
		m.collectionViews[cvIdx].activity = ""
		return m, nil
	}

	m.collectionViews[cvIdx] = v
	return m, nil
}

func (m Model) handleTimelineKeyWithMutations(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.timelineView
	key := msg.String()

	switch key {
	case "r":
		c := execCommand("powerhour", "render", "--project", m.pp.Root)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return renderDoneMsg{err: err}
		})

	case "up", "k":
		if v.concatFocus {
			v.concatFocus = false
			if len(v.sequence) > 0 {
				v.seqCursor = len(v.sequence) - 1
				v.autoScrollSeq()
			}
		} else if v.focusPanel == 0 {
			if v.seqCursor > 0 {
				v.seqCursor--
				v.autoScrollSeq()
			}
		} else {
			if v.resScrollTop > 0 {
				v.resScrollTop--
			}
		}
		m.timelineView = v
		return m, nil

	case "down", "j":
		if v.concatFocus {
			// Already at bottom, do nothing.
		} else if v.focusPanel == 0 {
			if v.seqCursor < len(v.sequence)-1 {
				v.seqCursor++
				v.autoScrollSeq()
			} else {
				// Move to concat row.
				v.concatFocus = true
			}
		} else {
			maxScroll := len(v.resolved) - v.resPanelHeight()
			if maxScroll < 0 {
				maxScroll = 0
			}
			if v.resScrollTop < maxScroll {
				v.resScrollTop++
			}
		}
		m.timelineView = v
		return m, nil

	case "J", "shift+down":
		if v.concatFocus {
			return m, nil
		}
		if v.focusPanel == 0 && v.seqCursor < len(v.sequence)-1 {
			v.sequence[v.seqCursor], v.sequence[v.seqCursor+1] = v.sequence[v.seqCursor+1], v.sequence[v.seqCursor]
			v.seqCursor++
			v.autoScrollSeq()
			m.timelineView = v
			m.cfg.Timeline.Sequence = v.sequence
			m = saveConfigAndReResolve(m)
			return m, nil
		}
		return m, nil

	case "K", "shift+up":
		if v.concatFocus {
			return m, nil
		}
		if v.focusPanel == 0 && v.seqCursor > 0 {
			v.sequence[v.seqCursor], v.sequence[v.seqCursor-1] = v.sequence[v.seqCursor-1], v.sequence[v.seqCursor]
			v.seqCursor--
			v.autoScrollSeq()
			m.timelineView = v
			m.cfg.Timeline.Sequence = v.sequence
			m = saveConfigAndReResolve(m)
			return m, nil
		}
		return m, nil

	case "x":
		if v.concatFocus {
			return m, nil
		}
		if v.focusPanel == 0 && len(v.sequence) > 0 {
			entry := v.sequence[v.seqCursor]
			desc := "sequence entry"
			if entry.File != "" {
				desc = fmt.Sprintf("file: %s", entry.File)
			} else if entry.Collection != "" {
				desc = fmt.Sprintf("%s × %d", entry.Collection, entry.Count)
			}
			m.deleteDesc = desc
			m.mode = modeConfirmDelete
			return m, nil
		}
		return m, nil

	case "a":
		if v.concatFocus {
			return m, nil
		}
		if v.focusPanel == 0 {
			m.mode = modeInput
			m.input = newTextInput("Add sequence entry — [c]ollection or [f]ile path:")
			return m, nil
		}
		return m, nil

	case "v":
		if !m.vlcAvailable() {
			m.statusMsg = "vlc not found — install VLC to preview clips"
			return m, nil
		}
		vlcPath := m.vlcPath()
		// Concat row: play the concat output.
		if v.concatFocus {
			if v.concatExists {
				if err := playFileInVLC(vlcPath, v.concatPath); err != nil {
					m.statusMsg = fmt.Sprintf("vlc error: %v", err)
				}
			} else {
				m.statusMsg = "Not yet exported — press c to concatenate"
			}
			return m, nil
		}
		if v.focusPanel == 0 && len(v.sequence) > 0 {
			paths := resolveSequenceEntrySegmentPaths(m.pp, m.cfg, m.collections, v.seqCursor)
			if len(paths) == 0 {
				m.statusMsg = "No segments for this entry"
				return m, nil
			}
			if len(paths) == 1 {
				if _, err := os.Stat(paths[0]); err == nil {
					if err := playFileInVLC(vlcPath, paths[0]); err != nil {
						m.statusMsg = fmt.Sprintf("vlc error: %v", err)
					}
				} else {
					m.statusMsg = "Segment not yet rendered"
				}
			} else {
				tmpDir := filepath.Join(m.pp.MetaDir, "tmp")
				_, _, err := playPlaylistInVLC(vlcPath, paths, tmpDir)
				if err != nil {
					m.statusMsg = fmt.Sprintf("vlc error: %v", err)
				}
			}
		}
		return m, nil

	case "V":
		if !m.vlcAvailable() {
			m.statusMsg = "vlc not found — install VLC to preview clips"
			return m, nil
		}
		vlcPath := m.vlcPath()
		// Concat row: V plays just the concat file (same as v).
		if v.concatFocus {
			if v.concatExists {
				if err := playFileInVLC(vlcPath, v.concatPath); err != nil {
					m.statusMsg = fmt.Sprintf("vlc error: %v", err)
				}
			} else {
				m.statusMsg = "Not yet exported — press c to concatenate"
			}
			return m, nil
		}
		allPaths := resolveAllTimelineSegmentPaths(m.pp, m.cfg, m.collections)
		if len(allPaths) == 0 {
			m.statusMsg = "No timeline segments found"
			return m, nil
		}
		tmpDir := filepath.Join(m.pp.MetaDir, "tmp")
		_, _, err := playPlaylistInVLC(vlcPath, allPaths, tmpDir)
		if err != nil {
			m.statusMsg = fmt.Sprintf("vlc error: %v", err)
		}
		return m, nil
	}

	m.timelineView = v
	return m, nil
}

func (m Model) handleCacheKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.cacheView
	entries := v.entries()
	key := msg.String()

	switch key {
	case "up", "k":
		if v.cursor > 0 {
			v.cursor--
			v.autoScroll()
		}
	case "down", "j":
		if v.cursor < len(entries)-1 {
			v.cursor++
			v.autoScroll()
		}
	case "f":
		v.toggle()
	case "d":
		if len(entries) == 0 {
			m.cacheView = v
			return m, nil
		}
		entry := entries[v.cursor]
		if strings.TrimSpace(entry.Identifier) == "" {
			m.statusMsg = "No cache identifier for selected entry"
			m.cacheView = v
			return m, nil
		}
		m.cacheView = v
		return m.openDoctorOverlay([]cacheEntry{entry}), nil
	case "D":
		if len(entries) == 0 {
			m.cacheView = v
			return m, nil
		}
		m.cacheView = v
		return m.openDoctorOverlay(append([]cacheEntry(nil), entries...)), nil
	case "x":
		if len(entries) == 0 || v.cursor >= len(entries) {
			m.cacheView = v
			return m, nil
		}
		entry := entries[v.cursor]
		title := entry.Title
		if title == "" {
			title = filepath.Base(entry.CachedPath)
		}
		m.deleteDesc = fmt.Sprintf("cache entry %q", title)
		m.mode = modeConfirmDelete
		m.cacheView = v
		return m, nil
	case "v":
		if !m.vlcAvailable() {
			m.statusMsg = "vlc not found — install VLC to preview clips"
			m.cacheView = v
			return m, nil
		}
		if len(entries) == 0 {
			m.cacheView = v
			return m, nil
		}
		vlcPath := m.vlcPath()
		entry := entries[v.cursor]
		m = m.setCacheRowNote(entry.Identifier, "opening vlc...")
		if entry.CachedPath != "" {
			if err := playFileInVLC(vlcPath, entry.CachedPath); err != nil {
				m.statusMsg = fmt.Sprintf("vlc error: %v", err)
			}
		} else {
			m.statusMsg = "No cached file"
		}
	case "V":
		if !m.vlcAvailable() {
			m.statusMsg = "vlc not found — install VLC to preview clips"
			m.cacheView = v
			return m, nil
		}
		if len(entries) == 0 {
			m.cacheView = v
			return m, nil
		}
		var allPaths []string
		for _, e := range entries {
			if e.CachedPath != "" {
				allPaths = append(allPaths, e.CachedPath)
			}
		}
		vlcPath := m.vlcPath()
		m.cacheView.activity = "opening vlc..."
		tmpDir := filepath.Join(m.pp.MetaDir, "tmp")
		_, _, err := playPlaylistInVLC(vlcPath, allPaths, tmpDir)
		if err != nil {
			m.statusMsg = fmt.Sprintf("vlc error: %v", err)
		}
		m.cacheView.activity = ""
	}

	m.cacheView = v
	return m, nil
}

func (v *cacheView) autoScroll() {
	visible := v.visibleRowCount()
	if v.cursor < v.scrollTop {
		v.scrollTop = v.cursor
	} else if v.cursor >= v.scrollTop+visible {
		v.scrollTop = v.cursor - visible + 1
	}
}

func (m Model) openDoctorOverlay(entries []cacheEntry) Model {
	idx, err := cache.Load(m.pp)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Cache load error: %v", err)
		return m
	}
	normCfg := cache.LoadNormalizationConfig()
	knownArtists := cachedoctor.BuildKnownArtists(idx, normCfg)

	var items []doctorItem
	for _, viewEntry := range entries {
		identifier := strings.TrimSpace(viewEntry.Identifier)
		if identifier == "" {
			continue
		}
		entry, ok := idx.GetByIdentifier(identifier)
		if !ok {
			continue
		}
		finding, needsFix, err := cachedoctor.InspectEntry(context.Background(), nil, normCfg, knownArtists, entry, false)
		if err != nil {
			continue
		}
		if !needsFix {
			continue
		}
		items = append(items, doctorItem{entry: entry, finding: finding})
	}
	if len(items) == 0 {
		m.statusMsg = "All entries look clean"
		return m
	}
	overlay := newCacheDoctorOverlay(items, knownArtists, m.termWidth, m.termHeight)
	m.doctorOverlay = &overlay
	m.overlay = overlayDoctor
	return m
}

type doctorRequeryDoneMsg struct {
	info cache.RemoteIDInfo
	err  error
}

func (m Model) startDoctorRequery() (tea.Model, tea.Cmd) {
	if m.doctorOverlay == nil || m.doctorOverlay.requerying {
		return m, nil
	}
	if m.doctorOverlay.cursor < 0 || m.doctorOverlay.cursor >= len(m.doctorOverlay.findings) {
		return m, nil
	}
	item := m.doctorOverlay.findings[m.doctorOverlay.cursor]
	source := item.entry.Source
	if source == "" && len(item.entry.Links) > 0 {
		source = item.entry.Links[0]
	}
	if source == "" || !strings.Contains(source, "://") {
		m.statusMsg = "No URL to requery"
		return m, nil
	}
	m.doctorOverlay.requerying = true
	pp := m.pp
	return m, func() tea.Msg {
		ctx := context.Background()
		logger := log.New(io.Discard, "", 0)
		svc, err := cache.NewService(ctx, pp, logger, nil)
		if err != nil {
			return doctorRequeryDoneMsg{err: err}
		}
		info, err := svc.QueryRemoteID(ctx, source)
		return doctorRequeryDoneMsg{info: info, err: err}
	}
}

func (m Model) applyCurrentDoctorEntry() Model {
	if m.doctorOverlay == nil {
		return m
	}
	o := m.doctorOverlay
	if o.cursor < 0 || o.cursor >= len(o.findings) {
		return m
	}
	identifier := o.findings[o.cursor].finding.Identifier
	title := strings.TrimSpace(o.editTitle)
	artist := strings.TrimSpace(o.editArtist)

	idx, err := cache.Load(m.pp)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Cache load error: %v", err)
		return m
	}
	entry, ok := idx.GetByIdentifier(identifier)
	if !ok {
		m.statusMsg = "Entry not found in cache"
		return m
	}
	entry.Title = title
	entry.Artist = artist
	idx.SetEntry(entry)
	if err := cache.Save(m.pp, idx); err != nil {
		m.statusMsg = fmt.Sprintf("Save error: %v", err)
		return m
	}
	o.applied++
	m.statusMsg = fmt.Sprintf("Saved: %s – %s", title, artist)
	m = reloadState(m)

	// Advance to next entry.
	if o.cursor < len(o.findings)-1 {
		o.cursor++
		o.loadCurrentEntry()
	}
	return m
}

func (v *timelineView) autoScrollSeq() {
	visible := v.seqPanelHeight()
	if v.seqCursor < v.seqScrollTop {
		v.seqScrollTop = v.seqCursor
	} else if v.seqCursor >= v.seqScrollTop+visible {
		v.seqScrollTop = v.seqCursor - visible + 1
	}
}

func (v *collectionView) autoScroll() {
	visible := v.visibleRowCount()
	if v.cursor < v.scrollTop {
		v.scrollTop = v.cursor
	} else if v.cursor >= v.scrollTop+visible {
		v.scrollTop = v.cursor - visible + 1
	}
}

// View satisfies tea.Model.
func (m Model) View() string {
	if m.termHeight == 0 || m.termWidth == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Header.
	b.WriteString(renderHeader(m))
	b.WriteByte('\n')
	b.WriteByte('\n')

	// Content — doctor overlay replaces the content area when active.
	var content string
	if m.overlay == overlayDoctor && m.doctorOverlay != nil {
		content = m.doctorOverlay.view()
	} else {
		switch m.viewKind(m.activeView) {
		case "timeline":
			content = m.timelineView.view(m.cacheStatus)
		case "collection":
			content = m.collectionViews[m.collectionViewIndex()].view()
		case "cache":
			content = m.cacheView.view()
		case "tools":
			content = m.toolsView.view()
		}
	}

	// Pad content to fill the available space so the status/footer stay fixed
	// at the bottom regardless of which view is active.
	// Chrome: header(1) + blank(1) + [content] + blank(1) + status(1) + footer(1) = 5 lines.
	targetLines := m.termHeight - 5
	contentLines := strings.Count(content, "\n")
	b.WriteString(content)
	for contentLines < targetLines {
		b.WriteByte('\n')
		contentLines++
	}

	// Status line (always present — shows action feedback or stays empty).
	if m.statusMsg != "" {
		if m.job.active {
			b.WriteString(countYellow.Render(busySpinner(m.tick) + " " + m.statusMsg))
		} else {
			b.WriteString(countYellow.Render(m.statusMsg))
		}
	}
	b.WriteByte('\n')

	// Footer / input / confirm.
	if m.overlay == overlayDoctor && m.doctorOverlay != nil {
		b.WriteString(m.doctorOverlay.doctorFooter())
	} else {
		switch m.mode {
		case modeInput:
			b.WriteString(m.input.view())
		case modeConfirmDelete:
			b.WriteString(footerStyle.Render(fmt.Sprintf("Delete %s? [y/n]", m.deleteDesc)))
		case modeInlineEdit:
			b.WriteString(footerStyle.Render("←/→ cursor  ↑/↓ row  Tab/S-Tab field  Enter save  Esc cancel"))
		case modeAddClip:
			b.WriteString(footerStyle.Render("paste link/path or CSV (multi-line auto-imports) · Enter submit · Esc cancel"))
		default:
			b.WriteString(renderFooter(m))
		}
	}

	result := b.String()

	// Full-screen overlays render on top.
	if m.overlay == overlayHelp {
		return renderHelpOverlay(m.activeView, m.termWidth, m.termHeight)
	}

	return result
}

// --- Mutation processing ---

type editorDoneMsg struct {
	err           error
	collectionIdx int
}

type renderDoneMsg struct {
	err    error
	status string
}
type concatDoneMsg struct{ err error }
type fetchDoneMsg struct {
	err    error
	status string
}

// metadataProbeMsg carries yt-dlp metadata for a newly added row.
type metadataProbeMsg struct {
	collectionIdx int
	link          string // URL used to match the target row (stable across add/delete)
	title         string
	artist        string
	err           error
}

func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func revealCommand(path string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "explorer", []string{path}
	default:
		return "xdg-open", []string{path}
	}
}

func (m Model) drainJobEvents() Model {
	if !m.job.active || m.job.events == nil {
		return m
	}
	for {
		select {
		case raw, ok := <-m.job.events:
			if !ok {
				m.job = dashboardJobState{}
				return m
			}
			switch evt := raw.(type) {
			case jobRowStatusEvent:
				if evt.collectionIdx >= 0 && evt.collectionIdx < len(m.collectionViews) {
					if m.collectionViews[evt.collectionIdx].rowStatus == nil {
						m.collectionViews[evt.collectionIdx].rowStatus = make(map[int]string)
					}
					if strings.TrimSpace(evt.status) == "" {
						delete(m.collectionViews[evt.collectionIdx].rowStatus, evt.rowIndex)
					} else {
						m.collectionViews[evt.collectionIdx].rowStatus[evt.rowIndex] = evt.status
					}
				}
			case jobCollectionStatusEvent:
				if evt.collectionIdx >= 0 && evt.collectionIdx < len(m.collectionViews) {
					m.collectionViews[evt.collectionIdx].activity = evt.status
				}
			case jobCacheRowStatusEvent:
				if m.cacheView.rowStatus == nil {
					m.cacheView.rowStatus = make(map[string]string)
				}
				if strings.TrimSpace(evt.status) == "" {
					delete(m.cacheView.rowStatus, evt.identifier)
				} else {
					m.cacheView.rowStatus[evt.identifier] = evt.status
				}
			case jobCacheStatusEvent:
				m.cacheView.activity = evt.status
			case jobCompletedEvent:
				if evt.err != nil {
					m.statusMsg = fmt.Sprintf("%s failed: %v", evt.label, evt.err)
				} else {
					m.statusMsg = evt.label
				}
				for i := range m.collectionViews {
					m.collectionViews[i].activity = ""
					m.collectionViews[i].rowStatus = make(map[int]string)
				}
				m.cacheView.activity = ""
				m.cacheView.rowStatus = make(map[string]string)
				m.job = dashboardJobState{}
				m = reloadState(m)
				return m
			}
		default:
			return m
		}
	}
}

func (m Model) startCollectionFetchJob(cvIdx int, rows []csvplan.CollectionRow, all bool) Model {
	if m.job.active {
		return m
	}
	collName := m.collectionNames[cvIdx]
	label := "Fetch"
	if all {
		m.collectionViews[cvIdx].activity = "fetching all"
		label = fmt.Sprintf("Fetch %s", collName)
	} else if len(rows) > 0 {
		m.collectionViews[cvIdx].rowStatus[rows[0].Index] = "fetching"
		label = fmt.Sprintf("Fetch %s row %d", collName, rows[0].Index)
	}
	m.statusMsg = label + "..."
	events := make(chan dashboardJobEvent, max(16, len(rows)*4))
	m.job = dashboardJobState{active: true, label: label, events: events}
	go runDashboardFetchJob(m.pp, cvIdx, rows, all, events)
	return m
}

func runDashboardFetchJob(pp paths.ProjectPaths, cvIdx int, rows []csvplan.CollectionRow, all bool, events chan<- dashboardJobEvent) {
	defer close(events)
	ctx := context.Background()
	logger := log.New(io.Discard, "", 0)
	svc, err := cache.NewService(ctx, pp, logger, nil)
	if err != nil {
		events <- jobCompletedEvent{label: "Fetch", err: err}
		return
	}
	idx, err := cache.Load(pp)
	if err != nil {
		events <- jobCompletedEvent{label: "Fetch", err: err}
		return
	}
	dirty := false
	if all {
		events <- jobCollectionStatusEvent{collectionIdx: cvIdx, status: "fetching all"}
		for _, row := range rows {
			events <- jobRowStatusEvent{collectionIdx: cvIdx, rowIndex: row.Index, status: "queued"}
		}
	}
	for _, row := range rows {
		events <- jobRowStatusEvent{collectionIdx: cvIdx, rowIndex: row.Index, status: "fetching"}
		result, err := svc.Resolve(ctx, idx, row.ToRow(), cache.ResolveOptions{})
		if err != nil {
			events <- jobRowStatusEvent{collectionIdx: cvIdx, rowIndex: row.Index, status: "error"}
			events <- jobCompletedEvent{label: "Fetch", err: err}
			return
		}
		dirty = dirty || result.Updated
		finalStatus := "OK"
		switch result.Status {
		case cache.ResolveStatusCached, cache.ResolveStatusMatched:
			finalStatus = "cached"
		case cache.ResolveStatusMissing:
			finalStatus = "error"
		}
		events <- jobRowStatusEvent{collectionIdx: cvIdx, rowIndex: row.Index, status: finalStatus}
	}
	if dirty {
		if err := cache.Save(pp, idx); err != nil {
			events <- jobCompletedEvent{label: "Fetch", err: err}
			return
		}
	}
	events <- jobCompletedEvent{label: "Fetch", err: nil}
}

func (m Model) startCollectionRenderJob(cvIdx int, rows []csvplan.CollectionRow, all bool) Model {
	if m.job.active {
		return m
	}
	collName := m.collectionNames[cvIdx]
	label := "Render"
	if all {
		m.collectionViews[cvIdx].activity = "rendering all"
		label = fmt.Sprintf("Render %s", collName)
	} else if len(rows) > 0 {
		m.collectionViews[cvIdx].rowStatus[rows[0].Index] = "queued"
		label = fmt.Sprintf("Render %s row %d", collName, rows[0].Index)
	}
	m.statusMsg = label + "..."
	events := make(chan dashboardJobEvent, max(32, len(rows)*8))
	m.job = dashboardJobState{active: true, label: label, events: events}
	go runDashboardRenderJob(m.pp, m.cfg, m.collectionNames[cvIdx], m.collections[m.collectionNames[cvIdx]], cvIdx, rows, all, events)
	return m
}

func runDashboardRenderJob(pp paths.ProjectPaths, cfg config.Config, collName string, coll project.Collection, cvIdx int, rows []csvplan.CollectionRow, all bool, events chan<- dashboardJobEvent) {
	defer close(events)
	ctx := context.Background()
	if err := pp.EnsureCollectionDirs(cfg); err != nil {
		events <- jobCompletedEvent{label: "Render", err: err}
		return
	}
	idx, err := cache.Load(pp)
	if err != nil {
		events <- jobCompletedEvent{label: "Render", err: err}
		return
	}
	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		events <- jobCompletedEvent{label: "Render", err: err}
		return
	}
	targetColl := coll
	targetColl.Rows = append([]csvplan.CollectionRow(nil), rows...)
	collections := map[string]project.Collection{collName: targetColl}
	collectionClips, err := resolver.BuildCollectionClips(collections)
	if err != nil {
		events <- jobCompletedEvent{label: "Render", err: err}
		return
	}
	applySequenceEntryFadesLocal(cfg, collectionClips)
	svc, err := render.NewService(ctx, pp, cfg, nil)
	if err != nil {
		events <- jobCompletedEvent{label: "Render", err: err}
		return
	}
	rs, err := renderstate.Load(pp.RenderStateFile)
	if err != nil {
		events <- jobCompletedEvent{label: "Render", err: err}
		return
	}
	filenameTemplate := cfg.SegmentFilenameTemplate()
	segments := make([]render.Segment, 0, len(collectionClips))
	for _, cc := range collectionClips {
		seg, err := buildCollectionRenderSegmentLocal(pp, cfg, idx, cc)
		if err != nil {
			events <- jobCompletedEvent{label: "Render", err: err}
			return
		}
		if prior, ok := rs.Segments[seg.OutputPath]; ok {
			seg.StoredHash = prior.InputHash
		}
		segments = append(segments, seg)
	}
	actions := renderstate.DetectChanges(rs, segments, cfg, filenameTemplate, false)
	toRender := make([]render.Segment, 0, len(segments))
	for i, action := range actions {
		rowIndex := segments[i].Clip.Row.Index
		switch action.Action {
		case renderstate.ActionSkip:
			events <- jobRowStatusEvent{collectionIdx: cvIdx, rowIndex: rowIndex, status: "cached"}
		default:
			toRender = append(toRender, segments[i])
			events <- jobRowStatusEvent{collectionIdx: cvIdx, rowIndex: rowIndex, status: "queued"}
		}
	}
	if all {
		events <- jobCollectionStatusEvent{collectionIdx: cvIdx, status: "rendering all"}
	}
	reporter := &dashboardRenderReporter{collectionIdx: cvIdx, events: events}
	results := svc.Render(ctx, toRender, render.Options{
		Concurrency: max(1, min(runtime.NumCPU(), 2)),
		Force:       false,
		Reporter:    reporter,
	})
	segByPath := make(map[string]render.Segment, len(segments))
	for _, seg := range segments {
		segByPath[seg.OutputPath] = seg
	}
	for _, res := range results {
		if res.Err != nil {
			events <- jobCompletedEvent{label: "Render", err: res.Err}
			return
		}
		if !res.Skipped && res.OutputPath != "" {
			if seg, ok := segByPath[res.OutputPath]; ok {
				rs.Segments[res.OutputPath] = renderstate.SegmentState{
					InputHash:  renderstate.SegmentInputHash(seg, filenameTemplate),
					RenderedAt: time.Now(),
					SourcePath: seg.CachedPath,
					DurationS:  float64(seg.Clip.DurationSeconds),
				}
			}
		}
	}
	currentKeys := make(map[string]bool, len(segments))
	for _, seg := range segments {
		currentKeys[seg.OutputPath] = true
	}
	renderstate.Prune(rs, currentKeys)
	if err := rs.Save(pp.RenderStateFile); err != nil {
		events <- jobCompletedEvent{label: "Render", err: err}
		return
	}
	events <- jobCompletedEvent{label: "Render", err: nil}
}

type dashboardRenderReporter struct {
	collectionIdx int
	events        chan<- dashboardJobEvent
}

func (r *dashboardRenderReporter) Start(seg render.Segment) {
	r.events <- jobRowStatusEvent{collectionIdx: r.collectionIdx, rowIndex: seg.Clip.Row.Index, status: "rendering"}
}

func (r *dashboardRenderReporter) Progress(seg render.Segment, pct float64) {
	r.events <- jobRowStatusEvent{collectionIdx: r.collectionIdx, rowIndex: seg.Clip.Row.Index, status: fmt.Sprintf("rendering %d%%", int(pct*100))}
}

func (r *dashboardRenderReporter) Complete(res render.Result) {
	status := "rendered"
	if res.Skipped {
		status = "cached"
	}
	if res.Err != nil {
		status = "error"
	}
	r.events <- jobRowStatusEvent{collectionIdx: r.collectionIdx, rowIndex: res.TypeIndex, status: status}
}

func buildCollectionRenderSegmentLocal(pp paths.ProjectPaths, cfg config.Config, idx *cache.Index, collClip project.CollectionClip) (render.Segment, error) {
	clip := collClip.Clip
	clip.Row.DurationSeconds = clip.DurationSeconds
	if clip.Row.Index <= 0 {
		clip.Row.Index = clip.TypeIndex
		if clip.Row.Index <= 0 {
			clip.Row.Index = clip.Sequence
		}
	}

	segment := render.Segment{
		Clip:     clip,
		Overlays: collClip.Overlays,
	}

	outputDir := collClip.OutputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(pp.SegmentsDir, outputDir)
	}
	baseName := render.SegmentBaseName(cfg.SegmentFilenameTemplate(), segment)
	segment.OutputPath = filepath.Join(outputDir, baseName+".mp4")

	link := clip.Row.Link
	isURL := strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "youtu")
	if !isURL {
		link = strings.Trim(link, "'\"")
		sourcePath := link
		if !filepath.IsAbs(sourcePath) {
			sourcePath = filepath.Join(pp.Root, link)
		}
		if _, err := os.Stat(sourcePath); err != nil {
			return segment, err
		}
		segment.SourcePath = sourcePath
		segment.CachedPath = sourcePath
		return segment, nil
	}

	entry, ok, err := resolveDashboardEntryForRow(pp, idx, clip.Row)
	if err != nil {
		return segment, err
	}
	if !ok {
		return segment, fmt.Errorf("video not downloaded; may be unavailable or region-locked")
	}
	segment.Entry = entry
	segment.SourcePath = entry.CachedPath
	segment.CachedPath = entry.CachedPath
	return segment, nil
}

func applySequenceEntryFadesLocal(cfg config.Config, clips []project.CollectionClip) {
	byCollection := make(map[string][]int)
	for i, cc := range clips {
		byCollection[cc.CollectionName] = append(byCollection[cc.CollectionName], i)
	}
	for _, indices := range byCollection {
		sort.Slice(indices, func(a, b int) bool {
			return clips[indices[a]].Clip.Row.Index < clips[indices[b]].Clip.Row.Index
		})
	}

	consumed := make(map[string]int)
	for _, entry := range cfg.Timeline.Sequence {
		if entry.Collection == "" {
			continue
		}
		indices := byCollection[entry.Collection]
		if len(indices) == 0 {
			continue
		}
		start := consumed[entry.Collection]
		if start >= len(indices) {
			continue
		}
		count := len(indices) - start
		if entry.Count > 0 && entry.Count < count {
			count = entry.Count
		}
		if count < 0 {
			count = 0
		}
		consumed[entry.Collection] = start + count
		if entry.Fade == 0 && entry.FadeIn == 0 && entry.FadeOut == 0 {
			continue
		}
		fadeIn, fadeOut := config.ResolveFade(entry.Fade, entry.FadeIn, entry.FadeOut)
		for _, idx := range indices[start : start+count] {
			clips[idx].Clip.FadeInSeconds = fadeIn
			clips[idx].Clip.FadeOutSeconds = fadeOut
		}
		if entry.Interleave != nil {
			ilIndices := byCollection[entry.Interleave.Collection]
			ilStart := consumed[entry.Interleave.Collection]
			ilCount := len(ilIndices) - ilStart
			if ilCount > count {
				ilCount = count
			}
			if ilCount < 0 {
				ilCount = 0
			}
			consumed[entry.Interleave.Collection] = ilStart + ilCount
		}
	}
}

func resolveDashboardEntryForRow(pp paths.ProjectPaths, idx *cache.Index, row csvplan.Row) (cache.Entry, bool, error) {
	if idx == nil {
		return cache.Entry{}, false, fmt.Errorf("row %03d: cache index is nil", row.Index)
	}

	link := strings.TrimSpace(row.Link)
	if link == "" {
		return cache.Entry{}, false, fmt.Errorf("row %03d missing link", row.Index)
	}

	if isURL(link) {
		key, exists := idx.LookupLink(link)
		if !exists {
			return cache.Entry{}, false, nil
		}
		entry, ok := idx.GetByIdentifier(key)
		if !ok || strings.TrimSpace(entry.CachedPath) == "" {
			return cache.Entry{}, false, nil
		}
		return entry, true, nil
	}

	path := link
	if !filepath.IsAbs(path) {
		path = filepath.Join(pp.Root, link)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return cache.Entry{}, false, err
	}
	entry, ok := idx.GetByIdentifier(abs)
	if !ok || strings.TrimSpace(entry.CachedPath) == "" {
		return cache.Entry{}, false, nil
	}
	return entry, true, nil
}

// processAddTimelineEntry adds a new sequence entry to the timeline.
func (m Model) processAddTimelineEntry(value string) (tea.Model, tea.Cmd) {
	v := m.timelineView

	// If value starts with "c" or is a collection name, add a collection entry.
	// If it starts with "f" or looks like a file path, add a file entry.
	var entry config.SequenceEntry

	if value == "c" || value == "C" {
		// Need a second prompt for collection name — for now, add first collection with count=all.
		if len(m.collectionNames) > 0 {
			entry = config.SequenceEntry{Collection: m.collectionNames[0]}
		} else {
			m.statusMsg = "No collections available"
			return m, nil
		}
	} else {
		// Check if it's a known collection name.
		isCollection := false
		for _, name := range m.collectionNames {
			if strings.EqualFold(value, name) {
				entry = config.SequenceEntry{Collection: name}
				isCollection = true
				break
			}
		}
		if !isCollection {
			// Treat as file path.
			entry = config.SequenceEntry{File: value}
		}
	}

	v.sequence = append(v.sequence, entry)
	v.seqCursor = len(v.sequence) - 1
	v.autoScrollSeq()
	m.timelineView = v
	m.cfg.Timeline.Sequence = v.sequence
	m = saveConfigAndReResolve(m)
	m = m.setTimelineSequenceNote(v.seqCursor, "added")
	return m, nil
}

// processDeleteTimelineEntry deletes the sequence entry at the cursor.
func (m Model) processDeleteTimelineEntry() (tea.Model, tea.Cmd) {
	v := m.timelineView
	if len(v.sequence) == 0 {
		return m, nil
	}

	idx := v.seqCursor
	desc := "removed entry"
	if entry := v.sequence[idx]; entry.File != "" {
		desc = "removed file entry"
	} else if entry.Collection != "" {
		desc = "removed " + entry.Collection
	}
	v.sequence = append(v.sequence[:idx], v.sequence[idx+1:]...)

	if v.seqCursor >= len(v.sequence) && v.seqCursor > 0 {
		v.seqCursor = len(v.sequence) - 1
	}
	m.timelineView = v
	m.cfg.Timeline.Sequence = v.sequence
	m = saveConfigAndReResolve(m)
	if len(m.timelineView.sequence) > 0 {
		m = m.setTimelineSequenceNote(m.timelineView.seqCursor, desc)
	}
	return m, nil
}

// processAddRow adds a new row to the active collection.
// cleanYouTubeURL strips playlist, radio, and tracking parameters from YouTube URLs,
// keeping only the video ID.
func cleanYouTubeURL(raw string) string {
	raw = strings.TrimSpace(raw)

	// Handle youtu.be short links — strip query params entirely.
	if strings.HasPrefix(raw, "https://youtu.be/") || strings.HasPrefix(raw, "http://youtu.be/") {
		if idx := strings.Index(raw, "?"); idx >= 0 {
			return raw[:idx]
		}
		return raw
	}

	// Handle youtube.com/watch?v=... — keep only the v parameter.
	if !strings.Contains(raw, "youtube.com/watch") {
		return raw
	}
	qIdx := strings.Index(raw, "?")
	if qIdx < 0 {
		return raw
	}
	base := raw[:qIdx]
	query := raw[qIdx+1:]

	// Parse v= parameter manually (avoid net/url import for this).
	videoID := ""
	for _, param := range strings.Split(query, "&") {
		if strings.HasPrefix(param, "v=") {
			videoID = param[2:]
			break
		}
	}
	if videoID == "" {
		return raw
	}
	return base + "?v=" + videoID
}

func (m Model) processAddRow(value string) (tea.Model, tea.Cmd) {
	cvIdx := m.activeView - 1
	if cvIdx < 0 || cvIdx >= len(m.collectionViews) {
		return m, nil
	}

	v := m.collectionViews[cvIdx]
	collName := m.collectionNames[cvIdx]
	coll := m.collections[collName]

	if looksLikeBatchImport(value) {
		rows, format, err := csvplan.ImportCollectionText(value, project.CollectionOptionsForConfig(coll))
		if err != nil {
			m = m.setCollectionCursorNote(cvIdx, fmt.Sprintf("import failed: %v", err))
			return m, nil
		}

		coll = project.AppendCollectionRows(coll, rows)
		if err := project.WriteCollectionPlan(coll); err != nil {
			m = m.setCollectionCursorNote(cvIdx, fmt.Sprintf("write error: %v", err))
			return m, nil
		}

		m.collections[collName] = coll
		m = reloadCollection(m, cvIdx)
		if len(rows) > 0 {
			m.collectionViews[cvIdx].cursor = len(m.collectionViews[cvIdx].rows) - 1
			m.collectionViews[cvIdx].autoScroll()
			m = m.setCollectionCursorNote(cvIdx, fmt.Sprintf("imported %d rows from %s", len(rows), format))
		}
		return m, nil
	}

	// Clean YouTube URLs.
	value = cleanYouTubeURL(value)

	newRow := project.BuildCollectionRow(coll, value)
	newRow.Index = len(v.rows) + 1

	v.rows = append(v.rows, newRow)
	v.cursor = len(v.rows) - 1
	v.autoScroll()
	m.collectionViews[cvIdx] = v

	m = writeCollection(m, cvIdx)
	m = reResolve(m)

	// Probe metadata for URLs.
	if isURL(value) {
		m = m.setCollectionCursorNote(cvIdx, "probing metadata...")
		return m, probeMetadata(value, cvIdx)
	}
	m = m.setCollectionCursorNote(cvIdx, "added")

	return m, nil
}

func looksLikeBatchImport(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}

	lines := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	if len(lines) > 1 {
		return true
	}
	if strings.HasPrefix(trimmed, "- ") {
		return true
	}
	firstLine := trimmed
	if len(lines) == 1 {
		firstLine = strings.TrimSpace(lines[0])
	}
	return strings.Contains(firstLine, ",") || strings.Contains(firstLine, "\t")
}

// processDeleteRow deletes the row at the cursor in the active collection.
func (m Model) processDeleteRow() (tea.Model, tea.Cmd) {
	cvIdx := m.activeView - 1
	if cvIdx < 0 || cvIdx >= len(m.collectionViews) {
		return m, nil
	}

	v := m.collectionViews[cvIdx]
	if len(v.rows) == 0 {
		return m, nil
	}

	idx := v.cursor
	v.rows = append(v.rows[:idx], v.rows[idx+1:]...)

	for i := range v.rows {
		v.rows[i].Index = i + 1
	}

	if v.cursor >= len(v.rows) && v.cursor > 0 {
		v.cursor = len(v.rows) - 1
	}
	m.collectionViews[cvIdx] = v

	m = writeCollection(m, cvIdx)
	m = reResolve(m)
	if len(m.collectionViews[cvIdx].rows) > 0 {
		m = m.setCollectionCursorNote(cvIdx, "removed row")
	}
	return m, nil
}

// processDuplicateRow duplicates the selected row and appends the copy to the
// end of the active collection.
func (m Model) processDuplicateRow(cvIdx int) Model {
	if cvIdx < 0 || cvIdx >= len(m.collectionViews) {
		return m
	}

	collName := m.collectionNames[cvIdx]
	coll := m.collections[collName]
	if len(coll.Rows) == 0 {
		return m
	}

	sourceIdx := m.collectionViews[cvIdx].cursor
	if sourceIdx < 0 || sourceIdx >= len(coll.Rows) {
		return m
	}

	coll = project.DuplicateCollectionRow(coll, sourceIdx)
	m.collections[collName] = coll

	v := m.collectionViews[cvIdx]
	v.rows = coll.Rows
	v.cursor = len(v.rows) - 1
	v.autoScroll()
	m.collectionViews[cvIdx] = v

	m = writeCollection(m, cvIdx)
	m = reResolve(m)
	m = m.setCollectionCursorNote(cvIdx, fmt.Sprintf("duplicated row %d", sourceIdx+1))
	return m
}

// processDeleteCacheEntry removes the cache entry at the cursor.
func (m Model) processDeleteCacheEntry() (tea.Model, tea.Cmd) {
	if m.cacheIdx == nil {
		return m, nil
	}

	entries := m.cacheView.entries()
	if m.cacheView.cursor >= len(entries) {
		return m, nil
	}

	entry := entries[m.cacheView.cursor]
	idxEntry, ok := m.cacheIdx.GetByIdentifier(entry.Identifier)
	if !ok {
		m = m.setCacheCursorNote("entry not found in index")
		return m, nil
	}

	// Delete the cached file for URL-sourced entries only.
	if idxEntry.SourceType == cache.SourceTypeURL && idxEntry.CachedPath != "" {
		if err := os.Remove(idxEntry.CachedPath); err != nil && !os.IsNotExist(err) {
			m = m.setCacheCursorNote(fmt.Sprintf("remove file: %v", err))
			return m, nil
		}
	}

	// Remove from index and all referencing links.
	m.cacheIdx.DeleteEntry(entry.Identifier)
	for link, target := range m.cacheIdx.Links {
		if target == entry.Identifier {
			m.cacheIdx.DeleteLink(link)
		}
	}

	if err := cache.Save(m.pp, m.cacheIdx); err != nil {
		m = m.setCacheCursorNote(fmt.Sprintf("save index: %v", err))
		return m, nil
	}

	title := entry.Title
	if title == "" {
		title = filepath.Base(entry.CachedPath)
	}

	m = reloadState(m)
	if m.cacheView.cursor >= len(m.cacheView.entries()) && m.cacheView.cursor > 0 {
		m.cacheView.cursor--
	}
	if len(m.cacheView.entries()) > 0 {
		m = m.setCacheCursorNote(fmt.Sprintf("removed %s", title))
	}

	return m, nil
}

// reindexAndWriteCollection re-indexes rows and writes the plan file.
func reindexAndWriteCollection(m Model, cvIdx int) Model {
	v := m.collectionViews[cvIdx]
	for i := range v.rows {
		v.rows[i].Index = i + 1
	}
	m.collectionViews[cvIdx] = v
	m = writeCollection(m, cvIdx)
	m = reResolve(m)
	return m
}

// writeCollection writes the collection's plan file back to disk.
func writeCollection(m Model, cvIdx int) Model {
	collName := m.collectionNames[cvIdx]
	coll := m.collections[collName]
	v := m.collectionViews[cvIdx]
	oldCursor := v.cursor
	oldScrollTop := v.scrollTop
	oldEditing := v.editing
	oldEditFieldIdx := v.editFieldIdx
	oldEditValue := v.editValue
	oldEditCursor := v.editCursor
	oldAddFocus := v.addFocus
	oldAddBuffer := v.addBuffer

	coll.Rows = v.rows
	if coll.PlanFormat != "yaml" {
		coll.Headers = csvplan.MergeHeaders(coll.Headers, v.rows)
	}
	err := project.WriteCollectionPlan(coll)

	if err != nil {
		m.statusMsg = fmt.Sprintf("Write error: %v", err)
		return m
	}

	m.collections[collName] = coll
	m = reloadCollection(m, cvIdx)
	v = m.collectionViews[cvIdx]
	v.cursor = min(oldCursor, max(0, len(v.rows)-1))
	v.scrollTop = oldScrollTop
	v.editing = oldEditing
	v.editFieldIdx = oldEditFieldIdx
	v.editValue = oldEditValue
	v.editCursor = oldEditCursor
	v.addFocus = oldAddFocus
	v.addBuffer = oldAddBuffer
	v.autoScroll()
	m.collectionViews[cvIdx] = v
	return m
}

// reResolve re-resolves the timeline after mutations.
func reResolve(m Model) Model {
	if len(m.cfg.Timeline.Sequence) > 0 {
		timeline, err := project.ResolveTimeline(m.cfg.Timeline, m.collections)
		if err != nil {
			m.statusMsg = fmt.Sprintf("Timeline error: %v", err)
			return m
		}
		m.timeline = timeline
		m.timelineView.resolved = timeline
	}

	m.summaries = buildSummaries(m.collections, m.collectionNames, m.cacheIdx, m.pp)
	m.cacheStatus = buildCacheStatus(m.collections, m.cacheIdx, m.pp)
	oldW, oldH := m.cacheView.termWidth, m.cacheView.termHeight
	oldShowAll := m.cacheView.showAll
	m.cacheView = newCacheView(m.cacheIdx, buildCollectionLinks(m.collections))
	m.cacheView.termWidth = oldW
	m.cacheView.termHeight = oldH
	m.cacheView.showAll = oldShowAll
	return m
}

// resolveVideoPath finds the cached or local file path for a collection row.
func (m Model) resolveVideoPath(row csvplan.CollectionRow) string {
	link := strings.TrimSpace(row.Link)
	if link == "" {
		return ""
	}

	isURL := isURL(link)
	if isURL {
		if m.cacheIdx == nil {
			return ""
		}
		key, ok := m.cacheIdx.LookupLink(link)
		if !ok {
			return ""
		}
		entry, ok := m.cacheIdx.GetByIdentifier(key)
		if !ok || entry.CachedPath == "" {
			return ""
		}
		return entry.CachedPath
	}

	// Local file.
	path := strings.Trim(link, "'\"")
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.pp.Root, path)
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// reloadState reloads cache index and render state from disk (after render/concat).
func reloadState(m Model) Model {
	idx, _ := cache.Load(m.pp)
	rs, _ := state.Load(m.pp.RenderStateFile)
	m.cacheIdx = idx
	m.renderState = rs
	oldW, oldH := m.cacheView.termWidth, m.cacheView.termHeight
	oldShowAll := m.cacheView.showAll
	m.cacheView = newCacheView(idx, buildCollectionLinks(m.collections))
	m.cacheView.termWidth = oldW
	m.cacheView.termHeight = oldH
	m.cacheView.showAll = oldShowAll
	m.summaries = buildSummaries(m.collections, m.collectionNames, idx, m.pp)
	m.cacheStatus = buildCacheStatus(m.collections, idx, m.pp)
	for i := range m.collectionNames {
		collName := m.collectionNames[i]
		coll := m.collections[collName]
		m.collectionViews[i].states = computeRowStates(coll, m.pp, m.cfg, idx)
	}
	return m
}

func (m Model) refreshFromDisk() Model {
	activeName := ""
	if m.activeView >= 0 && m.activeView < len(m.viewNames) {
		activeName = m.viewNames[m.activeView]
	}

	cfg, err := config.Load(m.pp.ConfigFile)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Refresh error: %v", err)
		return m
	}

	pp := paths.ApplyConfig(m.pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Refresh error: %v", err)
		return m
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		m.statusMsg = fmt.Sprintf("Refresh error: %v", err)
		return m
	}

	idx, _ := cache.Load(pp)
	rs, _ := state.Load(pp.RenderStateFile)

	var timeline []project.TimelineEntry
	if len(cfg.Timeline.Sequence) > 0 {
		timeline, err = project.ResolveTimeline(cfg.Timeline, collections)
		if err != nil {
			m.statusMsg = fmt.Sprintf("Refresh error: %v", err)
			return m
		}
	}

	refreshed := NewModel(cfg, pp, collections, timeline, idx, rs, m.toolWarning, m.toolStatuses)
	refreshed.termWidth = m.termWidth
	refreshed.termHeight = m.termHeight
	refreshed.tick = m.tick
	refreshed.mode = modeNormal
	refreshed.statusMsg = "Refreshed from disk"
	if refreshed.termWidth > 0 || refreshed.termHeight > 0 {
		refreshed.timelineView.termWidth = refreshed.termWidth
		refreshed.timelineView.termHeight = refreshed.termHeight
		refreshed.cacheView.termWidth = refreshed.termWidth
		refreshed.cacheView.termHeight = refreshed.termHeight
		refreshed.toolsView.termWidth = refreshed.termWidth
		for i := range refreshed.collectionViews {
			refreshed.collectionViews[i].termWidth = refreshed.termWidth
			refreshed.collectionViews[i].termHeight = refreshed.termHeight
			refreshed.collectionViews[i].tick = refreshed.tick
		}
	}

	refreshed.activeView = 0
	for i, name := range refreshed.viewNames {
		if name == activeName {
			refreshed.activeView = i
			break
		}
	}

	return refreshed
}

// saveConfigAndReResolve writes the config and re-resolves the timeline.
func saveConfigAndReResolve(m Model) Model {
	if err := config.Save(m.pp.ConfigFile, m.cfg); err != nil {
		m.statusMsg = fmt.Sprintf("Config write error: %v", err)
		return m
	}
	m.timelineView.sequence = m.cfg.Timeline.Sequence
	return reResolve(m)
}

// reloadCollection reloads a collection from disk (after editor exit).
func reloadCollection(m Model, cvIdx int) Model {
	collName := m.collectionNames[cvIdx]
	coll := m.collections[collName]

	opts := project.CollectionOptionsForConfig(coll)

	var rows []csvplan.CollectionRow
	var err error
	if coll.PlanFormat == "yaml" {
		result, yamlErr := csvplan.LoadCollectionYAML(coll.Plan, opts)
		rows = result.Rows
		coll.Headers = result.Columns
		coll.Defaults = result.Defaults
		err = yamlErr
	} else {
		rows, err = csvplan.LoadCollection(coll.Plan, opts)
	}

	if err != nil {
		m.statusMsg = fmt.Sprintf("Reload error: %v", err)
		return m
	}

	coll.Rows = rows
	m.collections[collName] = coll
	m.collectionViews[cvIdx].rows = rows
	m.collectionViews[cvIdx].columns = discoverColumns(rows, coll.Headers)
	m.collectionViews[cvIdx].states = computeRowStates(coll, m.pp, m.cfg, m.cacheIdx)
	if m.collectionViews[cvIdx].cursor >= len(rows) {
		m.collectionViews[cvIdx].cursor = max(0, len(rows)-1)
	}

	return reResolve(m)
}

// buildSummaries computes per-collection cache/render counts.
func buildSummaries(collections map[string]project.Collection, names []string, idx *cache.Index, pp paths.ProjectPaths) map[string]collectionSummary {
	summaries := make(map[string]collectionSummary, len(names))
	for _, name := range names {
		coll := collections[name]
		s := collectionSummary{Total: len(coll.Rows)}

		for _, row := range coll.Rows {
			link := strings.TrimSpace(row.Link)
			cached := false

			if isURL(link) {
				if idx != nil {
					_, cached = idx.LookupLink(link)
				}
			} else {
				cached = checkFileExists(link, pp.Root)
			}

			if cached {
				s.Cached++
			} else {
				s.CacheMissing++
			}
		}

		s.Rendered = 0 // Simplified for Phase 1; full render state analysis comes later.
		s.Missing = s.Total - s.Rendered

		summaries[name] = s
	}
	return summaries
}

// buildCacheStatus builds a cache status map keyed by "collection:index" or "file:path".
func buildCacheStatus(collections map[string]project.Collection, idx *cache.Index, pp paths.ProjectPaths) map[string]string {
	status := make(map[string]string)
	for name, coll := range collections {
		for _, row := range coll.Rows {
			key := fmt.Sprintf("%s:%d", name, row.Index)
			link := strings.TrimSpace(row.Link)
			cached := false

			if isURL(link) {
				if idx != nil {
					_, cached = idx.LookupLink(link)
				}
			} else {
				cached = checkFileExists(link, pp.Root)
			}

			if cached {
				status[key] = "cached"
			} else {
				status[key] = "missing"
			}
		}
	}
	return status
}

func buildCollectionLinks(collections map[string]project.Collection) map[string]string {
	collLinks := make(map[string]string)
	for name, coll := range collections {
		for _, row := range coll.Rows {
			link := strings.TrimSpace(row.Link)
			if link != "" {
				collLinks[link] = name
			}
		}
	}
	return collLinks
}

func checkFileExists(path, root string) bool {
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	_, err := os.Stat(path)
	return err == nil
}
