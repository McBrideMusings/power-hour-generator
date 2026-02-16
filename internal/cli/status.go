package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show parsed song entries from the project plan",
		RunE:  runStatus,
	}
	return cmd
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

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	return fmt.Errorf("status command is not yet supported for collections")
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
