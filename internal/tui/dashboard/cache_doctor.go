package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"powerhour/internal/cache"
	"powerhour/internal/cachedoctor"
)

type doctorItem struct {
	entry   cache.Entry
	finding cachedoctor.Finding
}

type cacheDoctorOverlay struct {
	findings []doctorItem
	cursor   int

	// 0 = title, 1 = artist
	activeField int

	editTitle     string
	editArtist    string
	titleCursor   int
	artistCursor  int
	titleTouched  bool
	artistTouched bool

	applied int

	// Known artists for fuzzy matching.
	knownArtists []string

	// Requery state.
	requerying    bool
	requeryStatus string // transient message after requery completes
	tick          int

	termWidth  int
	termHeight int
}

func newCacheDoctorOverlay(items []doctorItem, knownArtists []string, w, h int) cacheDoctorOverlay {
	o := cacheDoctorOverlay{
		findings:     items,
		knownArtists: knownArtists,
		termWidth:    w,
		termHeight:   h,
	}
	o.loadCurrentEntry()
	return o
}

func (o *cacheDoctorOverlay) loadCurrentEntry() {
	if o.cursor < 0 || o.cursor >= len(o.findings) {
		return
	}
	f := o.findings[o.cursor].finding
	o.editTitle = f.ProposedTitle
	o.editArtist = f.ProposedArtist
	o.titleCursor = len(o.editTitle)
	o.artistCursor = len(o.editArtist)
	o.titleTouched = false
	o.artistTouched = false
	o.activeField = 0
	o.requeryStatus = ""
}

func (o *cacheDoctorOverlay) activeText() string {
	if o.activeField == 0 {
		return o.editTitle
	}
	return o.editArtist
}

func (o *cacheDoctorOverlay) activeCursor() int {
	if o.activeField == 0 {
		return o.titleCursor
	}
	return o.artistCursor
}

func (o *cacheDoctorOverlay) setActiveText(s string, cur int) {
	if o.activeField == 0 {
		o.editTitle = s
		o.titleCursor = cur
		o.titleTouched = true
	} else {
		o.editArtist = s
		o.artistCursor = cur
		o.artistTouched = true
	}
}

func (o *cacheDoctorOverlay) setActiveCursor(cur int) {
	if o.activeField == 0 {
		o.titleCursor = cur
	} else {
		o.artistCursor = cur
	}
}

func (o *cacheDoctorOverlay) applyRequery(info cache.RemoteIDInfo, normCfg cache.NormalizationConfig) {
	if o.cursor < 0 || o.cursor >= len(o.findings) {
		o.requerying = false
		return
	}
	item := &o.findings[o.cursor]

	item.entry.Uploader = firstNonEmpty(info.Uploader, item.entry.Uploader)
	item.entry.Channel = firstNonEmpty(info.Channel, item.entry.Channel)
	item.entry.Track = firstNonEmpty(info.Track, item.entry.Track)
	item.entry.Album = firstNonEmpty(info.Album, item.entry.Album)

	input := cache.NormalizationInput{
		Title:    firstNonEmpty(info.Title, item.entry.Title),
		Artist:   firstNonEmpty(info.Artist, item.entry.Artist),
		Track:    firstNonEmpty(info.Track, item.entry.Track),
		Album:    firstNonEmpty(info.Album, item.entry.Album),
		Uploader: firstNonEmpty(info.Uploader, item.entry.Uploader),
		Channel:  firstNonEmpty(info.Channel, item.entry.Channel),
	}
	result := cache.NormalizeMetadata(normCfg, input)

	oldTitle := item.finding.ProposedTitle
	oldArtist := item.finding.ProposedArtist

	item.finding.ProposedTitle = result.Title
	item.finding.ProposedArtist = result.Artist
	item.finding.Confidence = result.Confidence
	item.finding.Reasons = result.Reasons

	changed := result.Title != oldTitle || result.Artist != oldArtist
	if !o.titleTouched {
		o.editTitle = result.Title
		o.titleCursor = len(o.editTitle)
	}
	if !o.artistTouched {
		o.editArtist = result.Artist
		o.artistCursor = len(o.editArtist)
	}

	if changed {
		o.requeryStatus = "updated from yt-dlp"
	} else {
		o.requeryStatus = "yt-dlp returned same metadata"
	}
	o.requerying = false
}

// handleKey processes input for the doctor overlay.
// Returns done=true when the overlay should close, applyNow=true when the current entry should be saved immediately.
func (o *cacheDoctorOverlay) handleKey(msg tea.KeyMsg) (done bool, applyNow bool) {
	if o.requerying {
		return false, false
	}

	o.requeryStatus = ""

	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEscape:
		return true, false
	case tea.KeyCtrlR:
		// Requery — handled by model to start async job.
		return false, false
	case tea.KeyEnter:
		if o.activeField == 1 {
			if suggestions := o.artistSuggestions(); len(suggestions) > 0 {
				o.editArtist = suggestions[0]
				o.artistCursor = len(o.editArtist)
				o.artistTouched = true
				return false, false
			}
		}
		return o.cursor >= len(o.findings)-1, true
	case tea.KeyTab:
		if o.activeField == 1 {
			if suggestions := o.artistSuggestions(); len(suggestions) > 0 {
				o.editArtist = suggestions[0]
				o.artistCursor = len(o.editArtist)
				o.artistTouched = true
				return false, false
			}
		}
		if o.cursor < len(o.findings)-1 {
			o.cursor++
			o.loadCurrentEntry()
		}
		return false, false
	case tea.KeyShiftTab:
		if o.cursor > 0 {
			o.cursor--
			o.loadCurrentEntry()
		}
		return false, false
	case tea.KeyUp:
		o.activeField = 0
		return false, false
	case tea.KeyDown:
		o.activeField = 1
		return false, false
	case tea.KeyLeft:
		text := o.activeText()
		cur := o.activeCursor()
		if cur > 0 {
			_, size := utf8.DecodeLastRuneInString(text[:cur])
			o.setActiveCursor(cur - size)
		}
		return false, false
	case tea.KeyRight:
		text := o.activeText()
		cur := o.activeCursor()
		if cur < len(text) {
			_, size := utf8.DecodeRuneInString(text[cur:])
			o.setActiveCursor(cur + size)
		}
		return false, false
	case tea.KeySpace:
		text := o.activeText()
		cur := o.activeCursor()
		o.setActiveText(text[:cur]+" "+text[cur:], cur+1)
		return false, false
	case tea.KeyBackspace:
		text := o.activeText()
		cur := o.activeCursor()
		if cur > 0 {
			_, size := utf8.DecodeLastRuneInString(text[:cur])
			o.setActiveText(text[:cur-size]+text[cur:], cur-size)
		}
		return false, false
	case tea.KeyDelete:
		text := o.activeText()
		cur := o.activeCursor()
		if cur < len(text) {
			_, size := utf8.DecodeRuneInString(text[cur:])
			o.setActiveText(text[:cur]+text[cur+size:], cur)
		}
		return false, false
	case tea.KeyRunes:
		text := o.activeText()
		cur := o.activeCursor()
		ch := string(msg.Runes)
		o.setActiveText(text[:cur]+ch+text[cur:], cur+len(ch))
		return false, false
	}

	return false, false
}

// isRequeryKey returns true if Ctrl+R was pressed.
func isRequeryKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyCtrlR
}

// artistSuggestions returns fuzzy-matched known artists based on current edit text.
func (o *cacheDoctorOverlay) artistSuggestions() []string {
	if o.activeField != 1 || !o.artistTouched {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(o.editArtist))
	if query == "" {
		return nil
	}

	type scored struct {
		name  string
		score int
	}

	var matches []scored
	for _, artist := range o.knownArtists {
		lower := strings.ToLower(artist)
		if lower == query {
			continue
		}
		if strings.HasPrefix(lower, query) {
			matches = append(matches, scored{artist, 0})
		} else if strings.Contains(lower, query) {
			matches = append(matches, scored{artist, 1})
		} else if fuzzyMatch(query, lower) {
			matches = append(matches, scored{artist, 2})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return matches[i].name < matches[j].name
	})

	var result []string
	for i, m := range matches {
		if i >= 5 {
			break
		}
		result = append(result, m.name)
	}
	return result
}

func fuzzyMatch(query, target string) bool {
	qi := 0
	for ti := 0; ti < len(target) && qi < len(query); ti++ {
		if target[ti] == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}

// view renders the doctor content for the content area (not full-screen).
//
// Content is built as prioritised sections rather than one flat string cut
// at the end. The never-droppable sections (edit fields, confidence
// reasons, requery status, and — via the shared termHeight-5 budget — the
// footer rendered separately by Model.View()) are measured first; whatever
// budget remains is spent on the optional context fields (dropped first)
// and the artist suggestion list (capped second). Any reduction is marked
// inline at the point it occurred so nothing is silently dropped.
func (o *cacheDoctorOverlay) view() string {
	if len(o.findings) == 0 {
		return faint.Render("No entries need attention.")
	}

	item := o.findings[o.cursor]
	entry := item.entry
	finding := item.finding

	// pre: header through the ARTIST "New:" line — never droppable.
	var pre []string

	confStyle := confidenceStyle(finding.Confidence)
	header := fmt.Sprintf("CACHE DOCTOR (%d of %d)", o.cursor+1, len(o.findings))
	confBadge := confStyle.Render(confidenceLabel(finding.Confidence))
	appliedStr := ""
	if o.applied > 0 {
		appliedStr = "  " + countGreen.Render(fmt.Sprintf("%d saved", o.applied))
	}
	pre = append(pre, sectionLabel.Render(header)+"  "+confBadge+appliedStr)

	source := entry.Source
	if source == "" && len(entry.Links) > 0 {
		source = entry.Links[0]
	}
	if source != "" {
		pre = append(pre, faint.Render("SOURCE  ")+truncate(source, o.termWidth-12))
	}
	pre = append(pre, faint.Render("FILE    ")+truncate(entry.CachedPath, o.termWidth-12))
	pre = append(pre, "")

	titleLabel := "TITLE"
	if o.activeField == 0 {
		titleLabel = editStyle.Render(titleLabel)
	} else {
		titleLabel = bold.Render(titleLabel)
	}
	currentTitle := displayBlank(finding.CurrentTitle)
	pre = append(pre, " "+titleLabel)
	pre = append(pre, "   Current:  "+faint.Render(currentTitle))
	if o.activeField == 0 {
		pre = append(pre, "   New:      "+renderEditField(o.editTitle, o.titleCursor))
	} else {
		pre = append(pre, "   New:      "+o.editTitle)
	}
	pre = append(pre, "")

	artistLabel := "ARTIST"
	if o.activeField == 1 {
		artistLabel = editStyle.Render(artistLabel)
	} else {
		artistLabel = bold.Render(artistLabel)
	}
	currentArtist := displayBlank(finding.CurrentArtist)
	pre = append(pre, " "+artistLabel)
	pre = append(pre, "   Current:  "+faint.Render(currentArtist))
	if o.activeField == 1 {
		pre = append(pre, "   New:      "+renderEditField(o.editArtist, o.artistCursor))
	} else {
		pre = append(pre, "   New:      "+o.editArtist)
	}
	artistNewIdx := len(pre) - 1

	// Required tail: reasons + requery status. Always present when non-empty.
	var reasonsRequery []string
	if humanReasons := humanizeReasons(finding.Reasons); humanReasons != "" {
		reasonsRequery = append(reasonsRequery, faint.Render(" "+humanReasons))
	}
	if o.requerying {
		reasonsRequery = append(reasonsRequery, countYellow.Render(busySpinner(o.tick)+" Fetching metadata from yt-dlp..."))
	} else if o.requeryStatus != "" {
		reasonsRequery = append(reasonsRequery, faint.Render(" "+o.requeryStatus))
	}

	// Optional groups, in drop order: context first, suggestions second.
	contextFull := o.contextSectionLines(entry)
	suggestionFull := o.suggestionDisplayLines()

	capped := o.termHeight > 0
	maxLines := 0
	if capped {
		maxLines = o.termHeight - 5
		if maxLines < 0 {
			maxLines = 0
		}
	}

	// requiredCount = pre + the always-present blank separator + reasons/requery tail.
	requiredCount := len(pre) + 1 + len(reasonsRequery)
	budget := 0
	if capped {
		budget = maxLines - requiredCount
	}

	// Allocate to context (dropped first).
	var contextOut []string
	if len(contextFull) > 0 {
		switch {
		case !capped || budget >= len(contextFull):
			contextOut = contextFull
			if capped {
				budget -= len(contextFull)
			}
		case budget >= 1:
			n := countContextFields(entry)
			contextOut = []string{faint.Render(fmt.Sprintf(" CONTEXT hidden (%d field%s) — resize to view", n, pluralSuffix(n)))}
			budget--
		default:
			n := countContextFields(entry)
			pre[artistNewIdx] += faint.Render(fmt.Sprintf("  CONTEXT hidden (%d field%s)", n, pluralSuffix(n)))
		}
	}

	// Allocate to suggestions (capped second, only after context is spent).
	var suggestOut []string
	total := len(suggestionFull)
	if total > 0 {
		switch {
		case !capped || budget >= total:
			suggestOut = suggestionFull
			if capped {
				budget -= total
			}
		case budget > 0:
			shown := budget
			suggestOut = append([]string{}, suggestionFull[:shown]...)
			suggestOut[len(suggestOut)-1] += faint.Render(fmt.Sprintf("  … %d more", total-shown))
			budget = 0
		default:
			pre[artistNewIdx] += faint.Render(fmt.Sprintf("  … %d more", total))
		}
	}

	all := make([]string, 0, len(pre)+len(suggestOut)+1+len(contextOut)+len(reasonsRequery))
	all = append(all, pre...)
	all = append(all, suggestOut...)
	all = append(all, "")
	all = append(all, contextOut...)
	all = append(all, reasonsRequery...)

	if capped {
		return clampToLines(all, maxLines)
	}
	return clampToLines(all, -1)
}

// contextSectionLines returns the CONTEXT block (header, populated fields,
// trailing blank separator) or nil when the entry has no context fields.
func (o *cacheDoctorOverlay) contextSectionLines(entry cache.Entry) []string {
	if entry.Uploader == "" && entry.Channel == "" && entry.Track == "" && entry.Album == "" {
		return nil
	}
	var lines []string
	lines = append(lines, bold.Render(" CONTEXT"))
	if entry.Uploader != "" {
		lines = append(lines, fmt.Sprintf("   Uploader:  %s", entry.Uploader))
	}
	if entry.Channel != "" {
		lines = append(lines, fmt.Sprintf("   Channel:   %s", entry.Channel))
	}
	if entry.Track != "" {
		lines = append(lines, fmt.Sprintf("   Track:     %s", entry.Track))
	}
	if entry.Album != "" {
		lines = append(lines, fmt.Sprintf("   Album:     %s", entry.Album))
	}
	lines = append(lines, "")
	return lines
}

func countContextFields(entry cache.Entry) int {
	n := 0
	if entry.Uploader != "" {
		n++
	}
	if entry.Channel != "" {
		n++
	}
	if entry.Track != "" {
		n++
	}
	if entry.Album != "" {
		n++
	}
	return n
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// suggestionDisplayLines renders the artist autocomplete suggestion list
// as pre-styled lines, matching the layout previously inlined in view().
func (o *cacheDoctorOverlay) suggestionDisplayLines() []string {
	if o.activeField != 1 {
		return nil
	}
	suggestions := o.artistSuggestions()
	if len(suggestions) == 0 {
		return nil
	}
	lines := make([]string, len(suggestions))
	for i, s := range suggestions {
		prefix := "   "
		text := s
		if i == 0 {
			prefix = " → "
			text += faint.Render("  (Enter to accept)")
		}
		lines[i] = faint.Render(prefix) + faint.Render(text)
	}
	return lines
}

// clampToLines joins lines with newlines, hard-clamping to max lines when
// max >= 0. max < 0 means no cap. Guarantees the returned string's newline
// count never exceeds max, regardless of how the caller allocated budget.
func clampToLines(lines []string, max int) string {
	if max >= 0 && len(lines) > max {
		lines = lines[:max]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// doctorFooter returns the footer text for the doctor overlay.
func (o *cacheDoctorOverlay) doctorFooter() string {
	if o.requerying {
		return footerStyle.Render("Waiting for yt-dlp...")
	}
	if len(o.findings) == 0 {
		return footerStyle.Render("Esc close")
	}
	item := o.findings[o.cursor]
	source := item.entry.Source
	if source == "" && len(item.entry.Links) > 0 {
		source = item.entry.Links[0]
	}
	hasURL := item.entry.SourceType == cache.SourceTypeURL || (source != "" && strings.Contains(source, "://"))
	footer := "↑/↓ field  Tab/S-Tab next/prev  Enter save  Esc close"
	if hasURL {
		footer = "↑/↓ field  Tab/S-Tab next/prev  Enter save  Ctrl+R requery  Esc close"
	}
	return footerStyle.Render(footer)
}

func confidenceLabel(conf string) string {
	switch conf {
	case "high":
		return "auto-fix"
	case "medium":
		return "best guess"
	default:
		return "needs review"
	}
}

func confidenceStyle(conf string) lipgloss.Style {
	switch conf {
	case "high":
		return countGreen
	case "medium":
		return countYellow
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	}
}

var reasonMap = map[string]string{
	"used track as title":                     "used track metadata as title",
	"applied artist alias":                    "matched a known artist alias",
	"split artist/title from title field":     "split \"Artist - Title\" format",
	"mapped uploader/channel to artist alias": "matched uploader/channel to known artist",
	"removed video suffix noise":              "cleaned title (removed Official Video, HD, etc.)",
	"removed repeated artist from title":      "removed artist name repeated in title",
	"fell back to uploader":                   "used uploader as artist (no better source)",
	"fell back to channel":                    "used channel name as artist (no better source)",
}

func humanizeReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	var parts []string
	for _, r := range reasons {
		if human, ok := reasonMap[r]; ok {
			parts = append(parts, human)
		} else {
			parts = append(parts, r)
		}
	}
	return strings.Join(parts, ", ")
}

func displayBlank(val string) string {
	if strings.TrimSpace(val) == "" {
		return "—"
	}
	return val
}

func truncate(s string, maxLen int) string {
	if maxLen < 4 {
		maxLen = 4
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
