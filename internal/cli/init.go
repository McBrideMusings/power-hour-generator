package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
)

const csvHeader = "title,artist,start_time,duration,name,link\n"

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a powerhour project",
		RunE:  runInit,
	}

	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	if err := pp.EnsureRoot(); err != nil {
		return err
	}
	if err := pp.EnsureMetaDirs(); err != nil {
		return err
	}

	logger, closer, err := logx.New(pp)
	if err != nil {
		return err
	}
	defer closer.Close()
	logger.Printf("powerhour init: project=%s", pp.Root)

	created := make([]string, 0, 3)

	if err := ensureCSV(pp, &created, logger); err != nil {
		return err
	}

	if err := ensureConfig(pp, &created, logger); err != nil {
		return err
	}

	if len(created) == 0 {
		cmd.Printf("Project already initialized at %s\n", pp.Root)
		return nil
	}

	cmd.Printf("Initialized project at %s\n", pp.Root)
	for _, entry := range created {
		cmd.Printf("  created %s\n", entry)
	}

	return nil
}

func ensureCSV(pp paths.ProjectPaths, created *[]string, logger Logger) error {
	exists, err := paths.FileExists(pp.CSVFile)
	if err != nil {
		return fmt.Errorf("check csv: %w", err)
	}
	if exists {
		logger.Printf("csv exists: %s", pp.CSVFile)
		return nil
	}

	if err := os.WriteFile(pp.CSVFile, []byte(csvHeader), 0o644); err != nil {
		return fmt.Errorf("write csv: %w", err)
	}
	logger.Printf("created csv: %s", pp.CSVFile)
	*created = append(*created, "powerhour.csv")
	return nil
}

func ensureConfig(pp paths.ProjectPaths, created *[]string, logger Logger) error {
	exists, err := paths.FileExists(pp.ConfigFile)
	if err != nil {
		return fmt.Errorf("check config: %w", err)
	}
	if exists {
		logger.Printf("config exists: %s", pp.ConfigFile)
		return nil
	}

	cfg := config.Default()
	cfg.ApplyDefaults()
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}

	if err := os.WriteFile(pp.ConfigFile, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	logger.Printf("created config: %s", pp.ConfigFile)
	*created = append(*created, "powerhour.yaml")
	return nil
}

// Logger keeps the subset of log.Logger used locally, enabling easy testing.
type Logger interface {
	Printf(format string, v ...any)
}
