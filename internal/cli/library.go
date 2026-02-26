package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
)

func newLibraryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Manage the shared media library",
	}

	cmd.AddCommand(
		newLibraryListCmd(),
		newLibrarySearchCmd(),
		newLibraryInfoCmd(),
		newLibraryVerifyCmd(),
		newLibraryPruneCmd(),
		newLibraryImportCmd(),
	)

	return cmd
}

// resolveLibraryPaths returns library paths resolved from config + env.
// It does not require a project directory.
func resolveLibraryPaths() (sourcesDir, indexFile string, err error) {
	var cfg config.Config
	if projectDir != "" {
		pp, pErr := paths.Resolve(projectDir)
		if pErr == nil {
			if loaded, lErr := config.Load(pp.ConfigFile); lErr == nil {
				cfg = loaded
			}
		}
	}

	libDir, err := paths.LibraryDir(cfg.LibraryPath())
	if err != nil {
		return "", "", fmt.Errorf("resolve library path: %w", err)
	}

	return filepath.Join(libDir, "sources"), filepath.Join(libDir, "index.json"), nil
}

func loadLibraryIndex() (*cache.Index, string, string, error) {
	sourcesDir, indexFile, err := resolveLibraryPaths()
	if err != nil {
		return nil, "", "", err
	}

	idx, err := cache.LoadFromPath(indexFile)
	if err != nil {
		return nil, "", "", fmt.Errorf("load library index: %w", err)
	}

	return idx, sourcesDir, indexFile, nil
}

// --- library list ---

func newLibraryListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all sources in the library",
		RunE:  runLibraryList,
	}
}

func runLibraryList(cmd *cobra.Command, _ []string) error {
	idx, _, _, err := loadLibraryIndex()
	if err != nil {
		return err
	}

	entries := sortedEntries(idx)
	out := cmd.OutOrStdout()

	if outputJSON {
		return json.NewEncoder(out).Encode(entries)
	}

	if len(entries) == 0 {
		fmt.Fprintln(out, "Library is empty.")
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tARTIST\tDURATION\tSIZE")
	for _, e := range entries {
		id := truncateStr(entryDisplayID(e), 30)
		title := truncateStr(e.Title, 40)
		artist := truncateStr(e.Artist, 25)
		dur := formatDuration(e)
		size := formatSize(e.SizeBytes)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, title, artist, dur, size)
	}
	return w.Flush()
}

// --- library search ---

func newLibrarySearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search library by title, artist, or URL",
		Args:  cobra.ExactArgs(1),
		RunE:  runLibrarySearch,
	}
}

func runLibrarySearch(cmd *cobra.Command, args []string) error {
	query := strings.ToLower(args[0])

	idx, _, _, err := loadLibraryIndex()
	if err != nil {
		return err
	}

	var matches []cache.Entry
	for _, e := range sortedEntries(idx) {
		if entryMatches(e, query) {
			matches = append(matches, e)
		}
	}

	out := cmd.OutOrStdout()

	if outputJSON {
		return json.NewEncoder(out).Encode(matches)
	}

	if len(matches) == 0 {
		fmt.Fprintf(out, "No results for %q.\n", args[0])
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tARTIST\tDURATION\tSIZE")
	for _, e := range matches {
		id := truncateStr(entryDisplayID(e), 30)
		title := truncateStr(e.Title, 40)
		artist := truncateStr(e.Artist, 25)
		dur := formatDuration(e)
		size := formatSize(e.SizeBytes)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, title, artist, dur, size)
	}
	return w.Flush()
}

func entryMatches(e cache.Entry, query string) bool {
	fields := []string{
		e.Title, e.Artist, e.Track, e.Album,
		e.Source, e.Identifier, e.ID,
	}
	fields = append(fields, e.Links...)

	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}

// --- library info ---

type libraryInfo struct {
	SourcesDir   string `json:"sources_dir"`
	IndexFile    string `json:"index_file"`
	EntryCount   int    `json:"entry_count"`
	TotalSize    int64  `json:"total_size_bytes"`
	OrphanCount  int    `json:"orphan_count"`
	MissingCount int    `json:"missing_count"`
}

func newLibraryInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show library statistics",
		RunE:  runLibraryInfo,
	}
}

func runLibraryInfo(cmd *cobra.Command, _ []string) error {
	idx, sourcesDir, indexFile, err := loadLibraryIndex()
	if err != nil {
		return err
	}

	info := libraryInfo{
		SourcesDir: sourcesDir,
		IndexFile:  indexFile,
		EntryCount: len(idx.Entries),
	}

	// Calculate total size and find missing entries
	indexedBasenames := make(map[string]bool)
	for _, e := range idx.Entries {
		info.TotalSize += e.SizeBytes
		if p := strings.TrimSpace(e.CachedPath); p != "" {
			indexedBasenames[filepath.Base(p)] = true
			if _, err := os.Stat(p); err != nil {
				info.MissingCount++
			}
		}
	}

	// Count orphan files in sources dir
	if dirEntries, err := os.ReadDir(sourcesDir); err == nil {
		for _, de := range dirEntries {
			if de.IsDir() {
				continue
			}
			if !indexedBasenames[de.Name()] {
				info.OrphanCount++
			}
		}
	}

	out := cmd.OutOrStdout()

	if outputJSON {
		return json.NewEncoder(out).Encode(info)
	}

	fmt.Fprintf(out, "Library path:  %s\n", sourcesDir)
	fmt.Fprintf(out, "Index:         %s\n", indexFile)
	fmt.Fprintf(out, "Entries:       %d\n", info.EntryCount)
	fmt.Fprintf(out, "Total size:    %s\n", formatSize(info.TotalSize))
	if info.MissingCount > 0 {
		fmt.Fprintf(out, "Missing files: %d\n", info.MissingCount)
	}
	if info.OrphanCount > 0 {
		fmt.Fprintf(out, "Orphan files:  %d\n", info.OrphanCount)
	}

	return nil
}

// --- helpers ---

func sortedEntries(idx *cache.Index) []cache.Entry {
	entries := make([]cache.Entry, 0, len(idx.Entries))
	for _, e := range idx.Entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Identifier < entries[j].Identifier
	})
	return entries
}

func entryDisplayID(e cache.Entry) string {
	if e.ID != "" {
		return e.ID
	}
	if e.Identifier != "" {
		return e.Identifier
	}
	return e.Key
}

func formatDuration(e cache.Entry) string {
	if e.Probe == nil || e.Probe.DurationSeconds <= 0 {
		return "-"
	}
	d := time.Duration(e.Probe.DurationSeconds * float64(time.Second))
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func formatSize(bytes int64) string {
	if bytes <= 0 {
		return "-"
	}
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
