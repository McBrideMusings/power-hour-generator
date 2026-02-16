package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
)

var (
	validateIndexes []int
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Run project validations",
	}

	cmd.AddCommand(newValidateFilenamesCmd())
	cmd.AddCommand(newValidateSegmentsCmd())
	cmd.AddCommand(newValidateCollectionCmd())
	return cmd
}

func newValidateFilenamesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filenames",
		Short: "Validate cached filenames against the configured template",
		RunE:  runValidateFilenames,
	}

	cmd.Flags().IntSliceVar(&validateIndexes, "index", nil, "Limit validation to specific 1-based row index (repeat flag for multiple)")
	return cmd
}

func runValidateFilenames(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	return fmt.Errorf("validate filenames is not yet supported for collections")
}

func matchTemplateBase(pattern, actual string) bool {
	pattern = strings.TrimSpace(pattern)
	actual = strings.TrimSpace(actual)
	if pattern == "" {
		return actual == ""
	}
	if !strings.Contains(pattern, "%(") {
		return pattern == actual
	}

	segments, placeholderCount := splitTemplatePattern(pattern)
	if len(segments) == 0 {
		return actual != ""
	}
	if segments[0] != "" && !strings.HasPrefix(actual, segments[0]) {
		return false
	}

	pos := len(segments[0])
	if len(segments) > 2 {
		for _, segment := range segments[1 : len(segments)-1] {
			if segment == "" {
				continue
			}
			idx := strings.Index(actual[pos:], segment)
			if idx == -1 {
				return false
			}
			pos += idx + len(segment)
		}
	}

	last := segments[len(segments)-1]
	if last != "" && !strings.HasSuffix(actual, last) {
		return false
	}

	if placeholderCount > 0 {
		staticLen := 0
		for _, seg := range segments {
			staticLen += len(seg)
		}
		if len(actual) <= staticLen {
			return false
		}
		if len(actual)-staticLen < placeholderCount {
			return false
		}
	}
	return true
}

func splitTemplatePattern(pattern string) ([]string, int) {
	segments := make([]string, 0)
	placeholderCount := 0
	cursor := 0
	for cursor < len(pattern) {
		start := strings.Index(pattern[cursor:], "%(")
		if start == -1 {
			segments = append(segments, pattern[cursor:])
			return segments, placeholderCount
		}
		start += cursor
		segments = append(segments, pattern[cursor:start])
		end := strings.Index(pattern[start:], ")s")
		if end == -1 {
			segments[len(segments)-1] += pattern[start:]
			return segments, placeholderCount
		}
		placeholderCount++
		cursor = start + end + 2
		if cursor == len(pattern) {
			segments = append(segments, "")
		}
	}
	if len(segments) == 0 {
		segments = append(segments, "")
	}
	return segments, placeholderCount
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return strings.EqualFold(a, b)
}
