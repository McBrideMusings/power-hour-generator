package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
)

var (
	validateSegmentIndexes []int
)

func newValidateSegmentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "segments",
		Short: "Validate rendered segment filenames against the configured template",
		RunE:  runValidateSegments,
	}

	cmd.Flags().IntSliceVar(&validateSegmentIndexes, "index", nil, "Limit validation to specific 1-based row index (repeat flag for multiple)")
	return cmd
}

func runValidateSegments(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	return fmt.Errorf("validate segments is not yet supported for collections")
}
