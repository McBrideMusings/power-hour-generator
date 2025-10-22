package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect or edit project configuration",
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigEditCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration in YAML",
		RunE:  runConfigShow,
	}
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the project configuration in $EDITOR",
		RunE:  runConfigEdit,
	}
}

func runConfigShow(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}

	data, err := cfg.Marshal()
	if err != nil {
		return err
	}

	fmt.Fprint(cmd.OutOrStdout(), string(data))
	if len(data) == 0 || data[len(data)-1] != '\n' {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func runConfigEdit(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	if err := pp.EnsureRoot(); err != nil {
		return err
	}

	if err := ensureConfigFileExists(pp); err != nil {
		return err
	}

	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}

	parts := splitEditorCommand(editor)
	if len(parts) == 0 {
		return fmt.Errorf("invalid EDITOR value: %q", editor)
	}

	parts = append(parts, pp.ConfigFile)

	execCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	execCmd.Stdout = cmd.OutOrStdout()
	execCmd.Stderr = cmd.ErrOrStderr()
	execCmd.Stdin = cmd.InOrStdin()
	execCmd.Dir = pp.Root

	if err := execCmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}
	return nil
}

func ensureConfigFileExists(pp paths.ProjectPaths) error {
	if _, err := os.Stat(pp.ConfigFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(pp.ConfigFile), 0o755); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}

	cfg := config.Default()
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}

	if err := os.WriteFile(pp.ConfigFile, data, 0o644); err != nil {
		return fmt.Errorf("write default config: %w", err)
	}
	return nil
}

func splitEditorCommand(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	// Basic splitting on whitespace; handles simple EDITOR values like "nano" or "code -w".
	fields := strings.Fields(value)
	return append([]string{}, fields...)
}
