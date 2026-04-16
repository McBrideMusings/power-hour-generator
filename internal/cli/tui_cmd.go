package cli

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render/state"
	"powerhour/internal/tools"
	"powerhour/internal/tui"
	"powerhour/internal/tui/dashboard"
)

func newTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive dashboard",
		RunE:  runTui,
	}
}

func runTui(cmd *cobra.Command, _ []string) error {
	glogf, gcloser := logx.StartCommand("tui")
	defer gcloser.Close()
	glogf("tui started")

	sw := tui.NewStatusWriter(cmd.ErrOrStderr())
	sw.Update("Resolving project...")

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		sw.Stop()
		return err
	}

	sw.Update("Loading config...")
	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		sw.Stop()
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	if len(cfg.Collections) == 0 {
		sw.Stop()
		return fmt.Errorf("no collections configured")
	}

	sw.Update("Loading collections...")
	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		sw.Stop()
		return err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		sw.Stop()
		return err
	}

	sw.Update("Loading cache and render state...")
	idx, _ := cache.Load(pp)
	rs, _ := state.Load(pp.RenderStateFile)

	sw.Update("Resolving timeline...")
	var timeline []project.TimelineEntry
	if len(cfg.Timeline.Sequence) > 0 {
		timeline, err = project.ResolveTimeline(cfg.Timeline, collections)
		if err != nil {
			sw.Stop()
			return fmt.Errorf("resolve timeline: %w", err)
		}
	}

	sw.Update("Detecting tools...")
	toolStatuses, toolWarning := detectToolStatuses(cmd.Context())

	sw.Stop()

	m := dashboard.NewModel(cfg, pp, collections, timeline, idx, rs, toolWarning, toolStatuses)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	return nil
}

func detectToolStatuses(ctx context.Context) ([]dashboard.ToolStatus, string) {
	statuses, err := tools.Detect(ctx)
	if err != nil {
		return nil, ""
	}

	statusByName := make(map[string]tools.Status, len(statuses))
	for _, status := range statuses {
		statusByName[status.Tool] = status
	}

	result := make([]dashboard.ToolStatus, 0, len(tools.KnownTools()))
	var warning string

	for _, name := range tools.KnownTools() {
		s, ok := statusByName[name]
		if !ok {
			continue
		}
		ts := dashboard.ToolStatus{
			Name:          s.Tool,
			Optional:      s.Optional,
			Available:     s.Path != "",
			Version:       s.Version,
			Path:          s.Path,
			InstallMethod: s.InstallMethod,
		}
		if !s.Optional && !s.Satisfied {
			ts.UpdateAvail = "not satisfied"
			if warning == "" {
				warning = s.Tool
			}
		}
		result = append(result, ts)
	}

	// Check for update notices.
	notices := tools.CheckForUpdates(ctx, statuses)
	for _, n := range notices {
		if warning == "" {
			warning = n.Tool + " update"
		}
		for i := range result {
			if result[i].Name == n.Tool {
				result[i].UpdateAvail = n.CurrentVersion + " → " + n.LatestVersion
			}
		}
	}

	return result, warning
}
