package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show resolved timeline for the project",
		RunE:  runStatus,
	}
	return cmd
}

// timelineEntryOutput is the JSON-serializable form of a resolved timeline entry.
type timelineEntryOutput struct {
	Sequence    int    `json:"sequence"`
	Collection  string `json:"collection"`
	Index       int    `json:"index"`
	SegmentPath string `json:"segment_path"`
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

	var timelineEntries []timelineEntryOutput
	hasTimeline := len(cfg.Timeline.Sequence) > 0

	if hasTimeline {
		resolved, err := project.ResolveTimeline(cfg.Timeline, collections)
		if err != nil {
			return fmt.Errorf("resolve timeline: %w", err)
		}
		timelineEntries = make([]timelineEntryOutput, len(resolved))
		for i, e := range resolved {
			timelineEntries[i] = timelineEntryOutput{
				Sequence:    e.Sequence,
				Collection:  e.Collection,
				Index:       e.Index,
				SegmentPath: e.SegmentPath,
			}
		}
	}

	payload := struct {
		Project     string                `json:"project"`
		HasTimeline bool                  `json:"has_timeline"`
		Timeline    []timelineEntryOutput `json:"timeline,omitempty"`
	}{
		Project:     pp.Root,
		HasTimeline: hasTimeline,
		Timeline:    timelineEntries,
	}

	var data []byte
	if outputJSON {
		data, err = json.MarshalIndent(payload, "", "  ")
	} else {
		data, err = json.Marshal(payload)
	}
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}

	fmt.Fprintln(os.Stdout, string(data))
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
