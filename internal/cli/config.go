package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
)

var (
	yamlKeyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))  // cyan-blue
	yamlStringStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("114")) // green
	yamlNumberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("215")) // orange
	yamlBoolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("183")) // lavender
	yamlCommentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim gray
	yamlListStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim gray
	filePathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
)

var (
	reKeyOnly    = regexp.MustCompile(`^(\s*)([\w\-]+)(\s*:\s*)$`)
	reKeyValue   = regexp.MustCompile(`^(\s*)([\w\-]+)(\s*:\s*)(.+)$`)
	reListItem   = regexp.MustCompile(`^(\s*)(-)(\s+)(.*)$`)
	reNumber     = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	reBool       = regexp.MustCompile(`^(true|false|yes|no|on|off)$`)
)

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print the effective configuration",
		RunE:  runConfigShow,
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

	out := cmd.OutOrStdout()
	fmt.Fprintln(out)
	fmt.Fprintln(out, filePathStyle.Render(pp.ConfigFile))
	fmt.Fprintln(out)
	fmt.Fprintln(out, highlightYAML(strings.TrimRight(string(data), "\n")))
	fmt.Fprintln(out)
	return nil
}

func highlightYAML(yaml string) string {
	lines := strings.Split(yaml, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = highlightYAMLLine(line)
	}
	return strings.Join(out, "\n")
}

func highlightYAMLLine(line string) string {
	// Comment lines
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return yamlCommentStyle.Render(line)
	}

	// List items: "  - value" or "  - key: value"
	if m := reListItem.FindStringSubmatch(line); m != nil {
		indent, dash, space, rest := m[1], m[2], m[3], m[4]
		// If rest looks like a key:value, highlight accordingly
		if km := reKeyValue.FindStringSubmatch(rest); km != nil {
			return indent + yamlListStyle.Render(dash+space) + yamlKeyStyle.Render(km[2]) + km[3] + colorizeValue(km[4])
		}
		if reKeyOnly.MatchString(rest) {
			return indent + yamlListStyle.Render(dash+space) + yamlKeyStyle.Render(strings.TrimRight(rest, ": ")) + ":"
		}
		return indent + yamlListStyle.Render(dash+space) + colorizeValue(rest)
	}

	// key: value
	if m := reKeyValue.FindStringSubmatch(line); m != nil {
		return m[1] + yamlKeyStyle.Render(m[2]) + m[3] + colorizeValue(m[4])
	}

	// key: (no value, mapping header)
	if m := reKeyOnly.FindStringSubmatch(line); m != nil {
		return m[1] + yamlKeyStyle.Render(m[2]) + m[3]
	}

	return line
}

func colorizeValue(v string) string {
	// Inline comment: split on " #"
	commentIdx := strings.Index(v, " #")
	val, comment := v, ""
	if commentIdx >= 0 {
		val = v[:commentIdx]
		comment = yamlCommentStyle.Render(v[commentIdx:])
	}

	val = strings.TrimSpace(val)
	switch {
	case reBool.MatchString(val):
		return yamlBoolStyle.Render(val) + comment
	case reNumber.MatchString(val):
		return yamlNumberStyle.Render(val) + comment
	default:
		return yamlStringStyle.Render(val) + comment
	}
}
