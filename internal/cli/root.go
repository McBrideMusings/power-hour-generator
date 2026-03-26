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

func init() {
	cobra.EnableCommandSorting = false
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "powerhour",
		Short:         "Power Hour generator CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&projectDir, "project", "", "Path to project directory")
	cmd.PersistentFlags().BoolVar(&outputJSON, "json", false, "Output machine-readable JSON")

	cmd.AddGroup(
		&cobra.Group{ID: "workflow", Title: "Workflow:"},
		&cobra.Group{ID: "inspect", Title: "Inspect:"},
		&cobra.Group{ID: "manage", Title: "Manage:"},
	)

	addTo := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			cmd.AddCommand(c)
		}
	}

	addTo("workflow",
		newInitCmd(),
		newFetchCmd(),
		newRenderCmd(),
		newConcatCmd(),
	)

	addTo("inspect",
		newStatusCmd(),
		newSampleCmd(),
		newValidateCmd(),
		newDoctorCmd(),
		newCheckCmd(),
		newExportCmd(),
		newConfigCmd(),
	)

	convertCmd := newConvertCmd()
	addTo("manage",
		newCacheCmd(),
		newLibraryCmd(),
		newCleanCmd(),
		newToolsCmd(),
		convertCmd,
	)
	// convert operates on a standalone file path; project/json flags don't apply.
	for _, name := range []string{"project", "json"} {
		if f := convertCmd.InheritedFlags().Lookup(name); f != nil {
			f.Hidden = true
		}
	}

	return cmd
}
