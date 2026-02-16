package cli

import (
	"context"
	"fmt"
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/tui"
)

var (
	fetchCollection string
)

// addCollectionFetchFlags adds collection-specific flags to the fetch command.
func addCollectionFetchFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&fetchCollection, "collection", "", "Fetch only the specified collection (omit to fetch all collections)")
}

// runCollectionFetch handles fetching for collections-based configuration.
func runCollectionFetch(ctx context.Context, cmd *cobra.Command, pp paths.ProjectPaths, cfg config.Config, glog *log.Logger, status *tui.StatusWriter) error {
	glogf := func(format string, v ...any) {
		if glog != nil {
			glog.Printf(format, v...)
		}
	}

	if cfg.Collections == nil || len(cfg.Collections) == 0 {
		return fmt.Errorf("no collections configured")
	}

	if err := pp.EnsureMetaDirs(); err != nil {
		return err
	}

	if err := pp.EnsureCollectionDirs(cfg); err != nil {
		return err
	}

	status.Update("Loading cache index...")
	glogf("loading cache index")
	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	status.Update("Loading collections...")
	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return err
	}

	glogf("loading collections")
	collections, err := resolver.LoadCollections()
	if err != nil {
		return err
	}

	if fetchCollection != "" {
		coll, ok := collections[fetchCollection]
		if !ok {
			return fmt.Errorf("collection %q not found in configuration", fetchCollection)
		}
		collections = map[string]project.Collection{fetchCollection: coll}
	}

	collectionRows := project.FlattenCollections(collections)
	if len(collectionRows) == 0 {
		return fmt.Errorf("no plan rows found in collections")
	}
	glogf("loaded %d rows across collections", len(collectionRows))
	status.Update(fmt.Sprintf("Loaded %d rows, checking tools...", len(collectionRows)))

	if len(fetchIndexArg) > 0 {
		filtered, err := filterCollectionRowsByIndexArgs(collectionRows, fetchIndexArg)
		if err != nil {
			return err
		}
		collectionRows = filtered
	}

	logger, closer, err := logx.New(pp)
	if err != nil {
		return err
	}
	defer closer.Close()

	status.Update("Checking tools (yt-dlp, ffmpeg)...")
	glogf("ensuring tools (yt-dlp, ffmpeg)")
	svc, err := newCacheServiceWithStatus(ctx, pp, logger, nil, status.Update)
	if err != nil {
		return err
	}
	svc.SetLogOutput(cmd.ErrOrStderr())
	glogf("tools ready, starting fetch")

	opts := cache.ResolveOptions{Force: fetchForce, Reprobe: fetchReprobe, NoDownload: fetchNoDownload}

	outWriter := cmd.OutOrStdout()
	mode := tui.DetectMode(outWriter, fetchNoProgress, outputJSON)
	status.Stop() // Hand off to TUI or plain output

	outcomes := make([]fetchRowResult, 0, len(collectionRows))
	counts := fetchCounts{}
	dirty := false

	fetchWork := func(send func(tea.Msg)) {
		for _, collRow := range collectionRows {
			row := collRow.Row
			key := collectionFetchProgressKey(collRow)

			if send != nil {
				send(tui.RowUpdateMsg{
					Key:    key,
					Fields: map[string]string{"STATUS": collectionFetchStartStatus(collRow, fetchForce)},
				})
			}

			result, err := svc.Resolve(ctx, idx, row, opts)
			if err != nil {
				counts.Failed++
				logger.Printf("fetch collection=%s row %03d failed: %v", collRow.CollectionName, row.Index, err)
				fmt.Fprintf(cmd.ErrOrStderr(), "fetch collection=%s row %03d failed: %v\n", collRow.CollectionName, row.Index, err)
				if send != nil {
					send(tui.RowUpdateMsg{
						Key:    key,
						Fields: map[string]string{"STATUS": "error", "ERROR": err.Error()},
					})
				}
				outcomes = append(outcomes, fetchRowResult{
					ClipType: collRow.CollectionName,
					Index:    row.Index,
					Title:    row.Title,
					Status:   "error",
					Link:     row.Link,
					Error:    err.Error(),
				})
				continue
			}

			switch result.Status {
			case cache.ResolveStatusDownloaded:
				counts.Downloaded++
			case cache.ResolveStatusCopied:
				counts.Copied++
			case cache.ResolveStatusMatched:
				counts.Matched++
			case cache.ResolveStatusMissing:
				counts.Missing++
			case cache.ResolveStatusCached:
				counts.Reused++
			}
			if result.Probed {
				counts.Probed++
			}
			if result.Updated {
				dirty = true
			}

			id := result.ID
			if id == "" {
				id = result.Identifier
			}
			if send != nil {
				send(tui.RowUpdateMsg{
					Key: key,
					Fields: map[string]string{
						"STATUS": string(result.Status),
						"ID":     tui.NonEmptyOrDash(id),
					},
				})
			}

			outcomes = append(outcomes, fetchRowResult{
				ClipType:   collRow.CollectionName,
				Index:      row.Index,
				Title:      row.Title,
				Status:     string(result.Status),
				CachedPath: result.Entry.CachedPath,
				Link:       row.Link,
				Identifier: result.Identifier,
				MediaID:    result.ID,
				SizeBytes:  result.Entry.SizeBytes,
				Probed:     result.Probed,
			})
		}
	}

	if mode == tui.ModeTUI {
		glogf("starting TUI (mode=tui)")
		fmt.Fprintf(outWriter, "Project: %s\n", pp.Root)
		model := buildCollectionFetchProgressModel(collectionRows)
		if err := tui.RunWithWork(outWriter, model, fetchWork); err != nil {
			return err
		}
		glogf("TUI finished")
	} else {
		fetchWork(nil)
	}

	if dirty {
		if err := cache.Save(pp, idx); err != nil {
			return err
		}
	}

	if mode == tui.ModeJSON {
		return writeFetchJSON(cmd, pp.Root, outcomes, counts)
	}

	if mode == tui.ModeTUI {
		printFetchSummary(outWriter, counts)
	} else {
		writeFetchTable(cmd, pp.Root, outcomes, counts)
	}
	if counts.Failed > 0 {
		writeFetchFailures(cmd, outcomes)
	}
	return nil
}

func filterCollectionRowsByIndexArgs(rows []project.CollectionPlanRow, args []string) ([]project.CollectionPlanRow, error) {
	indexes, err := parseIndexArgs(args)
	if err != nil {
		return nil, err
	}
	return filterCollectionRowsByIndex(rows, indexes)
}

func filterCollectionRowsByIndex(rows []project.CollectionPlanRow, indexes []int) ([]project.CollectionPlanRow, error) {
	filter := make(map[int]struct{}, len(indexes))
	for _, idx := range indexes {
		if idx <= 0 {
			return nil, fmt.Errorf("index must be greater than zero: %d", idx)
		}
		filter[idx] = struct{}{}
	}
	if len(filter) == 0 {
		return nil, fmt.Errorf("no indexes provided")
	}

	var filtered []project.CollectionPlanRow
	for _, collRow := range rows {
		if _, ok := filter[collRow.Row.Index]; ok {
			filtered = append(filtered, collRow)
			delete(filter, collRow.Row.Index)
		}
	}

	if len(filter) > 0 {
		return nil, fmt.Errorf("indexes not found in collections: %v", indexes)
	}
	return filtered, nil
}

var collectionFetchColumns = []tui.Column{
	{Header: "COLLECTION", Width: 14},
	{Header: "INDEX", Width: 5},
	{Header: "STATUS", Width: 12},
	{Header: "ID", Width: 14},
}

func buildCollectionFetchProgressModel(collectionRows []project.CollectionPlanRow) tui.ProgressModel {
	model := tui.NewProgressModel("fetch", collectionFetchColumns)
	for _, entry := range collectionRows {
		key := collectionFetchProgressKey(entry)
		model.AddRow(key, []string{
			entry.CollectionName,
			fmt.Sprintf("%03d", entry.Row.Index),
			"pending",
			"-",
		})
	}
	return model
}

func collectionFetchStartStatus(entry project.CollectionPlanRow, force bool) string {
	link := strings.TrimSpace(entry.Row.Link)
	if isRemoteLink(link) {
		if force {
			return "downloading"
		}
		return "matching"
	}
	return "copying"
}

func collectionFetchProgressKey(entry project.CollectionPlanRow) string {
	return fmt.Sprintf("%s:%03d", entry.CollectionName, entry.Row.Index)
}
