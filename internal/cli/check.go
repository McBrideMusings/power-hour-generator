package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/render"
	"powerhour/internal/tools"
)

var checkStrict bool

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check external tool availability",
		RunE:  runCheck,
	}

	cmd.Flags().BoolVar(&checkStrict, "strict", false, "fail when required tools are missing or outdated")

	return cmd
}

func runCheck(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	exists, err := paths.DirExists(pp.Root)
	if err != nil {
		return fmt.Errorf("stat project dir: %w", err)
	}
	if !exists {
		return fmt.Errorf("project directory does not exist: %s", pp.Root)
	}

	if err := pp.EnsureMetaDirs(); err != nil {
		return err
	}

	logger, closer, err := logx.New(pp)
	if err != nil {
		return err
	}
	defer closer.Close()
	logger.Printf("powerhour check: project=%s", pp.Root)

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	logger.Printf("loaded config version=%d", cfg.Version)

	detectCtx := tools.WithMinimums(cmd.Context(), cfg.ToolMinimums())
	statuses, err := tools.Detect(detectCtx)
	if err != nil {
		return err
	}

	for _, st := range statuses {
		logger.Printf("tool %s: source=%s version=%s satisfied=%v error=%s", st.Tool, st.Source, st.Version, st.Satisfied, st.Error)
	}

	var validations []config.ValidationResult
	if checkStrict {
		if err := ensureStrict(statuses); err != nil {
			return err
		}
		validations = cfg.ValidateStrict(pp.Root, render.ValidSegmentTokens())
		for _, v := range validations {
			if v.Level == "warning" {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", v.Message)
			}
		}
		var errs []string
		for _, v := range validations {
			if v.Level == "error" {
				errs = append(errs, v.Message)
			}
		}
		if len(errs) > 0 {
			return errors.New("config validation failed: " + strings.Join(errs, "; "))
		}
	}

	payload := struct {
		Project     string                    `json:"project"`
		Tools       []tools.Status            `json:"tools"`
		Validations []config.ValidationResult `json:"validations,omitempty"`
	}{
		Project:     pp.Root,
		Tools:       statuses,
		Validations: validations,
	}

	if outputJSON {
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		cmd.Println(string(data))
		return nil
	}

	printCheckResult(cmd, payload.Project, payload.Tools)
	return nil
}

func printCheckResult(cmd *cobra.Command, project string, statuses []tools.Status) {
	bold := lipgloss.NewStyle().Bold(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	faint := lipgloss.NewStyle().Faint(true)

	cmd.Println(bold.Render("Project:") + " " + project)
	cmd.Println()

	sorted := make([]tools.Status, len(statuses))
	copy(sorted, statuses)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Tool < sorted[j].Tool
	})

	for _, st := range sorted {
		if st.Satisfied {
			headline := green.Render("✓") + " " + bold.Render(st.Tool)
			if st.Version != "" {
				headline += " v" + st.Version
			}
			if st.Minimum != "" {
				headline += faint.Render(" (minimum: " + st.Minimum + ")")
			}
			cmd.Println(headline)

			detail := string(st.Source)
			if detail == "" {
				detail = "unknown"
			}
			if st.Path != "" {
				detail += " · " + st.Path
			}
			cmd.Println(faint.Render("  " + detail))
		} else {
			headline := red.Render("✗") + " " + bold.Render(st.Tool)
			if st.Error != "" {
				headline += red.Render(" (" + st.Error + ")")
			}
			cmd.Println(headline)
		}
		cmd.Println()
	}

	printEncodingStatus(cmd, bold, green, red, faint)
}

func printEncodingStatus(cmd *cobra.Command, bold, green, red, faint lipgloss.Style) {
	profile := tools.LoadEncodingProfile()
	if profile == nil {
		cmd.Println(red.Render("–") + " " + bold.Render("encoding"))
		cmd.Println(faint.Render("  not configured — run `powerhour tools encoding`"))
		cmd.Println()
		return
	}

	global := tools.LoadEncodingDefaults()
	codec := global.VideoCodec
	if codec == "" {
		codec = profile.SelectedCodec
	}
	container := global.Container
	if container == "" {
		container = "mp4"
	}
	bitrate := global.VideoBitrate
	if bitrate == "" {
		bitrate = "8M"
	}

	cmd.Println(green.Render("✓") + " " + bold.Render("encoding"))
	cmd.Println(faint.Render(fmt.Sprintf("  %s · %s · %s", codec, container, bitrate)))
	cmd.Println(faint.Render(fmt.Sprintf("  probed %s", profile.ProbedAt.Format("2006-01-02"))))
	cmd.Println()
}

func ensureStrict(statuses []tools.Status) error {
	var failures []string
	for _, st := range statuses {
		if st.Satisfied {
			continue
		}
		msg := st.Tool
		if st.Error != "" {
			msg = fmt.Sprintf("%s (%s)", st.Tool, st.Error)
		}
		failures = append(failures, msg)
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New("tool check failed: " + strings.Join(failures, ", "))
}
