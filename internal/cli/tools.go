package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
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
	glogf, gcloser := logx.StartCommand("tools-list")
	defer gcloser.Close()
	glogf("tools list started")

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
	glogf, gcloser := logx.StartCommand("tools-install")
	defer gcloser.Close()

	target := "all"
	if len(args) == 1 {
		target = strings.ToLower(args[0])
	}

	glogf("tools install started: target=%s version=%s force=%v", target, installVersion, installForce)

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
	glogf, gcloser := logx.StartCommand("tools-encoding")
	defer gcloser.Close()
	glogf("tools encoding started")

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

	bold := lipgloss.NewStyle().Bold(true).Inline(true)
	faint := lipgloss.NewStyle().Faint(true).Inline(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Inline(true)
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Inline(true)
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Inline(true)
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Inline(true)

	rows := make([]tools.Status, len(statuses))
	copy(rows, statuses)
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Tool < rows[j].Tool
	})

	col := func(s lipgloss.Style, width int) func(string) string {
		return func(text string) string {
			return s.Width(width).Render(text)
		}
	}

	hTool := col(bold, 10)
	hVer := col(bold, 14)
	hMethod := col(bold, 14)
	hStatus := col(bold, 10)

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "  %s %s %s %s %s\n",
		hTool("TOOL"),
		hVer("VERSION"),
		hMethod("INSTALLED VIA"),
		hStatus("STATUS"),
		bold.Render("PATH"),
	)

	cTool := col(lipgloss.NewStyle().Inline(true), 10)
	cVer := col(lipgloss.NewStyle().Inline(true), 14)
	cMethod := col(lipgloss.NewStyle().Inline(true), 14)

	var updatable []tools.Status
	for _, st := range rows {
		statusLabel := red.Width(10).Render("missing")
		if st.Satisfied {
			statusLabel = green.Width(10).Render("ok")
		} else if st.Version != "" {
			statusLabel = yellow.Width(10).Render("outdated")
		}

		path := st.Path
		if path == "" {
			path = faint.Render("(not found)")
		}

		method := tools.InstallMethodLabel(st.InstallMethod)
		if method == "" {
			method = "-"
		}

		fmt.Fprintf(out, "  %s %s %s %s %s\n",
			cTool(st.Tool),
			cVer(st.Version),
			cMethod(method),
			statusLabel,
			path,
		)

		if st.Error != "" {
			fmt.Fprintf(out, "    %s\n", red.Render(st.Error))
		}

		if hint := tools.FormatUpdateHint(st.Tool, st.Version); hint != "" {
			fmt.Fprintf(out, "    %s\n", cyan.Render(hint))
			updatable = append(updatable, st)
		}
	}

	if len(updatable) > 0 && xterm.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(out)
		promptToolUpdates(cmd, updatable)
	}
}

func promptToolUpdates(cmd *cobra.Command, updatable []tools.Status) {
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Inline(true)
	faint := lipgloss.NewStyle().Faint(true).Inline(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Inline(true)

	names := make([]string, len(updatable))
	for i, st := range updatable {
		names[i] = st.Tool
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n",
		cyan.Render("Updates available for: "+strings.Join(names, ", ")+"."),
		faint.Render("Install now? [y/N]"),
	)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "y" && answer != "yes" {
		return
	}

	out := cmd.OutOrStdout()

	// Separate tools by update strategy.
	var managed []tools.Status
	var external []tools.Status
	for _, st := range updatable {
		switch st.InstallMethod {
		case tools.InstallMethodManaged, "":
			managed = append(managed, st)
		default:
			external = append(external, st)
		}
	}

	// For externally-managed tools (homebrew, apt, etc.), run the appropriate
	// package manager command directly.
	for _, st := range external {
		notice := tools.UpdateNotice{Tool: st.Tool, InstallMethod: st.InstallMethod}
		updateCmd := notice.UpdateCommand()
		fmt.Fprintf(out, "Updating %s via %s...\n", st.Tool, tools.InstallMethodLabel(st.InstallMethod))
		parts := strings.Fields(updateCmd)
		c := exec.CommandContext(cmd.Context(), parts[0], parts[1:]...)
		c.Stdout = out
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		} else {
			fmt.Fprintf(out, "  %s\n", green.Render(st.Tool+" updated."))
			tools.ClearUpdateNotice(st.Tool)
		}
	}

	// For powerhour-managed tools, use the install system with the target version.
	if len(managed) > 0 {
		pp, err := paths.Resolve(projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		cfg, err := config.Load(pp.ConfigFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		baseCtx := tools.WithMinimums(cmd.Context(), cfg.ToolMinimums())
		ctx, cancel := context.WithTimeout(baseCtx, 10*time.Minute)
		defer cancel()

		for _, st := range managed {
			targetVersion := tools.FormatUpdateTarget(st.Tool)
			fmt.Fprintf(out, "Updating %s to %s...\n", st.Tool, targetVersion)
			_, err := tools.Install(ctx, st.Tool, targetVersion, tools.InstallOptions{Force: true, Version: targetVersion})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
			} else {
				fmt.Fprintf(out, "  %s\n", green.Render(st.Tool+" updated."))
				tools.ClearUpdateNotice(st.Tool)
			}
		}
	}
}
