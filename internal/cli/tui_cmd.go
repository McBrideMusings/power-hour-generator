package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render/state"
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
	toolStatuses, toolWarning := dashboard.DetectToolStatuses(cmd.Context())

	sw.Stop()

	m := dashboard.NewModel(cfg, pp, collections, timeline, idx, rs, toolWarning, toolStatuses)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	return nil
}
