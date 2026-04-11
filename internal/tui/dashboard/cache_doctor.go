package dashboard

import (
	"fmt"
	"sort"
	"strings"

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

func (o *cacheDoctorOverlay) skipCurrent() {
	if o.cursor < len(o.findings)-1 {
		o.cursor++
		o.loadCurrentEntry()
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
		cur := o.activeCursor()
		if cur > 0 {
			o.setActiveCursor(cur - 1)
		}
		return false, false
	case tea.KeyRight:
		text := o.activeText()
		cur := o.activeCursor()
		if cur < len(text) {
			o.setActiveCursor(cur + 1)
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
			o.setActiveText(text[:cur-1]+text[cur:], cur-1)
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
func (o *cacheDoctorOverlay) view() string {
	if len(o.findings) == 0 {
		return faint.Render("No entries need attention.")
	}

	item := o.findings[o.cursor]
	entry := item.entry
	finding := item.finding

	var b strings.Builder

	// Header line.
	confStyle := confidenceStyle(finding.Confidence)
	header := fmt.Sprintf("CACHE DOCTOR (%d of %d)", o.cursor+1, len(o.findings))
	confBadge := confStyle.Render(confidenceLabel(finding.Confidence))
	appliedStr := ""
	if o.applied > 0 {
		appliedStr = "  " + countGreen.Render(fmt.Sprintf("%d saved", o.applied))
	}
	b.WriteString(sectionLabel.Render(header) + "  " + confBadge + appliedStr)
	b.WriteByte('\n')

	// Source info.
	source := entry.Source
	if source == "" && len(entry.Links) > 0 {
		source = entry.Links[0]
	}
	if source != "" {
		b.WriteString(faint.Render("SOURCE  ") + truncate(source, o.termWidth-12))
		b.WriteByte('\n')
	}
	b.WriteString(faint.Render("FILE    ") + truncate(entry.CachedPath, o.termWidth-12))
	b.WriteByte('\n')
	b.WriteByte('\n')


	// Title field.
	titleLabel := "TITLE"
	if o.activeField == 0 {
		titleLabel = editStyle.Render(titleLabel)
	} else {
		titleLabel = bold.Render(titleLabel)
	}
	currentTitle := displayBlank(finding.CurrentTitle)
	fmt.Fprintf(&b, " %s\n", titleLabel)
	fmt.Fprintf(&b, "   Current:  %s\n", faint.Render(currentTitle))
	if o.activeField == 0 {
		fmt.Fprintf(&b, "   New:      %s\n", editStyle.Render(renderCursorField(o.editTitle, o.titleCursor)))
	} else {
		fmt.Fprintf(&b, "   New:      %s\n", o.editTitle)
	}
	b.WriteByte('\n')

	// Artist field.
	artistLabel := "ARTIST"
	if o.activeField == 1 {
		artistLabel = editStyle.Render(artistLabel)
	} else {
		artistLabel = bold.Render(artistLabel)
	}
	currentArtist := displayBlank(finding.CurrentArtist)
	fmt.Fprintf(&b, " %s\n", artistLabel)
	fmt.Fprintf(&b, "   Current:  %s\n", faint.Render(currentArtist))
	if o.activeField == 1 {
		fmt.Fprintf(&b, "   New:      %s\n", editStyle.Render(renderCursorField(o.editArtist, o.artistCursor)))
		if suggestions := o.artistSuggestions(); len(suggestions) > 0 {
			for i, s := range suggestions {
				prefix := "   "
				if i == 0 {
					prefix = " → "
					s += faint.Render("  (Enter to accept)")
				}
				fmt.Fprintf(&b, "%s%s\n", faint.Render(prefix), faint.Render(s))
			}
		}
	} else {
		fmt.Fprintf(&b, "   New:      %s\n", o.editArtist)
	}
	b.WriteByte('\n')

	// Context section.
	hasContext := entry.Uploader != "" || entry.Channel != "" || entry.Track != "" || entry.Album != ""
	if hasContext {
		b.WriteString(bold.Render(" CONTEXT"))
		b.WriteByte('\n')
		if entry.Uploader != "" {
			fmt.Fprintf(&b, "   Uploader:  %s\n", entry.Uploader)
		}
		if entry.Channel != "" {
			fmt.Fprintf(&b, "   Channel:   %s\n", entry.Channel)
		}
		if entry.Track != "" {
			fmt.Fprintf(&b, "   Track:     %s\n", entry.Track)
		}
		if entry.Album != "" {
			fmt.Fprintf(&b, "   Album:     %s\n", entry.Album)
		}
		b.WriteByte('\n')
	}

	// Reasons.
	if humanReasons := humanizeReasons(finding.Reasons); humanReasons != "" {
		b.WriteString(faint.Render(" " + humanReasons))
		b.WriteByte('\n')
	}

	// Requery spinner or status.
	if o.requerying {
		b.WriteString(countYellow.Render(busySpinner(o.tick) + " Fetching metadata from yt-dlp..."))
		b.WriteByte('\n')
	} else if o.requeryStatus != "" {
		b.WriteString(faint.Render(" " + o.requeryStatus))
		b.WriteByte('\n')
	}

	return b.String()
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

// renderCursorField renders a text value with a block cursor at the given position.
func renderCursorField(text string, cursor int) string {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	if cursor == len(text) {
		return text + "█"
	}
	return text[:cursor] + "█" + text[cursor:]
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
	"used track as title":                    "used track metadata as title",
	"applied artist alias":                   "matched a known artist alias",
	"split artist/title from title field":    "split \"Artist - Title\" format",
	"mapped uploader/channel to artist alias": "matched uploader/channel to known artist",
	"removed video suffix noise":             "cleaned title (removed Official Video, HD, etc.)",
	"removed repeated artist from title":     "removed artist name repeated in title",
	"fell back to uploader":                  "used uploader as artist (no better source)",
	"fell back to channel":                   "used channel name as artist (no better source)",
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
