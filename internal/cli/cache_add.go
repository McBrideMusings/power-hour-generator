package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/tui"
	"powerhour/pkg/csvplan"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage project cache entries",
		RunE:  runCacheLegacyAdd,
	}

	cmd.AddCommand(newCacheAddCmd())
	cmd.AddCommand(newCacheRemoveCmd())
	cmd.AddCommand(newCacheDoctorCmd())
	return cmd
}

func newCacheAddCmd() *cobra.Command {
	var (
		urlFlag    string
		titleFlag  string
		artistFlag string
		dryRun     bool
		noProbe    bool
	)

	cmd := &cobra.Command{
		Use:   "add <file-or-id>",
		Short: "Register a video into the project cache",
		Long: `Register a local video file or download a video by YouTube ID into
the project cache so it can be used during render.

Examples:
  powerhour cache HWl1Tu9oZmY.webm              # local file, auto-resolves URL
  powerhour cache "Title [HWl1Tu9oZmY].webm"    # yt-dlp filename format
  powerhour cache HWl1Tu9oZmY                    # downloads by YouTube ID
  powerhour cache song.webm --url https://...    # explicit URL override`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := args[0]

			// Check if the argument is a file on disk
			if info, err := os.Stat(arg); err == nil && !info.IsDir() {
				return runCacheFile(cmd.Context(), arg, urlFlag, titleFlag, artistFlag, dryRun, noProbe)
			}

			// Not a file — treat as a YouTube ID if it looks like one
			if cache.LooksLikeYouTubeID(arg) {
				return runCacheDownload(cmd.Context(), arg, titleFlag, artistFlag, dryRun)
			}

			// Also try extracting ID from a filename-like string that doesn't exist on disk
			if id := cache.ExtractVideoIDFromFilename(arg); id != "" {
				return runCacheDownload(cmd.Context(), id, titleFlag, artistFlag, dryRun)
			}

			return fmt.Errorf("not a file on disk and not a recognized video ID: %s", arg)
		},
	}

	cmd.Flags().StringVar(&urlFlag, "url", "", "Source URL (auto-detected from filename if omitted)")
	cmd.Flags().StringVar(&titleFlag, "title", "", "Override title metadata")
	cmd.Flags().StringVar(&artistFlag, "artist", "", "Override artist metadata")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what would happen without making changes")
	cmd.Flags().BoolVar(&noProbe, "no-probe", false, "Skip ffprobe metadata extraction")

	return cmd
}

func runCacheLegacyAdd(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	add := newCacheAddCmd()
	add.SetContext(cmd.Context())
	add.SetArgs(args)
	add.SetOut(cmd.OutOrStdout())
	add.SetErr(cmd.ErrOrStderr())
	return add.Execute()
}

// runCacheDownload downloads a video by YouTube ID and registers it in the cache.
func runCacheDownload(ctx context.Context, videoID, titleFlag, artistFlag string, dryRun bool) error {
	glogf, closer := logx.StartCommand("cache")
	defer closer.Close()

	rawURL := "https://www.youtube.com/watch?v=" + videoID
	glogf("cache download id=%s url=%s", videoID, rawURL)

	status := tui.NewStatusWriter(os.Stderr)
	defer status.Stop()

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
	pp = svc.Paths

	status.Update("Loading cache index...")
	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	// Check if already cached
	identifier := "youtube:" + videoID
	if existing, ok := idx.GetByIdentifier(identifier); ok {
		status.Stop()
		printCacheEntry("Already cached.", existing)
		return nil
	}

	if dryRun {
		status.Stop()
		fmt.Fprintf(os.Stderr, "Would download: %s\n", rawURL)
		return nil
	}

	// Use the cache service's Resolve to download
	status.Update("Downloading...")
	row := csvplan.Row{
		Index: 1,
		Link:  rawURL,
	}
	result, err := svc.Resolve(ctx, idx, row, cache.ResolveOptions{})
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Apply flag overrides
	if result.Identifier != "" {
		if entry, ok := idx.GetByIdentifier(result.Identifier); ok {
			if titleFlag != "" {
				entry.Title = titleFlag
			}
			if artistFlag != "" {
				entry.Artist = artistFlag
			}
			idx.SetEntry(entry)
			if err := cache.Save(pp, idx); err != nil {
				return fmt.Errorf("save index: %w", err)
			}
			status.Stop()
			printCacheEntry("Cached.", entry)
			return nil
		}
	}

	status.Stop()
	fmt.Fprintf(os.Stderr, "Cached: %s → %s\n", rawURL, result.Entry.CachedPath)
	return nil
}

// runCacheFile registers a local file into the cache.
func runCacheFile(ctx context.Context, filePath, urlFlag, titleFlag, artistFlag string, dryRun, noProbe bool) error {
	glogf, closer := logx.StartCommand("cache")
	defer closer.Close()

	// Resolve URL and plan metadata
	rawURL := urlFlag
	var planTitle, planArtist string
	if rawURL == "" {
		match, err := resolveFromPlans(filePath)
		if err != nil {
			return err
		}
		rawURL = match.URL
		planTitle = match.Title
		planArtist = match.Artist
	}

	status := tui.NewStatusWriter(os.Stderr)
	defer status.Stop()

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
	pp = svc.Paths

	status.Update("Loading cache index...")
	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	// Flag overrides fall back to plan-derived metadata; RegisterLocalFile
	// itself only knows about flag-level overrides vs. yt-dlp metadata.
	title := titleFlag
	if title == "" {
		title = planTitle
	}
	artist := artistFlag
	if artist == "" {
		artist = planArtist
	}

	result, err := cache.RegisterLocalFile(ctx, svc, pp, idx, cache.RegisterLocalFileParams{
		FilePath:  filePath,
		SourceURL: rawURL,
		Title:     title,
		Artist:    artist,
		NoProbe:   noProbe,
		DryRun:    dryRun,
		StatusFn:  status.Update,
		Logf:      glogf,
	})
	if err != nil {
		return err
	}

	status.Stop()

	if result.AlreadyCached {
		printCacheEntry("Already cached.", result.Entry)
		return nil
	}

	if result.DryRun {
		fmt.Fprintln(os.Stderr, "Dry run — no changes made.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "  Source:  %s\n", result.Entry.Source)
		fmt.Fprintf(os.Stderr, "  File:    %s\n", filePath)
		fmt.Fprintf(os.Stderr, "  Cache:   %s\n", result.TargetPath)
		if result.Entry.Title != "" {
			fmt.Fprintf(os.Stderr, "  Title:   %s\n", result.Entry.Title)
		}
		if result.Entry.Artist != "" {
			fmt.Fprintf(os.Stderr, "  Artist:  %s\n", result.Entry.Artist)
		}
		return nil
	}

	printCacheEntry("Cached.", result.Entry)
	return nil
}

func printCacheEntry(header string, entry cache.Entry) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, header)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  ID:      %s\n", entry.ID)
	fmt.Fprintf(os.Stderr, "  File:    %s\n", entry.CachedPath)
	fmt.Fprintf(os.Stderr, "  Title:   %s\n", entry.Title)
	fmt.Fprintf(os.Stderr, "  Artist:  %s\n", entry.Artist)
	if entry.Probe != nil {
		fmt.Fprintf(os.Stderr, "  Length:  %s\n", formatProbeSeconds(entry.Probe.DurationSeconds))
	}
}

// planMatch holds a URL and optional metadata resolved from a collection plan.
type planMatch struct {
	URL    string
	Title  string
	Artist string
}

func resolveFromPlans(filePath string) (*planMatch, error) {
	videoID := cache.ExtractVideoIDFromFilename(filepath.Base(filePath))

	// Try matching against collection plans
	pp, err := paths.Resolve(projectDir)
	if err == nil {
		cfg, cfgErr := config.Load(pp.ConfigFile)
		if cfgErr == nil {
			for _, collCfg := range cfg.Collections {
				plan := strings.TrimSpace(collCfg.Plan)
				if plan == "" {
					continue
				}
				planPath := plan
				if !filepath.IsAbs(planPath) {
					planPath = filepath.Join(pp.Root, planPath)
				}

				opts := csvplan.CollectionOptions{
					LinkHeader:      collCfg.LinkHeader,
					StartHeader:     collCfg.StartHeader,
					DurationHeader:  collCfg.DurationHeader,
					DefaultDuration: 60,
				}

				var rows []csvplan.CollectionRow
				ext := strings.ToLower(filepath.Ext(planPath))
				if ext == ".yaml" || ext == ".yml" {
					result, _ := csvplan.LoadCollectionYAML(planPath, opts)
					rows = result.Rows
				} else {
					rows, _ = csvplan.LoadCollection(planPath, opts)
				}

				for _, row := range rows {
					link := strings.TrimSpace(row.Link)
					if link == "" {
						continue
					}
					ytID := cache.ExtractYouTubeID(link)
					if ytID != "" && ytID == videoID {
						return &planMatch{
							URL:    link,
							Title:  row.CustomFields["title"],
							Artist: row.CustomFields["artist"],
						}, nil
					}
				}
			}
		}
	}

	// No plan match — if we extracted a YouTube ID from the filename, construct a URL
	if videoID != "" {
		return &planMatch{
			URL: "https://www.youtube.com/watch?v=" + videoID,
		}, nil
	}

	return nil, fmt.Errorf("could not resolve a URL for %q\n\nUsage: powerhour cache <file-path> [--url <url>]\n\nProvide the URL explicitly, or ensure the filename contains a YouTube video ID.", filepath.Base(filePath))
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
