package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
)

var (
	importProjectDir string
	importDryRun     bool
)

func newLibraryImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a project's local cache into the library",
		RunE:  runLibraryImport,
	}

	cmd.Flags().StringVar(&importProjectDir, "project", "", "Path to the project directory to import from (required)")
	cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Print actions without moving files")
	return cmd
}

type importStats struct {
	Moved          int `json:"moved"`
	AlreadyGlobal  int `json:"already_in_library"`
	Skipped        int `json:"skipped"`
	Recovered      int `json:"recovered"`
	Orphans        int `json:"orphans"`
	LibraryWins    int `json:"library_wins"`
	LinksMerged    int `json:"links_merged"`
	EntriesCleaned int `json:"entries_cleaned"`
}

func runLibraryImport(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(importProjectDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)

	// We need the project-local paths (not library-applied)
	localCacheDir := pp.CacheDir
	localIndexFile := pp.IndexFile

	// Resolve library paths
	libSourcesDir, libIndexFile, err := resolveLibraryPaths()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	// Load local index
	localIdx, err := cache.LoadFromPath(localIndexFile)
	if err != nil {
		return fmt.Errorf("load local index: %w", err)
	}

	if len(localIdx.Entries) == 0 {
		fmt.Fprintln(out, "No entries in local cache index, nothing to import.")
		return nil
	}

	// Load library index
	libIdx, err := cache.LoadFromPath(libIndexFile)
	if err != nil {
		return fmt.Errorf("load library index: %w", err)
	}

	// Ensure library sources dir exists
	if !importDryRun {
		if err := os.MkdirAll(libSourcesDir, 0o755); err != nil {
			return fmt.Errorf("create library sources dir: %w", err)
		}
	}

	stats := importStats{}

	for id, entry := range localIdx.Entries {
		cachedPath := strings.TrimSpace(entry.CachedPath)
		if cachedPath == "" {
			continue
		}

		// Skip if already in library dir
		if strings.HasPrefix(cachedPath, libSourcesDir+string(filepath.Separator)) || cachedPath == libSourcesDir {
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
		dest := filepath.Join(libSourcesDir, base)

		// Check for conflict
		if destInfo, err := os.Stat(dest); err == nil {
			if destInfo.Size() == info.Size() {
				// Same size — assume same file, skip move but update entry
				fmt.Fprintf(out, "skip %s: already exists at %s (same size)\n", id, dest)
				entry.CachedPath = dest
				libIdx.SetEntry(entry)
				stats.Skipped++
				continue
			}
			// Different size — deduplicate filename
			dest = deduplicateFilename(libSourcesDir, base)
		}

		// Check if library index already has a live entry for this identifier
		if libEntry, ok := libIdx.GetByIdentifier(id); ok {
			gPath := strings.TrimSpace(libEntry.CachedPath)
			if gPath != "" {
				if _, err := os.Stat(gPath); err == nil {
					// Library entry has a live file — library wins
					fmt.Fprintf(out, "skip %s: library already has live file at %s\n", id, gPath)
					stats.LibraryWins++
					continue
				}
			}
		}

		if importDryRun {
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
		libIdx.SetEntry(entry)
		stats.Moved++

		// Remove from local index
		localIdx.DeleteEntry(id)
		stats.EntriesCleaned++
	}

	// Move orphaned files (in cache dir but not in any index entry)
	indexedBasenames := make(map[string]bool)
	for _, entry := range libIdx.Entries {
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
			dest := filepath.Join(libSourcesDir, name)
			if _, err := os.Stat(dest); err == nil {
				continue // already exists in library
			}
			if importDryRun {
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

	// Merge links from local into library
	for link, identifier := range localIdx.Links {
		if _, ok := libIdx.LookupLink(link); !ok {
			libIdx.SetLink(link, identifier)
			stats.LinksMerged++
		}
	}

	if !importDryRun {
		if err := cache.SaveToPath(libIndexFile, libIdx); err != nil {
			return fmt.Errorf("save library index: %w", err)
		}
		if err := cache.SaveToPath(localIndexFile, localIdx); err != nil {
			return fmt.Errorf("save local index: %w", err)
		}
	}

	label := "complete"
	if importDryRun {
		label = "(dry run)"
	}
	fmt.Fprintf(out, "\nImport %s: %d moved, %d already in library, %d skipped",
		label, stats.Moved, stats.AlreadyGlobal, stats.Skipped)
	if stats.Recovered > 0 {
		fmt.Fprintf(out, ", %d recovered", stats.Recovered)
	}
	if stats.Orphans > 0 {
		fmt.Fprintf(out, ", %d orphans", stats.Orphans)
	}
	if stats.LibraryWins > 0 {
		fmt.Fprintf(out, ", %d library wins", stats.LibraryWins)
	}
	if stats.LinksMerged > 0 {
		fmt.Fprintf(out, ", %d links merged", stats.LinksMerged)
	}
	fmt.Fprintln(out)

	if !importDryRun && stats.Moved > 0 {
		// Check if local cache dir is empty and could be cleaned up
		remaining, _ := os.ReadDir(localCacheDir)
		if len(remaining) == 0 {
			fmt.Fprintf(out, "Local cache directory is now empty: %s\n", localCacheDir)
		}
	}

	return nil
}
