package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

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
	// defaultConfigYAML is the raw template written by init. Using a string
	// constant (rather than config.Default().Marshal()) allows embedding YAML
	// comments for documentation and examples.
	defaultConfigYAML = `version: 1
video:
    width: 1920
    height: 1080
    fps: 30
    codec: libx264
    crf: 20
    preset: medium
audio:
    acodec: aac
    bitrate_kbps: 192
    sample_rate: 48000
    channels: 2
    loudnorm:
        enabled: true
        integrated_lufs: -14
        true_peak_db: -1.5
        lra_db: 11
collections:
    songs:
        plan: songs.yaml
        output_dir: songs
        fade: 1.0
        overlays:
            - type: song-info
        link_header: link
        start_header: start_time
        duration_header: duration
    interstitials:
        plan: interstitials.yaml
        output_dir: interstitials
        fade: 1.0
        overlays:
            - type: drink
        link_header: link
        start_header: start_time
        duration_header: duration
timeline:
    sequence:
        # - file: videos/intro.mp4              # optional: play a video before songs start
        #   fade_out: 0.5
        - collection: songs
          count: 30                             # adjust to half your total song count
          interleave:
            collection: interstitials
            every: 1
            placement: between                  # between (default), after, before, around
        # - file: videos/intermission.mp4       # optional: play a video between halves
        #   fade: 1.0
        - collection: songs                     # automatically continues from row 31
          interleave:
            collection: interstitials
            every: 1
            placement: between                  # between (default), after, before, around
        # - file: videos/outro.mp4              # optional: play a video after songs end
        #   fade_in: 0.5
outputs:
    segment_template: $INDEX_PAD3_$SAFE_TITLE
plan:
    headers: {}
    default_duration_s: 60
files:
    plan: ""
    cookies: ""
tools: {}
downloads:
    filename_template: $ID
library: {}
segments_base_dir: segments
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
	glogf, gcloser := logx.StartCommand("init")
	defer gcloser.Close()
	glogf("init started")

	dir, err := resolveInitDir(projectDir, args)
	if err != nil {
		return err
	}
	glogf("target directory: %s", dir)

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

	if err := os.WriteFile(pp.ConfigFile, []byte(defaultConfigYAML), 0o644); err != nil {
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
