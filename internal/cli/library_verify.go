package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
)

var verifyFix bool

func newLibraryVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Check integrity of library sources via ffprobe",
		RunE:  runLibraryVerify,
	}

	cmd.Flags().BoolVar(&verifyFix, "fix", false, "Re-download corrupt or missing URL sources")
	return cmd
}

type verifyResult struct {
	Valid   int           `json:"valid"`
	Missing int           `json:"missing"`
	Corrupt int           `json:"corrupt"`
	Fixed   int           `json:"fixed"`
	Entries []verifyEntry `json:"entries,omitempty"`
}

type verifyEntry struct {
	Identifier string `json:"identifier"`
	Status     string `json:"status"` // valid, missing, corrupt, fixed
	Path       string `json:"path,omitempty"`
	Error      string `json:"error,omitempty"`
}

func runLibraryVerify(cmd *cobra.Command, _ []string) error {
	idx, _, indexFile, err := loadLibraryIndex()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	result := verifyResult{}

	// Find ffprobe
	ffprobe := findFFprobe()

	entries := sortedEntries(idx)
	indexModified := false

	for _, e := range entries {
		ve := verifyEntry{
			Identifier: e.Identifier,
			Path:       e.CachedPath,
		}

		path := strings.TrimSpace(e.CachedPath)
		if path == "" {
			ve.Status = "missing"
			ve.Error = "no cached path"
			result.Missing++
			result.Entries = append(result.Entries, ve)
			if !outputJSON {
				fmt.Fprintf(out, "MISSING  %s (no cached path)\n", truncateStr(e.Identifier, 60))
			}
			continue
		}

		if _, err := os.Stat(path); err != nil {
			ve.Status = "missing"
			ve.Error = "file not found"
			result.Missing++
			result.Entries = append(result.Entries, ve)
			if !outputJSON {
				fmt.Fprintf(out, "MISSING  %s (%s)\n", truncateStr(e.Identifier, 60), path)
			}
			continue
		}

		// Probe with ffprobe if available
		if ffprobe != "" {
			if err := probeFile(cmd.Context(), ffprobe, path); err != nil {
				ve.Status = "corrupt"
				ve.Error = err.Error()
				result.Corrupt++
				if !outputJSON {
					fmt.Fprintf(out, "CORRUPT  %s (%s)\n", truncateStr(e.Identifier, 60), err.Error())
				}

				if verifyFix && e.SourceType == cache.SourceTypeURL && len(e.Links) > 0 {
					// Remove corrupt file so re-fetch can occur
					os.Remove(path)
					updated := e
					updated.CachedPath = ""
					idx.SetEntry(updated)
					indexModified = true
					ve.Status = "fixed"
					result.Fixed++
					if !outputJSON {
						fmt.Fprintf(out, "  -> marked for re-download\n")
					}
				}

				result.Entries = append(result.Entries, ve)
				continue
			}
		}

		ve.Status = "valid"
		result.Valid++
		result.Entries = append(result.Entries, ve)
	}

	if indexModified {
		if err := cache.SaveToPath(indexFile, idx); err != nil {
			return fmt.Errorf("save library index: %w", err)
		}
	}

	if outputJSON {
		return json.NewEncoder(out).Encode(result)
	}

	fmt.Fprintf(out, "\nVerification complete: %d valid", result.Valid)
	if result.Missing > 0 {
		fmt.Fprintf(out, ", %d missing", result.Missing)
	}
	if result.Corrupt > 0 {
		fmt.Fprintf(out, ", %d corrupt", result.Corrupt)
	}
	if result.Fixed > 0 {
		fmt.Fprintf(out, ", %d marked for re-download", result.Fixed)
	}
	fmt.Fprintln(out)

	if result.Fixed > 0 {
		fmt.Fprintln(out, "Run 'powerhour fetch' in a project to re-download fixed entries.")
	}

	return nil
}

func findFFprobe() string {
	// Check common locations
	if path, err := exec.LookPath("ffprobe"); err == nil {
		return path
	}
	return ""
}

func probeFile(ctx context.Context, ffprobe, path string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		path,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ffprobe failed: %s", msg)
	}
	return nil
}
