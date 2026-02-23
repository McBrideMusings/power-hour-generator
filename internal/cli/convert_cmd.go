package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"powerhour/pkg/csvplan"
)

func newConvertCmd() *cobra.Command {
	var (
		outputPath     string
		linkHeader     string
		startHeader    string
		durationHeader string
		dryRun         bool
	)

	cmd := &cobra.Command{
		Use:   "convert <input.csv>",
		Short: "Convert a CSV/TSV plan file to YAML format",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := args[0]

			opts := csvplan.ImportOptions{
				LinkHeader:     linkHeader,
				StartHeader:    startHeader,
				DurationHeader: durationHeader,
			}

			rows, err := csvplan.ImportFromCSV(input, opts)
			if err != nil {
				// On validation errors with partial data, still continue.
				if _, ok := err.(csvplan.ValidationErrors); !ok {
					return fmt.Errorf("convert %s: %w", input, err)
				}
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}

			if dryRun {
				printConvertDryRun(cmd, rows)
				return nil
			}

			out := outputPath
			if out == "" {
				base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
				out = filepath.Join(filepath.Dir(input), base+".yaml")
			}

			if err := writeConvertYAML(out, rows); err != nil {
				return err
			}

			cmd.Printf("Converted %d rows → %s\n", len(rows), out)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output YAML path (default: <input-basename>.yaml)")
	cmd.Flags().StringVar(&linkHeader, "link", "", "Column name for the URL field (default: auto-detect)")
	cmd.Flags().StringVar(&startHeader, "start", "", "Column name for the start time field (default: auto-detect)")
	cmd.Flags().StringVar(&durationHeader, "duration", "", "Column name for the duration field (default: auto-detect)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print detected column mapping and sample rows without writing")

	return cmd
}

// printConvertDryRun prints a preview of detected columns and sample rows.
func printConvertDryRun(cmd *cobra.Command, rows []csvplan.CollectionRow) {
	if len(rows) == 0 {
		cmd.Println("No rows detected.")
		return
	}

	// Print detected field names from first row's custom fields.
	first := rows[0]
	cmd.Println("Detected columns:")
	cmd.Printf("  link      = %s\n", truncate(first.Link, 60))
	cmd.Printf("  start_time = %s\n", first.StartRaw)
	cmd.Printf("  duration  = %d\n", first.DurationSeconds)

	// Show other fields.
	for k, v := range first.CustomFields {
		if k == "link" || k == "start_time" || k == "duration" {
			continue
		}
		cmd.Printf("  %-12s= %s\n", k, truncate(v, 60))
	}

	limit := len(rows)
	if limit > 3 {
		limit = 3
	}
	cmd.Printf("\nSample rows (%d of %d):\n", limit, len(rows))
	for _, row := range rows[:limit] {
		cmd.Printf("  [%d] link=%s start=%s duration=%d\n",
			row.Index, truncate(row.Link, 50), row.StartRaw, row.DurationSeconds)
	}
}

// writeConvertYAML marshals rows as a YAML list to path.
func writeConvertYAML(path string, rows []csvplan.CollectionRow) error {
	items := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		m := make(map[string]string, len(row.CustomFields))
		for k, v := range row.CustomFields {
			m[k] = v
		}
		// Ensure canonical fields are present.
		if row.Link != "" {
			m["link"] = row.Link
		}
		if row.StartRaw != "" {
			m["start_time"] = row.StartRaw
		}
		items = append(items, m)
	}

	data, err := yaml.Marshal(items)
	if err != nil {
		return fmt.Errorf("marshal YAML: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
