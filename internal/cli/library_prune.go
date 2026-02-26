package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
)

var (
	pruneDryRun    bool
	pruneOlderThan string
)

func newLibraryPruneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove sources not used recently",
		RunE:  runLibraryPrune,
	}

	cmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "List what would be removed without deleting")
	cmd.Flags().StringVar(&pruneOlderThan, "older-than", "90d", "Prune entries not used within this duration (e.g., 30d, 6m, 1y)")
	return cmd
}

type pruneResult struct {
	Pruned     int   `json:"pruned"`
	FreedBytes int64 `json:"freed_bytes"`
	Skipped    int   `json:"skipped"`
	DryRun     bool  `json:"dry_run"`
}

func runLibraryPrune(cmd *cobra.Command, _ []string) error {
	cutoff, err := parseDurationFlag(pruneOlderThan)
	if err != nil {
		return fmt.Errorf("invalid --older-than: %w", err)
	}

	idx, _, indexFile, err := loadLibraryIndex()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	result := pruneResult{DryRun: pruneDryRun}
	now := time.Now().UTC()
	threshold := now.Add(-cutoff)

	for id, e := range idx.Entries {
		lastUsed := e.LastUsedAt
		if lastUsed.IsZero() {
			// Fall back to retrieval time
			lastUsed = e.RetrievedAt
		}

		if !lastUsed.IsZero() && lastUsed.After(threshold) {
			result.Skipped++
			continue
		}

		path := strings.TrimSpace(e.CachedPath)

		if pruneDryRun {
			fmt.Fprintf(out, "would prune %s", truncateStr(id, 60))
			if path != "" {
				fmt.Fprintf(out, " (%s)", formatSize(e.SizeBytes))
			}
			fmt.Fprintln(out)
			result.Pruned++
			result.FreedBytes += e.SizeBytes
			continue
		}

		// Delete the file
		if path != "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(out, "error removing %s: %v\n", path, err)
				result.Skipped++
				continue
			}
		}

		// Remove from index
		idx.DeleteEntry(id)
		// Clean up link references
		for link, target := range idx.Links {
			if target == id {
				idx.DeleteLink(link)
			}
		}

		result.Pruned++
		result.FreedBytes += e.SizeBytes

		if !outputJSON {
			fmt.Fprintf(out, "pruned %s (%s)\n", truncateStr(id, 60), formatSize(e.SizeBytes))
		}
	}

	if !pruneDryRun && result.Pruned > 0 {
		if err := cache.SaveToPath(indexFile, idx); err != nil {
			return fmt.Errorf("save library index: %w", err)
		}
	}

	if outputJSON {
		return json.NewEncoder(out).Encode(result)
	}

	label := "complete"
	if pruneDryRun {
		label = "(dry run)"
	}
	fmt.Fprintf(out, "\nPrune %s: %d removed, %s freed, %d kept\n",
		label, result.Pruned, formatSize(result.FreedBytes), result.Skipped)

	return nil
}

// parseDurationFlag parses human-friendly duration strings like "30d", "6m", "1y".
func parseDurationFlag(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	last := s[len(s)-1]
	numStr := s[:len(s)-1]

	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid number in %q", s)
	}

	switch last {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'm':
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	case 'y':
		return time.Duration(n) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit %q in %q (use d, m, or y)", string(last), s)
	}
}
