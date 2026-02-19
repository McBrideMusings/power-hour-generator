package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	projectDir string
	outputJSON bool
)

// Execute runs the root cobra command.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "powerhour",
		Short: "Power Hour generator CLI",
	}

	cmd.PersistentFlags().StringVar(&projectDir, "project", "", "Path to project directory")
	cmd.PersistentFlags().BoolVar(&outputJSON, "json", false, "Output machine-readable JSON")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newCheckCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newToolsCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newValidateCmd())
	cmd.AddCommand(newFetchCmd())
	cmd.AddCommand(newRenderCmd())
	cmd.AddCommand(newMigrateCmd())

	return cmd
}
