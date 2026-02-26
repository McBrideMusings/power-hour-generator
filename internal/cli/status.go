package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/state"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show project status with per-row cache and render state",
		RunE:  runStatus,
	}
	return cmd
}

// timelineEntryOutput is the JSON-serializable form of a resolved timeline entry.
type timelineEntryOutput struct {
	Sequence    int    `json:"sequence"`
	Collection  string `json:"collection"`
	Index       int    `json:"index"`
	SegmentPath string `json:"segment_path"`
}

// rowStatus captures per-row cache and render status.
type rowStatus struct {
	Collection   string `json:"collection"`
	Index        int    `json:"index"`
	Title        string `json:"title"`
	CacheStatus  string `json:"cache_status"`
	RenderStatus string `json:"render_status"`
	RenderReason string `json:"render_reason,omitempty"`
}

// collectionSummary aggregates row statuses for a collection.
type collectionSummary struct {
	Name         string `json:"name"`
	Total        int    `json:"total"`
	Cached       int    `json:"cached"`
	CacheMissing int    `json:"cache_missing"`
	Rendered     int    `json:"rendered"`
	Stale        int    `json:"stale"`
	Missing      int    `json:"missing"`
}

// collectionPalette maps sorted collection index to a terminal color.
// Index 0 gets no color (default). Subsequent collections get distinct colors.
var collectionPalette = []string{"", "3", "6", "5", "2", "4", "1"}

func buildCollectionStyles(names []string) map[string]lipgloss.Style {
	styles := make(map[string]lipgloss.Style, len(names))
	for i, name := range names {
		color := collectionPalette[i%len(collectionPalette)]
		if color == "" {
			styles[name] = lipgloss.NewStyle().Inline(true)
		} else {
			styles[name] = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Inline(true)
		}
	}
	return styles
}

func runStatus(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		return err
	}

	// Load cache index and render state
	idx, _ := cache.Load(pp)
	rs, _ := state.Load(pp.RenderStateFile)

	// Build per-row statuses
	tmpl := cfg.SegmentFilenameTemplate()
	rows, summaries := buildRowStatuses(pp, cfg, idx, rs, resolver, collections, tmpl)

	// Resolve timeline
	var timelineEntries []timelineEntryOutput
	hasTimeline := len(cfg.Timeline.Sequence) > 0

	if hasTimeline {
		resolved, err := project.ResolveTimeline(cfg.Timeline, collections)
		if err != nil {
			return fmt.Errorf("resolve timeline: %w", err)
		}
		timelineEntries = make([]timelineEntryOutput, len(resolved))
		for i, e := range resolved {
			timelineEntries[i] = timelineEntryOutput{
				Sequence:    e.Sequence,
				Collection:  e.Collection,
				Index:       e.Index,
				SegmentPath: e.SegmentPath,
			}
		}
	}

	payload := struct {
		Project     string                `json:"project"`
		Summaries   []collectionSummary   `json:"summaries"`
		Rows        []rowStatus           `json:"rows"`
		HasTimeline bool                  `json:"has_timeline"`
		Timeline    []timelineEntryOutput `json:"timeline,omitempty"`
	}{
		Project:     pp.Root,
		Summaries:   summaries,
		Rows:        rows,
		HasTimeline: hasTimeline,
		Timeline:    timelineEntries,
	}

	if outputJSON {
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	printStatusResult(pp.Root, collections, summaries, rows, timelineEntries)
	return nil
}

func buildRowStatuses(pp paths.ProjectPaths, cfg config.Config, idx *cache.Index, rs *state.RenderState, resolver *project.CollectionResolver, collections map[string]project.Collection, tmpl string) ([]rowStatus, []collectionSummary) {
	// Sort collection names for deterministic output
	sortedNames := make([]string, 0, len(collections))
	for name := range collections {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

	var allRows []rowStatus
	var summaries []collectionSummary

	globalHash := state.GlobalConfigHash(cfg)
	configChanged := globalHash != rs.GlobalConfigHash

	for _, collName := range sortedNames {
		coll := collections[collName]
		summary := collectionSummary{Name: collName, Total: len(coll.Rows)}

		for _, collRow := range coll.Rows {
			r := collRow.ToRow()

			// Cache status
			cacheStatus := "missing"
			link := strings.TrimSpace(r.Link)
			isURL := strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "youtu")

			if isURL {
				_, ok, err := resolveEntryForRow(pp, idx, r)
				if err == nil && ok {
					cacheStatus = "cached"
				}
			} else {
				// Local file — check if it exists
				localPath := link
				if !filepath.IsAbs(localPath) {
					localPath = filepath.Join(pp.Root, strings.Trim(localPath, "'\""))
				}
				if _, err := os.Stat(localPath); err == nil {
					cacheStatus = "cached"
				}
			}

			// Build segment for render status
			clip := project.Clip{
				Sequence:        r.Index,
				ClipType:        project.ClipType(collName),
				TypeIndex:       r.Index,
				Row:             r,
				SourceKind:      project.SourceKindPlan,
				DurationSeconds: r.DurationSeconds,
				OverlayProfile:  coll.Profile,
			}
			if coll.Profile != "" {
				if prof, ok := resolver.Profile(coll.Profile); ok {
					if prof.FadeInSec != nil {
						clip.FadeInSeconds = *prof.FadeInSec
					}
					if prof.FadeOutSec != nil {
						clip.FadeOutSeconds = *prof.FadeOutSec
					}
				}
			}

			clip.Row.DurationSeconds = clip.DurationSeconds
			if clip.Row.Index <= 0 {
				clip.Row.Index = clip.TypeIndex
			}

			var prof project.ResolvedProfile
			var segs []config.OverlaySegment
			if clip.OverlayProfile != "" {
				if p, ok := resolver.Profile(clip.OverlayProfile); ok {
					prof = p
					segs = p.ResolveSegments()
				}
			}

			seg := render.Segment{
				Clip:     clip,
				Profile:  prof,
				Segments: segs,
			}

			outputDir := coll.OutputDir
			if !filepath.IsAbs(outputDir) {
				outputDir = filepath.Join(pp.SegmentsDir, outputDir)
			}
			seg.OutputPath = filepath.Join(outputDir, render.SegmentBaseName(tmpl, seg)+".mp4")

			// Render status
			renderStatus := "missing"
			renderReason := ""
			if prior, exists := rs.Segments[seg.OutputPath]; exists {
				if configChanged {
					renderStatus = "stale"
					renderReason = "config changed"
				} else {
					currentHash := state.SegmentInputHash(seg, tmpl)
					if currentHash != prior.InputHash {
						renderStatus = "stale"
						renderReason = "input changed"
					} else if _, err := os.Stat(seg.OutputPath); os.IsNotExist(err) {
						renderStatus = "stale"
						renderReason = "output missing"
					} else {
						renderStatus = "rendered"
					}
				}
			}

			// Update summary
			if cacheStatus == "cached" {
				summary.Cached++
			} else {
				summary.CacheMissing++
			}
			switch renderStatus {
			case "rendered":
				summary.Rendered++
			case "stale":
				summary.Stale++
			default:
				summary.Missing++
			}

			title := sanitizeField(r.CustomFields["title"])
			if title == "" {
				title = sanitizeField(r.Title)
			}

			allRows = append(allRows, rowStatus{
				Collection:   collName,
				Index:        r.Index,
				Title:        title,
				CacheStatus:  cacheStatus,
				RenderStatus: renderStatus,
				RenderReason: renderReason,
			})
		}

		summaries = append(summaries, summary)
	}

	return allRows, summaries
}

func timelineEntryLabel(e timelineEntryOutput, collections map[string]project.Collection) string {
	if c, ok := collections[e.Collection]; ok && e.Index >= 1 && e.Index <= len(c.Rows) {
		row := c.Rows[e.Index-1]
		title := sanitizeField(row.CustomFields["title"])
		artist := sanitizeField(row.CustomFields["artist"])
		if title != "" && artist != "" {
			return title + " — " + artist
		}
		if title != "" {
			return title
		}
		// Fall back to link: use basename for file paths, raw value for URLs
		link := strings.Trim(row.Link, `'"`)
		if link != "" {
			if strings.HasPrefix(link, "/") || strings.HasPrefix(link, "./") {
				return filepath.Base(link)
			}
			return link
		}
	}
	return e.Collection
}

// sanitizeField strips whitespace and embedded tabs/newlines from a CSV field
// value, preventing misaligned output when a field contains the whole row.
func sanitizeField(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\t\n\r"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func printStatusResult(projectPath string, collections map[string]project.Collection, summaries []collectionSummary, rows []rowStatus, timeline []timelineEntryOutput) {
	bold := lipgloss.NewStyle().Bold(true).Inline(true)
	faint := lipgloss.NewStyle().Faint(true).Inline(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Inline(true)
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Inline(true)

	// Sort collection names for consistent color assignment
	sortedNames := make([]string, 0, len(collections))
	for name := range collections {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)
	collStyles := buildCollectionStyles(sortedNames)

	fmt.Println(bold.Render("Project:") + " " + projectPath)
	fmt.Println()

	// Collection summaries
	if len(summaries) > 0 {
		fmt.Println(bold.Render("Collections:"))
		for _, s := range summaries {
			style := collStyles[s.Name]
			cachePart := fmt.Sprintf("%d cached", s.Cached)
			if s.CacheMissing > 0 {
				cachePart += fmt.Sprintf(", %d missing", s.CacheMissing)
			}

			renderPart := fmt.Sprintf("%d rendered", s.Rendered)
			if s.Stale > 0 {
				renderPart += fmt.Sprintf(", %d stale", s.Stale)
			}
			if s.Missing > 0 {
				renderPart += fmt.Sprintf(", %d missing", s.Missing)
			}

			fmt.Printf("  %s  %d rows   %s   %s\n",
				style.Width(14).Render(s.Name),
				s.Total,
				cachePart,
				renderPart,
			)
		}
		fmt.Println()
	}

	// Per-row table
	if len(rows) > 0 {
		fmt.Printf("  %4s  %-14s %-30s %-10s %s\n",
			bold.Render("#"),
			bold.Render("COLLECTION"),
			bold.Render("TITLE"),
			bold.Render("CACHE"),
			bold.Render("RENDER"),
		)

		for _, r := range rows {
			style := collStyles[r.Collection]
			title := r.Title
			if len(title) > 28 {
				title = title[:25] + "..."
			}

			cacheLabel := faint.Render(r.CacheStatus)
			if r.CacheStatus == "cached" {
				cacheLabel = green.Render("cached")
			}

			renderLabel := faint.Render(r.RenderStatus)
			if r.RenderStatus == "rendered" {
				renderLabel = green.Render("rendered")
			} else if r.RenderStatus == "stale" {
				reason := ""
				if r.RenderReason != "" {
					reason = " (" + r.RenderReason + ")"
				}
				renderLabel = yellow.Render("stale") + faint.Render(reason)
			}

			fmt.Printf("  %4d  %s %-30s %-10s %s\n",
				r.Index,
				style.Width(14).Render(r.Collection),
				title,
				cacheLabel,
				renderLabel,
			)
		}
		fmt.Println()
	}

	if len(timeline) == 0 {
		fmt.Println(faint.Render("No timeline configured."))
		return
	}

	fmt.Println(bold.Render("Timeline:") + fmt.Sprintf(" %d entries", len(timeline)))
	fmt.Println()
	fmt.Printf("  %4s  %s\n", bold.Render("#"), bold.Render("Title"))

	for _, e := range timeline {
		label := timelineEntryLabel(e, collections)
		style := collStyles[e.Collection]
		seg := ""
		if e.SegmentPath != "" {
			seg = "  " + green.Render("✓") + " " + filepath.Base(e.SegmentPath)
		}
		fmt.Printf("  %4d  %s%s\n", e.Sequence, style.Render(label), seg)
	}
}

func ensureProjectDirs(pp paths.ProjectPaths) error {
	exists, err := paths.DirExists(pp.Root)
	if err != nil {
		return fmt.Errorf("stat project dir: %w", err)
	}
	if !exists {
		return fmt.Errorf("project directory does not exist: %s", pp.Root)
	}

	if err := pp.EnsureMetaDirs(); err != nil {
		return err
	}

	return nil
}
