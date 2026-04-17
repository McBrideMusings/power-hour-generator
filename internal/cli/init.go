package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/logx"
	"powerhour/internal/paths"
)

const (
	songsPlanYAML = `columns: [title, artist, start_time, duration, link]
defaults:
    start_time: "0:00"
    duration: "60"
rows: []
`
	interstitialsPlanYAML = `columns: [link, start_time, duration]
defaults:
    start_time: "0:00"
    duration: "5"
rows: []
`
	songsPlanCSV         = "title,artist,start_time,duration,link\n"
	songsPlanTSV         = "title\tartist\tstart_time\tduration\tlink\n"
	interstitialsPlanCSV = "link,start_time,duration\n"
	interstitialsPlanTSV = "link\tstart_time\tduration\n"
)

var initPlanFormat string

func renderDefaultConfigYAML(planFormat string) string {
	songsPlan := "songs.yaml"
	interstitialsPlan := "interstitials.yaml"
	switch planFormat {
	case "csv":
		songsPlan = "songs.csv"
		interstitialsPlan = "interstitials.csv"
	case "tsv":
		songsPlan = "songs.tsv"
		interstitialsPlan = "interstitials.tsv"
	}

	// defaultConfigYAML is the raw template written by init. Using a string
	// constant (rather than config.Default().Marshal()) allows embedding YAML
	// comments for documentation and examples.
	return fmt.Sprintf(`version: 1
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
        plan: %s
        output_dir: songs
        fade: 1.0
        overlays:
            - type: song-info
        link_header: link
        start_header: start_time
        duration_header: duration
        cache_search_profile: song_lookup
    interstitials:
        plan: %s
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
          slice: start:30                       # adjust to the first half of your total song count
          interleave:
            collection: interstitials
            every: 1
            placement: between                  # between (default), after, before, around
        # - file: videos/intermission.mp4       # optional: play a video between halves
        #   fade: 1.0
        - collection: songs                     # automatically continues from the remaining songs
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
cache:
    view:
        primary_fields: [title, track]
        secondary_fields: [artist, uploader, channel]
    search_profiles:
        song_lookup:
            search_fields: [title, artist]
            fill:
                title_fields: [title, track]
                artist_fields: [artist, uploader, channel]
                link_fields: [source, links]
library: {}
segments_base_dir: segments
`, songsPlan, interstitialsPlan)
}

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Initialize a powerhour project",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runInit,
	}
	cmd.Flags().StringVar(&initPlanFormat, "plan-format", "yaml", "Collection plan storage format: yaml, csv, or tsv")

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

	planFormat := strings.ToLower(strings.TrimSpace(initPlanFormat))
	switch planFormat {
	case "", "yaml":
		planFormat = "yaml"
	case "csv", "tsv":
	default:
		return fmt.Errorf("unsupported plan format %q (expected yaml, csv, or tsv)", initPlanFormat)
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

	if err := ensureSongsPlan(pp, planFormat, &created, logger); err != nil {
		return err
	}

	if err := ensureInterstitialsPlan(pp, planFormat, &created, logger); err != nil {
		return err
	}

	if err := ensureConfig(pp, planFormat, &created, logger); err != nil {
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

func ensureSongsPlan(pp paths.ProjectPaths, planFormat string, created *[]string, logger Logger) error {
	filename, contents := initPlanTemplate("songs", planFormat)
	planPath := filepath.Join(pp.Root, filename)
	exists, err := paths.FileExists(planPath)
	if err != nil {
		return fmt.Errorf("check songs plan: %w", err)
	}
	if exists {
		logger.Printf("songs plan exists: %s", planPath)
		return nil
	}

	if err := os.WriteFile(planPath, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write songs plan: %w", err)
	}
	logger.Printf("created songs plan: %s", planPath)
	*created = append(*created, filename)
	return nil
}

func ensureInterstitialsPlan(pp paths.ProjectPaths, planFormat string, created *[]string, logger Logger) error {
	filename, contents := initPlanTemplate("interstitials", planFormat)
	planPath := filepath.Join(pp.Root, filename)
	exists, err := paths.FileExists(planPath)
	if err != nil {
		return fmt.Errorf("check interstitials plan: %w", err)
	}
	if exists {
		logger.Printf("interstitials plan exists: %s", planPath)
		return nil
	}

	if err := os.WriteFile(planPath, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write interstitials plan: %w", err)
	}
	logger.Printf("created interstitials plan: %s", planPath)
	*created = append(*created, filename)
	return nil
}

func ensureConfig(pp paths.ProjectPaths, planFormat string, created *[]string, logger Logger) error {
	exists, err := paths.FileExists(pp.ConfigFile)
	if err != nil {
		return fmt.Errorf("check config: %w", err)
	}
	if exists {
		logger.Printf("config exists: %s", pp.ConfigFile)
		return nil
	}

	if err := os.WriteFile(pp.ConfigFile, []byte(renderDefaultConfigYAML(planFormat)), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	logger.Printf("created config: %s", pp.ConfigFile)
	*created = append(*created, "powerhour.yaml")
	return nil
}

func initPlanTemplate(collectionName, planFormat string) (string, string) {
	switch collectionName {
	case "songs":
		switch planFormat {
		case "csv":
			return "songs.csv", songsPlanCSV
		case "tsv":
			return "songs.tsv", songsPlanTSV
		default:
			return "songs.yaml", songsPlanYAML
		}
	case "interstitials":
		switch planFormat {
		case "csv":
			return "interstitials.csv", interstitialsPlanCSV
		case "tsv":
			return "interstitials.tsv", interstitialsPlanTSV
		default:
			return "interstitials.yaml", interstitialsPlanYAML
		}
	default:
		return "", ""
	}
}

// Logger keeps the subset of log.Logger used locally, enabling easy testing.
type Logger interface {
	Printf(format string, v ...any)
}
