package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"powerhour/internal/tools"
)

// printUpdateNotices checks for tool updates and prints notices to stderr.
// Called from PersistentPostRun on the root command. The check is gated by a
// 24-hour cache so most runs only read a small JSON file.
func printUpdateNotices(cmd *cobra.Command) {
	if outputJSON {
		return
	}

	manifest, err := tools.LoadManifestPublic()
	if err != nil {
		return
	}

	var statuses []tools.Status
	for _, entry := range manifest.Entries {
		statuses = append(statuses, tools.Status{
			Tool:          entry.Tool,
			Version:       entry.Version,
			InstallMethod: entry.InstallMethod,
			Satisfied:     true,
		})
	}

	if len(statuses) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()

	notices := tools.CheckForUpdates(ctx, statuses)
	if len(notices) == 0 {
		return
	}

	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Inline(true)
	faint := lipgloss.NewStyle().Faint(true).Inline(true)

	fmt.Fprintln(os.Stderr)
	for _, n := range notices {
		fmt.Fprintf(os.Stderr, "%s %s → %s  %s\n",
			cyan.Render("update available:"),
			n.Tool+" "+n.CurrentVersion,
			cyan.Render(n.LatestVersion),
			faint.Render("run '"+n.UpdateCommand()+"'"),
		)
		tools.MarkNotified(n.Tool, n.LatestVersion)
	}
}
