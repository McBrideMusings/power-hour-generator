package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/render"
	"powerhour/pkg/csvplan"
)

var (
	renderConcurrency int
	renderForce       bool
)

func newRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render cached clips into individual segment files",
		RunE:  runRender,
	}

	defaultConcurrency := runtime.NumCPU()
	if defaultConcurrency < 1 {
		defaultConcurrency = 1
	}

	cmd.Flags().IntVar(&renderConcurrency, "concurrency", defaultConcurrency, "Concurrent ffmpeg processes")
	cmd.Flags().BoolVar(&renderForce, "force", false, "Re-render even if segment output already exists")

	return cmd
}

func runRender(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

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

	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	planOpts := csvplan.Options{
		HeaderAliases:   cfg.HeaderAliases(),
		DefaultDuration: cfg.PlanDefaultDuration(),
	}
	rows, err := csvplan.LoadWithOptions(pp.CSVFile, planOpts)
	if err != nil {
		return err
	}

	segments := make([]render.Segment, 0, len(rows))
	for _, row := range rows {
		entry, ok := idx.Get(row.Index)
		if !ok || entry.CachedPath == "" {
			return fmt.Errorf("row %03d %q has no cached source; run `powerhour fetch` first", row.Index, row.Title)
		}
		exists, err := paths.FileExists(entry.CachedPath)
		if err != nil {
			return fmt.Errorf("stat cached source for row %03d: %w", row.Index, err)
		}
		if !exists {
			return fmt.Errorf("cached source not found for row %03d %q (expected at %s)", row.Index, row.Title, entry.CachedPath)
		}
		segments = append(segments, render.Segment{
			Row:        row,
			CachedPath: entry.CachedPath,
			Entry:      entry,
		})
	}

	svc, err := render.NewService(ctx, pp, cfg, nil)
	if err != nil {
		return err
	}
	svc.SetWriters(cmd.OutOrStdout(), nil)

	results := svc.Render(ctx, segments, render.Options{
		Concurrency: renderConcurrency,
		Force:       renderForce,
	})

	if outputJSON {
		return writeRenderJSON(cmd, pp.Root, results)
	}

	return writeRenderOutput(cmd, results)
}

func writeRenderOutput(cmd *cobra.Command, results []render.Result) error {
	var (
		failures int
		success  int
	)

	for _, res := range results {
		if res.Err != nil {
			failures++
			fmt.Fprintf(cmd.ErrOrStderr(), "render %03d %q failed: %v\n", res.Index, res.Title, res.Err)
			continue
		}
		success++
		fmt.Fprintf(cmd.OutOrStdout(), "rendered %03d → %s\n", res.Index, res.OutputPath)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "completed renders: %d success, %d failed\n", success, failures)

	if failures > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "%d render(s) failed; see logs for details\n", failures)
	}
	return nil
}

func writeRenderJSON(cmd *cobra.Command, project string, results []render.Result) error {
	payload := struct {
		Project string             `json:"project"`
		Results []renderJSONResult `json:"results"`
		Summary renderJSONSummary  `json:"summary"`
	}{
		Project: project,
		Results: make([]renderJSONResult, 0, len(results)),
	}

	for _, res := range results {
		payload.Results = append(payload.Results, renderJSONResult{
			Index:      res.Index,
			Title:      res.Title,
			OutputPath: res.OutputPath,
			LogPath:    res.LogPath,
			Error:      errorString(res.Err),
		})
		if res.Err != nil {
			payload.Summary.Failed++
		} else {
			payload.Summary.Succeeded++
		}
	}

	sort.Slice(payload.Results, func(i, j int) bool {
		return payload.Results[i].Index < payload.Results[j].Index
	})

	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode render json: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(out))
	return nil
}

type renderJSONResult struct {
	Index      int    `json:"index"`
	Title      string `json:"title"`
	OutputPath string `json:"output_path"`
	LogPath    string `json:"log_path"`
	Error      string `json:"error,omitempty"`
}

type renderJSONSummary struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
