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

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show resolved timeline for the project",
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
	pp = paths.ApplyGlobalCache(pp, cfg.GlobalCacheEnabled())

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
		HasTimeline bool                  `json:"has_timeline"`
		Timeline    []timelineEntryOutput `json:"timeline,omitempty"`
	}{
		Project:     pp.Root,
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

	printStatusResult(pp.Root, collections, timelineEntries)
	return nil
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

func printStatusResult(projectPath string, collections map[string]project.Collection, timeline []timelineEntryOutput) {
	bold := lipgloss.NewStyle().Bold(true).Inline(true)
	faint := lipgloss.NewStyle().Faint(true).Inline(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Inline(true)

	// Sort collection names once; used for consistent color assignment and display.
	sortedNames := make([]string, 0, len(collections))
	for name := range collections {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)
	collStyles := buildCollectionStyles(sortedNames)

	fmt.Println(bold.Render("Project:") + " " + projectPath)
	fmt.Println()

	if len(collections) > 0 {
		fmt.Println(bold.Render("Collections:"))
		for _, name := range sortedNames {
			c := collections[name]
			n := len(c.Rows)
			unit := "rows"
			if n == 1 {
				unit = "row"
			}
			fmt.Printf("  %s  %d %s\n", collStyles[name].Width(20).Render(name), n, unit)
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
