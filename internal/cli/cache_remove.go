package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
)

var (
	cacheRemoveDryRun   bool
	cacheRemoveKeepFile bool
)

func newCacheRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <identifier>",
		Short: "Remove a cache entry",
		Long: `Remove a cache entry by identifier, video ID, or filename.

Accepts any of:
  - YouTube video ID (e.g. dQw4w9WgXcQ)
  - Full identifier (e.g. youtube:dQw4w9WgXcQ)
  - Filename or path substring matching the cached file`,
		Args: cobra.ExactArgs(1),
		RunE: runCacheRemove,
	}

	cmd.Flags().BoolVar(&cacheRemoveDryRun, "dry-run", false, "Show what would be removed without deleting")
	cmd.Flags().BoolVar(&cacheRemoveKeepFile, "keep-file", false, "Remove index entry but leave cached file on disk")
	return cmd
}

func runCacheRemove(cmd *cobra.Command, args []string) error {
	glogf, closer := logx.StartCommand("cache-remove")
	defer closer.Close()

	query := strings.TrimSpace(args[0])
	glogf("cache remove query=%q dry_run=%v keep_file=%v", query, cacheRemoveDryRun, cacheRemoveKeepFile)

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	matches := findCacheMatches(idx, query)
	if len(matches) == 0 {
		return fmt.Errorf("no cache entry matching %q", query)
	}
	if len(matches) > 1 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Ambiguous — %d entries match %q:\n", len(matches), query)
		for _, m := range matches {
			fmt.Fprintf(cmd.ErrOrStderr(), "  %s  %s\n", truncateStr(m.Identifier, 40), entryTitle(m))
		}
		return fmt.Errorf("use a more specific identifier")
	}

	entry := matches[0]
	out := cmd.OutOrStdout()
	title := entryTitle(entry)

	if cacheRemoveDryRun {
		fmt.Fprintf(out, "Would remove: %s\n", title)
		fmt.Fprintf(out, "  Identifier: %s\n", entry.Identifier)
		if entry.CachedPath != "" {
			fmt.Fprintf(out, "  File:       %s\n", entry.CachedPath)
		}
		return nil
	}

	shouldDeleteFile := !cacheRemoveKeepFile &&
		entry.SourceType == cache.SourceTypeURL &&
		entry.CachedPath != ""

	if shouldDeleteFile {
		if err := os.Remove(entry.CachedPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove cached file: %w", err)
		}
	}

	idx.DeleteEntry(entry.Identifier)
	for link, target := range idx.Links {
		if target == entry.Identifier {
			idx.DeleteLink(link)
		}
	}

	if err := cache.Save(pp, idx); err != nil {
		return fmt.Errorf("save index: %w", err)
	}

	fmt.Fprintf(out, "Removed: %s\n", title)
	if shouldDeleteFile {
		fmt.Fprintf(out, "  Deleted: %s\n", entry.CachedPath)
	}

	return nil
}

func findCacheMatches(idx *cache.Index, query string) []cache.Entry {
	if idx == nil {
		return nil
	}

	// Exact identifier match
	if entry, ok := idx.GetByIdentifier(query); ok {
		return []cache.Entry{entry}
	}

	// Try as "youtube:<query>" identifier
	if entry, ok := idx.GetByIdentifier("youtube:" + query); ok {
		return []cache.Entry{entry}
	}

	// Substring match against identifier, video ID, cached path, title
	query = strings.ToLower(query)
	var matches []cache.Entry
	for _, entry := range idx.Entries {
		if strings.Contains(strings.ToLower(entry.Identifier), query) {
			matches = append(matches, entry)
			continue
		}
		if entry.ID != "" && strings.EqualFold(entry.ID, query) {
			matches = append(matches, entry)
			continue
		}
		if entry.CachedPath != "" && strings.Contains(strings.ToLower(filepath.Base(entry.CachedPath)), query) {
			matches = append(matches, entry)
			continue
		}
		if entry.Title != "" && strings.Contains(strings.ToLower(entry.Title), query) {
			matches = append(matches, entry)
			continue
		}
	}

	return matches
}

func entryTitle(entry cache.Entry) string {
	if entry.Title != "" {
		return entry.Title
	}
	return filepath.Base(entry.CachedPath)
}
