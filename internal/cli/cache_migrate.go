package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
)

var migrateDryRun bool

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Move project cache files into the global cache",
		RunE:  runCacheMigrate,
	}

	cmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Print actions without moving files")
	return cmd
}

type migrateStats struct {
	Moved          int
	AlreadyGlobal  int
	Skipped        int
	Recovered      int
	Orphans        int
	GlobalWins     int
	LinksMerged    int
	EntriesCleaned int
}

func runCacheMigrate(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)

	// We need the project-local paths (not global-applied)
	localCacheDir := pp.CacheDir
	localIndexFile := pp.IndexFile

	globalCacheDir := pp.GlobalCacheDir
	globalIndexFile := pp.GlobalIndexFile

	if globalCacheDir == "" || globalIndexFile == "" {
		return fmt.Errorf("could not determine global cache paths (home directory unavailable)")
	}

	out := cmd.OutOrStdout()

	// Load local index
	localIdx, err := cache.LoadFromPath(localIndexFile)
	if err != nil {
		return fmt.Errorf("load local index: %w", err)
	}

	if len(localIdx.Entries) == 0 {
		fmt.Fprintln(out, "No entries in local cache index, nothing to migrate.")
		return nil
	}

	// Load global index
	globalIdx, err := cache.LoadFromPath(globalIndexFile)
	if err != nil {
		return fmt.Errorf("load global index: %w", err)
	}

	// Ensure global cache dir exists
	if !migrateDryRun {
		if err := os.MkdirAll(globalCacheDir, 0o755); err != nil {
			return fmt.Errorf("create global cache dir: %w", err)
		}
	}

	stats := migrateStats{}

	for id, entry := range localIdx.Entries {
		cachedPath := strings.TrimSpace(entry.CachedPath)
		if cachedPath == "" {
			continue
		}

		// Skip if already in global cache dir
		if strings.HasPrefix(cachedPath, globalCacheDir+string(filepath.Separator)) || cachedPath == globalCacheDir {
			stats.AlreadyGlobal++
			continue
		}

		// Check if file exists at indexed path
		info, err := os.Stat(cachedPath)
		if err != nil {
			// Stale path recovery: check local cache dir for same basename
			base := filepath.Base(cachedPath)
			recovered := filepath.Join(localCacheDir, base)
			if rInfo, rErr := os.Stat(recovered); rErr == nil && rInfo.Mode().IsRegular() {
				fmt.Fprintf(out, "recovered %s: %s -> %s\n", id, cachedPath, recovered)
				cachedPath = recovered
				info = rInfo
				entry.CachedPath = recovered
				stats.Recovered++
			} else {
				fmt.Fprintf(out, "skip %s: file not found at %s\n", id, cachedPath)
				stats.Skipped++
				continue
			}
		}

		base := filepath.Base(cachedPath)
		dest := filepath.Join(globalCacheDir, base)

		// Check for conflict
		if destInfo, err := os.Stat(dest); err == nil {
			if destInfo.Size() == info.Size() {
				// Same size — assume same file, skip move but update entry
				fmt.Fprintf(out, "skip %s: already exists at %s (same size)\n", id, dest)
				entry.CachedPath = dest
				globalIdx.SetEntry(entry)
				stats.Skipped++
				continue
			}
			// Different size — deduplicate filename
			dest = deduplicateFilename(globalCacheDir, base)
		}

		// Check if global index already has a live entry for this identifier
		if globalEntry, ok := globalIdx.GetByIdentifier(id); ok {
			gPath := strings.TrimSpace(globalEntry.CachedPath)
			if gPath != "" {
				if _, err := os.Stat(gPath); err == nil {
					// Global entry has a live file — global wins
					fmt.Fprintf(out, "skip %s: global entry already has live file at %s\n", id, gPath)
					stats.GlobalWins++
					continue
				}
			}
		}

		if migrateDryRun {
			fmt.Fprintf(out, "would move %s -> %s\n", cachedPath, dest)
			stats.Moved++
			continue
		}

		// Move file
		if err := moveFile(cachedPath, dest); err != nil {
			fmt.Fprintf(out, "error moving %s: %v\n", id, err)
			stats.Skipped++
			continue
		}

		fmt.Fprintf(out, "moved %s -> %s\n", cachedPath, dest)
		entry.CachedPath = dest
		globalIdx.SetEntry(entry)
		stats.Moved++

		// Remove from local index
		localIdx.DeleteEntry(id)
		stats.EntriesCleaned++
	}

	// Move orphaned files (in cache dir but not in any index entry)
	indexedBasenames := make(map[string]bool)
	for _, entry := range globalIdx.Entries {
		if p := strings.TrimSpace(entry.CachedPath); p != "" {
			indexedBasenames[filepath.Base(p)] = true
		}
	}
	if dirEntries, err := os.ReadDir(localCacheDir); err == nil {
		for _, de := range dirEntries {
			if de.IsDir() {
				continue
			}
			name := de.Name()
			if indexedBasenames[name] {
				continue
			}
			src := filepath.Join(localCacheDir, name)
			dest := filepath.Join(globalCacheDir, name)
			if _, err := os.Stat(dest); err == nil {
				continue // already exists in global
			}
			if migrateDryRun {
				fmt.Fprintf(out, "would move orphan %s -> %s\n", src, dest)
			} else {
				if err := moveFile(src, dest); err != nil {
					fmt.Fprintf(out, "error moving orphan %s: %v\n", name, err)
					continue
				}
				fmt.Fprintf(out, "moved orphan %s -> %s\n", src, dest)
			}
			stats.Orphans++
		}
	}

	// Merge links from local into global
	for link, identifier := range localIdx.Links {
		if _, ok := globalIdx.LookupLink(link); !ok {
			globalIdx.SetLink(link, identifier)
			stats.LinksMerged++
		}
	}

	if !migrateDryRun {
		if err := cache.SaveToPath(globalIndexFile, globalIdx); err != nil {
			return fmt.Errorf("save global index: %w", err)
		}
		if err := cache.SaveToPath(localIndexFile, localIdx); err != nil {
			return fmt.Errorf("save local index: %w", err)
		}
	}

	fmt.Fprintf(out, "\nMigration %s: %d moved, %d already global, %d skipped",
		dryRunLabel(migrateDryRun), stats.Moved, stats.AlreadyGlobal, stats.Skipped)
	if stats.Recovered > 0 {
		fmt.Fprintf(out, ", %d recovered", stats.Recovered)
	}
	if stats.Orphans > 0 {
		fmt.Fprintf(out, ", %d orphans", stats.Orphans)
	}
	if stats.GlobalWins > 0 {
		fmt.Fprintf(out, ", %d global wins", stats.GlobalWins)
	}
	if stats.LinksMerged > 0 {
		fmt.Fprintf(out, ", %d links merged", stats.LinksMerged)
	}
	fmt.Fprintln(out)

	if !migrateDryRun && stats.Moved > 0 {
		// Check if local cache dir is empty and could be cleaned up
		remaining, _ := os.ReadDir(localCacheDir)
		if len(remaining) == 0 {
			fmt.Fprintf(out, "Local cache directory is now empty: %s\n", localCacheDir)
		}
	}

	return nil
}

func dryRunLabel(dryRun bool) string {
	if dryRun {
		return "(dry run)"
	}
	return "complete"
}

// moveFile moves a file from src to dest, falling back to copy+remove for
// cross-device moves.
func moveFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	// Fallback: copy + remove (cross-device)
	return copyAndRemove(src, dest)
}

func copyAndRemove(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		os.Remove(dest) // clean up partial
		return fmt.Errorf("copy: %w", err)
	}

	if err := destFile.Close(); err != nil {
		return fmt.Errorf("close dest: %w", err)
	}
	srcFile.Close()

	return os.Remove(src)
}

// deduplicateFilename adds a numeric suffix to avoid filename collisions.
func deduplicateFilename(dir, base string) string {
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
