package cli

import "github.com/spf13/cobra"

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the media cache",
	}

	cmd.AddCommand(newCacheMigrateCmd())
	return cmd
}
