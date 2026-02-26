package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/state"
	"powerhour/internal/tools"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check project health",
		RunE:  runDoctor,
	}
}

type healthCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok", "warning", "error"
	Summary string `json:"summary"`
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}
	exists, err := paths.DirExists(pp.Root)
	if err != nil {
		return fmt.Errorf("stat project dir: %w", err)
	}
	if !exists {
		return fmt.Errorf("project directory does not exist: %s", pp.Root)
	}

	var checks []healthCheck

	// Tools check
	checks = append(checks, checkTools(cmd))

	// Config check
	cfg, cfgErr := config.Load(pp.ConfigFile)
	checks = append(checks, checkConfig(pp, cfg, cfgErr))

	if cfgErr != nil {
		// Can't proceed with further checks without config
		return writeDoctorResult(cmd, pp.Root, checks)
	}

	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	// Sources + Segments checks require collections
	if cfg.Collections != nil && len(cfg.Collections) > 0 {
		resolver, err := project.NewCollectionResolver(cfg, pp)
		if err == nil {
			collections, loadErr := resolver.LoadCollections()
			if loadErr == nil && len(collections) > 0 {
				checks = append(checks, checkSources(pp, collections))
				checks = append(checks, checkSegments(pp, cfg, resolver, collections))
			}
		}
	}

	// Timeline check
	if len(cfg.Timeline.Sequence) > 0 {
		if cfg.Collections != nil && len(cfg.Collections) > 0 {
			resolver, err := project.NewCollectionResolver(cfg, pp)
			if err == nil {
				collections, loadErr := resolver.LoadCollections()
				if loadErr == nil {
					checks = append(checks, checkTimeline(cfg, collections))
				}
			}
		}
	}

	return writeDoctorResult(cmd, pp.Root, checks)
}

func checkTools(cmd *cobra.Command) healthCheck {
	statuses, err := tools.Detect(cmd.Context())
	if err != nil {
		return healthCheck{Name: "Tools", Status: "error", Summary: err.Error()}
	}

	var satisfied, total int
	var toolInfo []string
	for _, st := range statuses {
		total++
		if st.Satisfied {
			satisfied++
			label := st.Tool
			if st.Version != "" {
				label += " " + st.Version
			}
			toolInfo = append(toolInfo, label)
		}
	}

	if satisfied == total {
		return healthCheck{Name: "Tools", Status: "ok", Summary: joinComma(toolInfo)}
	}
	return healthCheck{
		Name:    "Tools",
		Status:  "error",
		Summary: fmt.Sprintf("%d of %d tools satisfied", satisfied, total),
	}
}

func checkConfig(pp paths.ProjectPaths, cfg config.Config, cfgErr error) healthCheck {
	if cfgErr != nil {
		return healthCheck{Name: "Config", Status: "error", Summary: cfgErr.Error()}
	}

	validations := cfg.ValidateStrict(pp.Root, render.ValidSegmentTokens())
	var warnings, errors int
	for _, v := range validations {
		switch v.Level {
		case "warning":
			warnings++
		case "error":
			errors++
		}
	}

	nProfiles := len(cfg.Profiles)
	nCollections := len(cfg.Collections)
	summary := fmt.Sprintf("%d profiles, %d collections", nProfiles, nCollections)

	if errors > 0 {
		return healthCheck{Name: "Config", Status: "error", Summary: fmt.Sprintf("%s; %d errors", summary, errors)}
	}
	if warnings > 0 {
		return healthCheck{Name: "Config", Status: "warning", Summary: fmt.Sprintf("%s; %d warnings", summary, warnings)}
	}
	return healthCheck{Name: "Config", Status: "ok", Summary: summary}
}

func checkSources(pp paths.ProjectPaths, collections map[string]project.Collection) healthCheck {
	idx, err := cache.Load(pp)
	if err != nil {
		return healthCheck{Name: "Sources", Status: "warning", Summary: "could not load cache index"}
	}

	var total, cached int
	for _, coll := range collections {
		for _, row := range coll.Rows {
			total++
			r := row.ToRow()
			_, ok, err := resolveEntryForRow(pp, idx, r)
			if err == nil && ok {
				cached++
			}
		}
	}

	if cached == total {
		return healthCheck{Name: "Sources", Status: "ok", Summary: fmt.Sprintf("%d of %d rows cached", cached, total)}
	}
	missing := total - cached
	return healthCheck{
		Name:    "Sources",
		Status:  "warning",
		Summary: fmt.Sprintf("%d of %d rows missing cached source", missing, total),
	}
}

func checkSegments(pp paths.ProjectPaths, cfg config.Config, resolver *project.CollectionResolver, collections map[string]project.Collection) healthCheck {
	rs, err := state.Load(pp.RenderStateFile)
	if err != nil {
		return healthCheck{Name: "Segments", Status: "warning", Summary: "could not load render state"}
	}

	clips, err := resolver.BuildCollectionClips(collections)
	if err != nil {
		return healthCheck{Name: "Segments", Status: "error", Summary: err.Error()}
	}

	tmpl := cfg.SegmentFilenameTemplate()
	var segments []render.Segment
	for _, collClip := range clips {
		clip := collClip.Clip
		clip.Row.DurationSeconds = clip.DurationSeconds
		if clip.Row.Index <= 0 {
			clip.Row.Index = clip.TypeIndex
			if clip.Row.Index <= 0 {
				clip.Row.Index = clip.Sequence
			}
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

		outputDir := collClip.OutputDir
		if !filepath.IsAbs(outputDir) {
			outputDir = filepath.Join(pp.SegmentsDir, outputDir)
		}
		seg.OutputPath = filepath.Join(outputDir, render.SegmentBaseName(tmpl, seg)+".mp4")
		segments = append(segments, seg)
	}

	actions := state.DetectChanges(rs, segments, cfg, tmpl, false)
	var rendered, staleCount, missingCount int
	for _, a := range actions {
		if a.Action == state.ActionSkip {
			rendered++
		} else {
			switch a.Reason {
			case state.ReasonNew, state.ReasonOutputMissing:
				missingCount++
			default:
				staleCount++
			}
		}
	}

	total := len(actions)
	if rendered == total {
		return healthCheck{Name: "Segments", Status: "ok", Summary: fmt.Sprintf("%d segments rendered", rendered)}
	}

	parts := []string{}
	if staleCount > 0 {
		parts = append(parts, fmt.Sprintf("%d stale", staleCount))
	}
	if missingCount > 0 {
		parts = append(parts, fmt.Sprintf("%d missing", missingCount))
	}
	return healthCheck{
		Name:    "Segments",
		Status:  "warning",
		Summary: joinComma(parts),
	}
}

func checkTimeline(cfg config.Config, collections map[string]project.Collection) healthCheck {
	entries, err := project.ResolveTimeline(cfg.Timeline, collections)
	if err != nil {
		return healthCheck{Name: "Timeline", Status: "error", Summary: err.Error()}
	}
	return healthCheck{Name: "Timeline", Status: "ok", Summary: fmt.Sprintf("%d entries", len(entries))}
}

func writeDoctorResult(cmd *cobra.Command, projectRoot string, checks []healthCheck) error {
	if outputJSON {
		data, err := json.MarshalIndent(checks, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	bold := lipgloss.NewStyle().Bold(true).Inline(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Inline(true)
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Inline(true)
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Inline(true)

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, bold.Render("PROJECT HEALTH:")+" "+projectRoot)

	for _, c := range checks {
		var statusStr string
		switch c.Status {
		case "ok":
			statusStr = green.Render("OK")
		case "warning":
			statusStr = yellow.Render("WARN")
		case "error":
			statusStr = red.Render("ERROR")
		}
		fmt.Fprintf(out, "  %-12s %s    %s\n", c.Name+":", statusStr, c.Summary)
	}

	return nil
}

func joinComma(items []string) string {
	if len(items) == 0 {
		return ""
	}
	result := items[0]
	for _, item := range items[1:] {
		result += ", " + item
	}
	return result
}
