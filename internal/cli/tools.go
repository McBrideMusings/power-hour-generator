package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"powerhour/internal/tools"
)

var (
	installVersion string
	installForce   bool
)

func newToolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Manage external tools",
	}

	cmd.AddCommand(newToolsListCmd())
	cmd.AddCommand(newToolsInstallCmd())

	return cmd
}

func newToolsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List resolved tool statuses",
		RunE:  runToolsList,
	}
	return cmd
}

func runToolsList(cmd *cobra.Command, _ []string) error {
	statuses, err := tools.Detect(cmd.Context())
	if err != nil {
		return err
	}

	if outputJSON {
		data, err := json.MarshalIndent(statuses, "", "  ")
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		cmd.Println(string(data))
		return nil
	}

	printStatusTable(cmd, statuses)
	return nil
}

func newToolsInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [tool|all]",
		Short: "Install or update managed tools",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runToolsInstall,
	}

	cmd.Flags().StringVar(&installVersion, "version", "", "Specific version to install when supported")
	cmd.Flags().BoolVar(&installForce, "force", false, "Reinstall even if a cached copy exists")

	return cmd
}

func runToolsInstall(cmd *cobra.Command, args []string) error {
	target := "all"
	if len(args) == 1 {
		target = strings.ToLower(args[0])
	}

	var toolsToInstall []string
	if target == "all" {
		toolsToInstall = tools.KnownTools()
	} else {
		if _, ok := tools.Definition(target); !ok {
			return fmt.Errorf("unknown tool: %s", target)
		}
		toolsToInstall = []string{target}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()

	var (
		statuses []tools.Status
		errs     []error
	)

	for _, name := range toolsToInstall {
		status, err := tools.Install(ctx, name, installVersion, tools.InstallOptions{Force: installForce, Version: installVersion})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		statuses = append(statuses, status)
	}

	if outputJSON {
		data, err := json.MarshalIndent(statuses, "", "  ")
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		cmd.Println(string(data))
	} else {
		printStatusTable(cmd, statuses)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func printStatusTable(cmd *cobra.Command, statuses []tools.Status) {
	if len(statuses) == 0 {
		cmd.Println("(no tool statuses)")
		return
	}

	rows := make([]tools.Status, len(statuses))
	copy(rows, statuses)
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Tool < rows[j].Tool
	})

	cmd.Printf("%-10s %-8s %-12s %-7s %s\n", "Tool", "Source", "Version", "OK", "Path")
	for _, st := range rows {
		ok := "no"
		if st.Satisfied {
			ok = "yes"
		}
		path := st.Path
		if path == "" {
			path = "(missing)"
		}
		cmd.Printf("%-10s %-8s %-12s %-7s %s\n", st.Tool, st.Source, st.Version, ok, path)
		if st.Error != "" {
			cmd.Printf("  error: %s\n", st.Error)
		}
	}
}
