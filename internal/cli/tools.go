package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/tools"
	"powerhour/internal/tui"
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
	cmd.AddCommand(newToolsEncodingCmd())

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
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}
	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	ctx := tools.WithMinimums(cmd.Context(), cfg.ToolMinimums())
	statuses, err := tools.Detect(ctx)
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

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}
	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	baseCtx := tools.WithMinimums(cmd.Context(), cfg.ToolMinimums())
	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Minute)
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

func newToolsEncodingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "encoding",
		Short: "Configure encoding defaults",
		RunE:  runToolsEncoding,
	}
}

func runToolsEncoding(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}
	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}

	ffmpegPath, ffmpegErr := tools.Lookup("ffmpeg")
	if ffmpegErr != nil {
		cmd.Println("ffmpeg not found; run `powerhour check` or `powerhour tools install` first.")
		return nil
	}

	global := tools.LoadEncodingDefaults()

	isTTY := xterm.IsTerminal(os.Stdout.Fd())
	if isTTY {
		// Probing happens inside the TUI; terminal is grayed out until ready.
		result, err := tui.RunEncodingSetup(cmd.OutOrStdout(), ffmpegPath, global)
		if err != nil {
			return fmt.Errorf("encoding setup: %w", err)
		}
		if result.Cancelled {
			return nil
		}
		global.VideoCodec = result.VideoCodec
		global.Width = result.Width
		global.Height = result.Height
		global.FPS = result.FPS
		global.CRF = result.CRF
		global.Preset = result.Preset
		global.VideoBitrate = result.VideoBitrate
		global.Container = result.Container
		global.AudioCodec = result.AudioCodec
		global.AudioBitrate = result.AudioBitrate
		global.SampleRate = result.SampleRate
		global.Channels = result.Channels
		loudnorm := result.LoudnormEnabled
		global.LoudnormEnabled = &loudnorm
		if err := tools.SaveEncodingDefaults(global); err != nil {
			return fmt.Errorf("save encoding defaults: %w", err)
		}
	} else {
		// Non-TTY: probe directly and auto-save best defaults.
		ctx := tools.WithMinimums(cmd.Context(), cfg.ToolMinimums())
		p, err := tools.ProbeEncoders(ctx, ffmpegPath)
		if err != nil {
			return fmt.Errorf("probe encoders: %w", err)
		}
		_ = tools.SaveEncodingProfile(p)
		if global.VideoCodec == "" {
			global.VideoCodec = p.SelectedCodec
		}
		if global.VideoBitrate == "" {
			global.VideoBitrate = "8M"
		}
		if global.Container == "" {
			global.Container = "mp4"
		}
		if global.AudioCodec == "" {
			global.AudioCodec = "aac"
		}
		if global.AudioBitrate == "" {
			global.AudioBitrate = "192k"
		}
		_ = tools.SaveEncodingDefaults(global)
	}

	// If the project has encoding overrides in its YAML, note that they take precedence.
	enc := cfg.Encoding
	if enc.VideoCodec != "" || enc.Container != "" || enc.VideoBitrate != "" ||
		enc.AudioCodec != "" || enc.AudioBitrate != "" || enc.Preset != "" ||
		enc.Width > 0 || enc.Height > 0 || enc.FPS > 0 || enc.CRF > 0 ||
		enc.SampleRate > 0 || enc.Channels > 0 || enc.LoudnormEnabled != nil {
		cmd.Println()
		cmd.Println("Note: this project has encoding overrides in powerhour.yaml that take")
		cmd.Println("precedence over global defaults. Edit that file to change them.")
	}

	return nil
}

func printEncodingField(cmd *cobra.Command, name, value string) {
	if value == "" {
		value = "(not set)"
	}
	cmd.Printf("  %-14s %s\n", name+":", value)
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
