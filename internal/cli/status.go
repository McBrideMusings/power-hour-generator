package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show parsed song entries from the project plan",
		RunE:  runStatus,
	}
	return cmd
}

func runStatus(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)

	if err := ensureProjectDirs(pp); err != nil {
		return err
	}

	resolver, err := project.NewResolver(cfg, pp)
	if err != nil {
		return err
	}

	plans, err := resolver.LoadPlans()
	if err != nil {
		return err
	}

	planRows := project.FlattenPlans(plans)
	planErrors := resolver.PlanErrors()

	if outputJSON {
		if err := writeStatusJSON(cmd, pp.Root, planRows, planErrors); err != nil {
			return err
		}
	} else {
		writeStatusTable(cmd, pp.Root, planRows, planErrors)
	}

	if len(planErrors) > 0 {
		var agg csvplan.ValidationErrors
		for _, errs := range planErrors {
			agg = append(agg, errs...)
		}
		return agg
	}

	return nil
}

func ensureProjectDirs(pp paths.ProjectPaths) error {
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

	return nil
}

func writeStatusTable(cmd *cobra.Command, projectName string, rows []project.PlanRow, errs map[project.ClipType]csvplan.ValidationErrors) {
	fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\n", projectName)

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tINDEX\tTITLE\tARTIST\tSTART\tDURATION\tNAME\tLINK")
	for _, entry := range rows {
		row := entry.Row
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%d\t%s\t%s\n",
			entry.ClipType,
			row.Index,
			row.Title,
			row.Artist,
			printableStart(row),
			row.DurationSeconds,
			row.Name,
			row.Link,
		)
	}
	w.Flush()

	if len(errs) > 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "Validation issues:")
		for clipType, issues := range errs {
			for _, issue := range issues {
				fmt.Fprintf(cmd.ErrOrStderr(), "  - %s: %s\n", clipType, issue.Error())
			}
		}
	}
}

func writeStatusJSON(cmd *cobra.Command, projectName string, rows []project.PlanRow, errs map[project.ClipType]csvplan.ValidationErrors) error {
	payload := struct {
		Project string                        `json:"project"`
		Rows    []statusJSONRow               `json:"rows"`
		Errors  map[project.ClipType][]string `json:"errors,omitempty"`
	}{
		Project: projectName,
		Rows:    make([]statusJSONRow, 0, len(rows)),
	}

	for _, entry := range rows {
		row := entry.Row
		payload.Rows = append(payload.Rows, statusJSONRow{
			ClipType:        string(entry.ClipType),
			Index:           row.Index,
			Title:           row.Title,
			Artist:          row.Artist,
			Start:           formatDuration(row.Start),
			StartRaw:        row.StartRaw,
			DurationSeconds: row.DurationSeconds,
			Name:            row.Name,
			Link:            row.Link,
		})
	}

	if len(errs) > 0 {
		payload.Errors = make(map[project.ClipType][]string, len(errs))
		for clipType, issues := range errs {
			for _, issue := range issues {
				payload.Errors[clipType] = append(payload.Errors[clipType], issue.Error())
			}
		}
	}

	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode status json: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(out))
	return nil
}

func printableStart(row csvplan.Row) string {
	if strings.TrimSpace(row.StartRaw) != "" {
		return row.StartRaw
	}
	return formatDuration(row.Start)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}

	totalSeconds := int64(d / time.Second)
	nanos := int64(d % time.Second)

	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	var base string
	if hours > 0 {
		base = fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	} else {
		base = fmt.Sprintf("%d:%02d", minutes, seconds)
	}

	if nanos > 0 {
		frac := fmt.Sprintf(".%09d", nanos)
		frac = strings.TrimRight(frac, "0")
		base += frac
	}

	return base
}

type statusJSONRow struct {
	ClipType        string `json:"clip_type"`
	Index           int    `json:"index"`
	Title           string `json:"title"`
	Artist          string `json:"artist"`
	Start           string `json:"start"`
	StartRaw        string `json:"start_raw"`
	DurationSeconds int    `json:"duration"`
	Name            string `json:"name"`
	Link            string `json:"link"`
}
