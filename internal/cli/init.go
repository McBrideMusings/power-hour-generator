package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
)

const (
	songsPlanYAML = `# Add your songs here. Each entry needs a link and a start_time.
# - title: Song Title
#   artist: Artist Name
#   start_time: "1:30"
#   link: https://youtube.com/watch?v=...
#   name: Person Name
#   duration: 60
`
	interstitialsPlanYAML = `# Add interstitial clips here.
# - link: path/to/clip.mp4
#   start_time: "0:00"
#   duration: 7
`
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Initialize a powerhour project",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runInit,
	}

	return cmd
}

func resolveInitDir(projectFlag string, args []string) (string, error) {
	if projectFlag != "" {
		return projectFlag, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	if len(args) > 0 {
		if args[0] == "." {
			return cwd, nil
		}
		return filepath.Join(cwd, args[0]), nil
	}

	return nextAvailableDir(cwd)
}

func nextAvailableDir(base string) (string, error) {
	for i := 1; ; i++ {
		candidate := filepath.Join(base, fmt.Sprintf("powerhour-%d", i))
		exists, err := paths.DirExists(candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
}

func runInit(cmd *cobra.Command, args []string) error {
	dir, err := resolveInitDir(projectDir, args)
	if err != nil {
		return err
	}

	pp, err := paths.Resolve(dir)
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

	created := make([]string, 0, 4)

	if err := ensureSongsPlan(pp, &created, logger); err != nil {
		return err
	}

	if err := ensureInterstitialsPlan(pp, &created, logger); err != nil {
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

func ensureSongsPlan(pp paths.ProjectPaths, created *[]string, logger Logger) error {
	planPath := filepath.Join(pp.Root, "songs.yaml")
	exists, err := paths.FileExists(planPath)
	if err != nil {
		return fmt.Errorf("check songs plan: %w", err)
	}
	if exists {
		logger.Printf("songs plan exists: %s", planPath)
		return nil
	}

	if err := os.WriteFile(planPath, []byte(songsPlanYAML), 0o644); err != nil {
		return fmt.Errorf("write songs plan: %w", err)
	}
	logger.Printf("created songs plan: %s", planPath)
	*created = append(*created, "songs.yaml")
	return nil
}

func ensureInterstitialsPlan(pp paths.ProjectPaths, created *[]string, logger Logger) error {
	planPath := filepath.Join(pp.Root, "interstitials.yaml")
	exists, err := paths.FileExists(planPath)
	if err != nil {
		return fmt.Errorf("check interstitials plan: %w", err)
	}
	if exists {
		logger.Printf("interstitials plan exists: %s", planPath)
		return nil
	}

	if err := os.WriteFile(planPath, []byte(interstitialsPlanYAML), 0o644); err != nil {
		return fmt.Errorf("write interstitials plan: %w", err)
	}
	logger.Printf("created interstitials plan: %s", planPath)
	*created = append(*created, "interstitials.yaml")
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
