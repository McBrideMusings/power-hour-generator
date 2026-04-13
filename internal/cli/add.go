package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

func newAddCmd() *cobra.Command {
	var (
		name     string
		filePath string
	)

	cmd := &cobra.Command{
		Use:   "add [text]",
		Short: "Add a row or import rows into a collection",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			glogf, gcloser := logx.StartCommand("add")
			defer gcloser.Close()
			glogf("add started: collection=%s file=%s args=%d", name, filePath, len(args))

			pp, err := paths.Resolve(projectDir)
			if err != nil {
				return err
			}

			cfg, err := config.Load(pp.ConfigFile)
			if err != nil {
				return err
			}
			pp = paths.ApplyConfig(pp, cfg)

			resolver, err := project.NewCollectionResolver(cfg, pp)
			if err != nil {
				return err
			}
			collections, err := resolver.LoadCollections()
			if err != nil {
				return err
			}

			coll, ok := collections[name]
			if !ok {
				return fmt.Errorf("collection %q not found", name)
			}

			raw, err := readAddInput(args, cmd.InOrStdin(), filePath)
			if err != nil {
				return err
			}

			rows, format, err := buildRowsForAdd(raw, coll)
			if err != nil {
				if outputJSON {
					return writeAddErrorJSON(cmd, name, string(format), err)
				}
				return fmt.Errorf("add %s: %w", name, err)
			}

			coll = project.AppendCollectionRows(coll, rows)
			if err := project.WriteCollectionPlan(coll); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Collection    string `json:"collection"`
					AddedRows     int    `json:"added_rows"`
					SourceFormat  string `json:"source_format"`
					StorageFormat string `json:"storage_format"`
					Plan          string `json:"plan"`
				}{
					Collection:    name,
					AddedRows:     len(rows),
					SourceFormat:  string(format),
					StorageFormat: coll.PlanFormat,
					Plan:          coll.Plan,
				})
			}

			if len(rows) == 1 && format == "single" {
				fmt.Fprintf(cmd.OutOrStdout(), "Added 1 row to %s\n", name)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %d rows to %s (%s -> %s)\n", len(rows), name, format, coll.PlanFormat)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "collection", "", "Collection name to add into (required)")
	cmd.Flags().StringVar(&filePath, "file", "", "Path to YAML/CSV/TSV text to add (default: use [text] or read stdin)")
	cmd.MarkFlagRequired("collection")
	return cmd
}

func readAddInput(args []string, in io.Reader, filePath string) (string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return args[0], nil
	}
	if strings.TrimSpace(filePath) != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read add file: %w", err)
		}
		return string(data), nil
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return "", fmt.Errorf("no add input provided")
	}
	return string(data), nil
}

func buildRowsForAdd(raw string, coll project.Collection) ([]csvplan.CollectionRow, csvplan.ImportFormat, error) {
	if looksLikeBatchImport(raw) {
		return csvplan.ImportCollectionText(raw, project.CollectionOptionsForConfig(coll))
	}

	value := cleanYouTubeURL(raw)
	opts := project.CollectionOptionsForConfig(coll)
	defaultDur := opts.DefaultDuration
	if defaultDur <= 0 {
		defaultDur = 60
	}

	linkHeader := opts.LinkHeader
	if linkHeader == "" {
		linkHeader = "link"
	}
	startHeader := opts.StartHeader
	if startHeader == "" {
		startHeader = "start_time"
	}
	durationHeader := opts.DurationHeader
	if durationHeader == "" {
		durationHeader = "duration"
	}

	row := csvplan.CollectionRow{
		Link:            strings.TrimSpace(value),
		StartRaw:        "0:00",
		DurationSeconds: defaultDur,
		CustomFields: map[string]string{
			linkHeader:     strings.TrimSpace(value),
			startHeader:    "0:00",
			durationHeader: fmt.Sprintf("%d", defaultDur),
		},
	}
	return []csvplan.CollectionRow{row}, csvplan.ImportFormat("single"), nil
}

func looksLikeBatchImport(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}

	lines := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	if len(lines) > 1 {
		return true
	}
	if strings.HasPrefix(trimmed, "- ") {
		return true
	}
	firstLine := trimmed
	if len(lines) == 1 {
		firstLine = strings.TrimSpace(lines[0])
	}
	return strings.Contains(firstLine, ",") || strings.Contains(firstLine, "\t")
}

func cleanYouTubeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "https://youtu.be/") || strings.HasPrefix(raw, "http://youtu.be/") {
		if idx := strings.Index(raw, "?"); idx >= 0 {
			return raw[:idx]
		}
		return raw
	}
	if !strings.Contains(raw, "youtube.com/watch") {
		return raw
	}
	qIdx := strings.Index(raw, "?")
	if qIdx < 0 {
		return raw
	}
	base := raw[:qIdx]
	query := raw[qIdx+1:]
	videoID := ""
	for _, param := range strings.Split(query, "&") {
		if strings.HasPrefix(param, "v=") {
			videoID = param[2:]
			break
		}
	}
	if videoID == "" {
		return raw
	}
	return base + "?v=" + videoID
}

func writeAddErrorJSON(cmd *cobra.Command, collectionName, format string, err error) error {
	payload := struct {
		Collection   string                    `json:"collection"`
		SourceFormat string                    `json:"source_format,omitempty"`
		Error        string                    `json:"error"`
		Issues       []csvplan.ValidationError `json:"issues,omitempty"`
	}{
		Collection:   collectionName,
		SourceFormat: format,
		Error:        err.Error(),
	}
	if errs, ok := err.(csvplan.ValidationErrors); ok {
		payload.Issues = errs.Issues()
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(payload)
}
