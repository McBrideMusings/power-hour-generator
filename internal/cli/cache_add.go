package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/tui"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the project media cache",
	}
	cmd.AddCommand(newCacheAddCmd())
	return cmd
}

func newCacheAddCmd() *cobra.Command {
	var (
		titleFlag  string
		artistFlag string
		dryRun     bool
		noProbe    bool
	)

	cmd := &cobra.Command{
		Use:   "add <url> <file-path>",
		Short: "Register a manually-downloaded video into the project cache",
		Long:  "Associates a local video file with its original URL so it can be used during render like any other cached source.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheAdd(cmd.Context(), args[0], args[1], titleFlag, artistFlag, dryRun, noProbe)
		},
	}

	cmd.Flags().StringVar(&titleFlag, "title", "", "Override title metadata")
	cmd.Flags().StringVar(&artistFlag, "artist", "", "Override artist metadata")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what would happen without making changes")
	cmd.Flags().BoolVar(&noProbe, "no-probe", false, "Skip ffprobe metadata extraction")

	return cmd
}

func runCacheAdd(ctx context.Context, rawURL, filePath, titleFlag, artistFlag string, dryRun, noProbe bool) error {
	glogf, closer := logx.StartCommand("cache-add")
	defer closer.Close()

	// Validate file exists
	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolve file path: %w", err)
	}
	info, err := os.Stat(absFile)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a file", absFile)
	}

	// Validate URL
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid URL: %s", rawURL)
	}

	glogf("cache add url=%s file=%s", rawURL, absFile)

	status := tui.NewStatusWriter(os.Stderr)
	defer status.Stop()

	// Resolve project paths and construct cache service
	status.Update("Resolving project...")
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	status.Update("Detecting tools...")
	svc, err := cache.NewServiceWithStatus(ctx, pp, nil, nil, status.Update)
	if err != nil {
		return err
	}

	// Reload pp from svc (it applies config + library settings)
	pp = svc.Paths

	// Load existing index
	status.Update("Loading cache index...")
	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	// Try yt-dlp metadata query
	status.Update("Querying video metadata...")
	var identifier, extractor, videoID, title, artist string

	remoteInfo, queryErr := svc.QueryRemoteID(ctx, rawURL)
	if queryErr == nil {
		glogf("yt-dlp metadata: id=%s extractor=%s title=%s", remoteInfo.ID, remoteInfo.Extractor, remoteInfo.Title)
		extractor = remoteInfo.Extractor
		videoID = remoteInfo.ID
		identifier = cache.CanonicalRemoteIdentifier(rawURL, extractor, videoID)
		title = remoteInfo.Title
		artist = remoteInfo.Artist
	} else {
		glogf("yt-dlp metadata query failed: %v", queryErr)
		status.Stop()
		fmt.Fprintf(os.Stderr, "yt-dlp metadata query failed: %v\n", queryErr)
		fmt.Fprintln(os.Stderr, "Falling back to manual identification...")
		fmt.Fprintln(os.Stderr)

		// Try to extract YouTube ID from URL
		if ytID := cache.ExtractYouTubeID(rawURL); ytID != "" {
			extractor = "youtube"
			videoID = ytID
			fmt.Fprintf(os.Stderr, "Detected platform: %s\n", extractor)
			fmt.Fprintf(os.Stderr, "Detected video ID: %s\n", videoID)
			fmt.Fprintln(os.Stderr)
		} else {
			// Try hostname + path heuristic
			host := u.Hostname()
			pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(pathParts) > 0 && pathParts[len(pathParts)-1] != "" {
				videoID = pathParts[len(pathParts)-1]
				// Derive platform from hostname
				extractor = extractPlatformFromHost(host)
				fmt.Fprintf(os.Stderr, "Detected platform: %s\n", extractor)
				fmt.Fprintf(os.Stderr, "Detected video ID: %s\n", videoID)
				fmt.Fprintln(os.Stderr)
			}

			if videoID == "" {
				fmt.Fprintln(os.Stderr, "Could not parse video ID from URL.")
				fmt.Fprintln(os.Stderr)
				scanner := bufio.NewScanner(os.Stdin)
				extractor = promptField(scanner, "Platform", "unknown")
				videoID = promptField(scanner, "Video ID", "")
				if videoID == "" {
					return fmt.Errorf("video ID is required")
				}
			}
		}

		identifier = cache.CanonicalRemoteIdentifier(rawURL, extractor, videoID)
	}

	// Apply flag overrides or prompt for missing metadata
	if titleFlag != "" {
		title = titleFlag
	} else if title == "" && queryErr != nil {
		scanner := bufio.NewScanner(os.Stdin)
		title = promptField(scanner, "Title", "")
	}

	if artistFlag != "" {
		artist = artistFlag
	} else if artist == "" && queryErr != nil {
		scanner := bufio.NewScanner(os.Stdin)
		artist = promptField(scanner, "Artist", "")
	}

	// Determine cache filename
	baseName := cache.SanitizeSegment(videoID)
	if baseName == "" {
		baseName = cache.SanitizeSegment(cache.HashIdentifier(rawURL)[:12])
	}
	ext := filepath.Ext(absFile)
	targetPath := filepath.Join(pp.CacheDir, baseName+ext)

	// Check for existing entry
	if existing, ok := idx.GetByIdentifier(identifier); ok {
		fmt.Fprintf(os.Stderr, "Warning: entry already exists for %s (cached at %s)\n", identifier, existing.CachedPath)
		fmt.Fprintln(os.Stderr, "Overwriting existing entry.")
	}

	if dryRun {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Dry run — no changes made.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "  Identifier:  %s\n", identifier)
		fmt.Fprintf(os.Stderr, "  Source:      %s\n", rawURL)
		fmt.Fprintf(os.Stderr, "  File:        %s\n", absFile)
		fmt.Fprintf(os.Stderr, "  Cache path:  %s\n", targetPath)
		fmt.Fprintf(os.Stderr, "  Title:       %s\n", title)
		fmt.Fprintf(os.Stderr, "  Artist:      %s\n", artist)
		fmt.Fprintf(os.Stderr, "  Size:        %d bytes\n", info.Size())
		if !noProbe {
			fmt.Fprintln(os.Stderr, "  Probe:       would run ffprobe")
		}
		return nil
	}

	// Copy file to cache
	status = tui.NewStatusWriter(os.Stderr)
	defer status.Stop()
	status.Update("Copying file to cache...")

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("ensure cache dir: %w", err)
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing cache file: %w", err)
	}

	hardLinked, err := cache.TryLinkOrCopy(absFile, targetPath)
	if err != nil {
		return fmt.Errorf("copy to cache: %w", err)
	}

	copyMethod := "copied"
	if hardLinked {
		copyMethod = "hardlinked"
	}
	glogf("file %s to %s", copyMethod, targetPath)

	// Probe if requested
	var probe *cache.ProbeMetadata
	if !noProbe {
		status.Update("Running ffprobe...")
		probe, err = svc.ProbeFile(ctx, targetPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: ffprobe failed: %v\n", err)
			glogf("ffprobe failed: %v", err)
		}
	}

	// Build and save entry
	now := time.Now()
	entry := cache.Entry{
		Key:         cache.HashIdentifier(identifier),
		Identifier:  identifier,
		ID:          videoID,
		Extractor:   extractor,
		Source:      rawURL,
		SourceType:  cache.SourceTypeURL,
		CachedPath:  targetPath,
		RetrievedAt: now,
		SizeBytes:   info.Size(),
		Probe:       probe,
		Title:       title,
		Artist:      artist,
		Notes:       []string{"manually added via cache add"},
		Links:       []string{rawURL},
		LastUsedAt:  now,
	}
	if probe != nil {
		entry.LastProbeAt = now
	}

	idx.SetEntry(entry)
	idx.SetLink(rawURL, identifier)

	status.Update("Saving index...")
	if err := cache.Save(pp, idx); err != nil {
		return fmt.Errorf("save index: %w", err)
	}

	status.Stop()

	// Print summary
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Cached successfully.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  Identifier:  %s\n", identifier)
	fmt.Fprintf(os.Stderr, "  Cache path:  %s\n", targetPath)
	fmt.Fprintf(os.Stderr, "  Method:      %s\n", copyMethod)
	fmt.Fprintf(os.Stderr, "  Size:        %d bytes\n", info.Size())
	if title != "" {
		fmt.Fprintf(os.Stderr, "  Title:       %s\n", title)
	}
	if artist != "" {
		fmt.Fprintf(os.Stderr, "  Artist:      %s\n", artist)
	}
	if probe != nil {
		fmt.Fprintf(os.Stderr, "  Duration:    %s\n", formatProbeSeconds(probe.DurationSeconds))
		fmt.Fprintf(os.Stderr, "  Format:      %s\n", probe.FormatName)
	}

	return nil
}

func promptField(scanner *bufio.Scanner, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, defaultVal)
	} else {
		fmt.Fprintf(os.Stderr, "%s []: ", label)
	}
	if scanner.Scan() {
		val := strings.TrimSpace(scanner.Text())
		if val != "" {
			return val
		}
	}
	return defaultVal
}

func extractPlatformFromHost(host string) string {
	host = strings.ToLower(host)
	host = strings.TrimPrefix(host, "www.")

	switch {
	case strings.Contains(host, "youtube"):
		return "youtube"
	case strings.Contains(host, "vimeo"):
		return "vimeo"
	case strings.Contains(host, "dailymotion"):
		return "dailymotion"
	case strings.Contains(host, "twitch"):
		return "twitch"
	default:
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			return parts[0]
		}
		return "unknown"
	}
}

func formatProbeSeconds(s float64) string {
	total := int(s)
	hours := total / 3600
	minutes := (total % 3600) / 60
	secs := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}
