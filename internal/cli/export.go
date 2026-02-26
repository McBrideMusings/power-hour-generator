package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
)

var exportTimeline bool

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export project data as JSON",
		RunE:  runExport,
	}

	cmd.Flags().BoolVar(&exportTimeline, "timeline", false, "Include resolved timeline in output")

	return cmd
}

type exportOutput struct {
	Project     string                       `json:"project"`
	Config      exportConfig                 `json:"config"`
	Collections map[string]exportCollection  `json:"collections"`
	Timeline    []exportTimelineEntry        `json:"timeline,omitempty"`
}

type exportConfig struct {
	Video    config.VideoConfig    `json:"video"`
	Audio    config.AudioConfig    `json:"audio"`
	Encoding config.EncodingConfig `json:"encoding"`
}

type exportCollection struct {
	Rows []exportRow `json:"rows"`
}

type exportRow struct {
	Index        int               `json:"index"`
	Title        string            `json:"title,omitempty"`
	Artist       string            `json:"artist,omitempty"`
	Link         string            `json:"link,omitempty"`
	Start        string            `json:"start,omitempty"`
	Duration     int               `json:"duration,omitempty"`
	CustomFields map[string]string `json:"custom_fields,omitempty"`
}

type exportTimelineEntry struct {
	Sequence   int    `json:"sequence"`
	Collection string `json:"collection"`
	Index      int    `json:"index"`
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
}

func runExport(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return err
	}

	collections, err := resolver.LoadCollections()
	if err != nil {
		return err
	}

	output := exportOutput{
		Project: pp.Root,
		Config: exportConfig{
			Video:    cfg.Video,
			Audio:    cfg.Audio,
			Encoding: cfg.Encoding,
		},
		Collections: make(map[string]exportCollection, len(collections)),
	}

	for name, coll := range collections {
		rows := make([]exportRow, len(coll.Rows))
		for i, collRow := range coll.Rows {
			r := collRow.ToRow()
			rows[i] = exportRow{
				Index:        r.Index,
				Title:        r.Title,
				Artist:       r.Artist,
				Link:         r.Link,
				Start:        r.StartRaw,
				Duration:     r.DurationSeconds,
				CustomFields: r.CustomFields,
			}
		}
		output.Collections[name] = exportCollection{Rows: rows}
	}

	if exportTimeline && len(cfg.Timeline.Sequence) > 0 {
		entries, err := project.ResolveTimeline(cfg.Timeline, collections)
		if err != nil {
			return fmt.Errorf("resolve timeline: %w", err)
		}

		output.Timeline = make([]exportTimelineEntry, len(entries))
		for i, e := range entries {
			te := exportTimelineEntry{
				Sequence:   e.Sequence,
				Collection: e.Collection,
				Index:      e.Index,
			}
			// Populate title/artist from collection rows
			if coll, ok := collections[e.Collection]; ok && e.Index >= 1 && e.Index <= len(coll.Rows) {
				row := coll.Rows[e.Index-1]
				te.Title = row.CustomFields["title"]
				te.Artist = row.CustomFields["artist"]
			}
			output.Timeline[i] = te
		}
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
