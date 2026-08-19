package cli

import (
	"context"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
)

var (
	renderConcurrency int
	renderForce       bool
	renderDryRun      bool
	renderIndexArg    []string
	renderNoProgress  bool
)

func newRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render cached clips into individual segment files",
		RunE:  runRender,
	}

	defaultConcurrency := max(runtime.NumCPU(), 1)

	cmd.Flags().IntVar(&renderConcurrency, "concurrency", defaultConcurrency, "Concurrent ffmpeg processes")
	cmd.Flags().BoolVar(&renderForce, "force", false, "Re-render even if segment output already exists")
	cmd.Flags().BoolVar(&renderDryRun, "dry-run", false, "Show what would change without rendering")
	cmd.Flags().BoolVar(&renderNoProgress, "no-progress", false, "Disable interactive progress output")
	cmd.Flags().StringSliceVar(&renderIndexArg, "index", nil, "Limit render to specific 1-based row index or range like 5-10 (repeat flag for multiple)")
	addCollectionRenderFlags(cmd)

	return cmd
}

func runRender(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	glogf, gcloser := logx.StartCommand("render")
	defer gcloser.Close()
	glogf("render started")

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}
	glogf("project resolved: %s", pp.Root)

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())
	glogf("config loaded (%d collections)", len(cfg.Collections))

	if len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	err = runCollectionRender(ctx, cmd, pp, cfg)
	if err != nil {
		glogf("render failed: %v", err)
	} else {
		glogf("render finished")
	}
	return err
}
