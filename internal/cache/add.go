package cache

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"powerhour/internal/paths"
)

// LooksLikeYouTubeID reports whether s matches YouTube's video ID format: 11
// characters, alphanumeric plus - and _.
func LooksLikeYouTubeID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ExtractVideoIDFromFilename extracts a YouTube video ID from a filename.
// Handles two patterns:
//   - Bare ID: "HWl1Tu9oZmY.webm"
//   - yt-dlp default: "Artist - Title [HWl1Tu9oZmY].webm"
func ExtractVideoIDFromFilename(filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))

	// Try bracket suffix: "... [ID]"
	if i := strings.LastIndex(base, "["); i >= 0 {
		if j := strings.LastIndex(base, "]"); j > i {
			candidate := base[i+1 : j]
			if LooksLikeYouTubeID(candidate) {
				return candidate
			}
		}
	}

	// Try bare filename as ID.
	if LooksLikeYouTubeID(base) {
		return base
	}

	return ""
}

// ExtractPlatformFromHost derives a short platform identifier from a URL
// host, used as the "extractor" field for entries yt-dlp couldn't identify.
func ExtractPlatformFromHost(host string) string {
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

// RegisterLocalFileParams describes a request to register a local video file
// into the project cache under a resolved source URL. SourceURL is required —
// callers are responsible for resolving it (via a --url flag, a matching
// collection plan row, or a YouTube ID extracted from the filename) before
// calling in; RegisterLocalFile does no plan/config lookups of its own.
type RegisterLocalFileParams struct {
	FilePath  string // path to the local file (absolute preferred)
	SourceURL string // resolved source URL used for identity/dedupe
	Title     string // optional metadata override (flag/user-provided)
	Artist    string // optional metadata override (flag/user-provided)
	NoProbe   bool
	DryRun    bool
	StatusFn  func(string)         // optional progress callback
	Logf      func(string, ...any) // optional debug logger
}

// RegisterLocalFileResult reports what RegisterLocalFile did.
type RegisterLocalFileResult struct {
	Entry         Entry
	AlreadyCached bool // Entry was already present under the resolved identifier
	DryRun        bool // no changes were made; Entry/TargetPath are previews
	TargetPath    string
}

// RegisterLocalFile registers a local video file into the project cache: it
// resolves the file's identity against the given source URL (via yt-dlp
// metadata when possible, falling back to a video ID parsed from the URL),
// dedupes against the existing index, copies the file into the cache
// directory, probes it with ffprobe, and saves the resulting entry. This is
// the shared core behind both `powerhour cache <file>` and the TUI cache
// view's add slot.
func RegisterLocalFile(ctx context.Context, svc *Service, pp paths.ProjectPaths, idx *Index, params RegisterLocalFileParams) (RegisterLocalFileResult, error) {
	logf := params.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	status := params.StatusFn
	if status == nil {
		status = func(string) {}
	}

	absFile, err := filepath.Abs(params.FilePath)
	if err != nil {
		return RegisterLocalFileResult{}, fmt.Errorf("resolve file path: %w", err)
	}
	info, err := os.Stat(absFile)
	if err != nil {
		return RegisterLocalFileResult{}, fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return RegisterLocalFileResult{}, fmt.Errorf("%s is a directory, not a file", absFile)
	}

	rawURL := strings.TrimSpace(params.SourceURL)
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return RegisterLocalFileResult{}, fmt.Errorf("invalid URL: %s", rawURL)
	}

	logf("cache file url=%s file=%s", rawURL, absFile)

	status("Querying video metadata...")
	var (
		identifier, extractor, videoID, title, artist string
		remoteInfo                                    RemoteIDInfo
	)

	remoteInfo, queryErr := svc.QueryRemoteID(ctx, rawURL)
	if queryErr == nil {
		logf("yt-dlp metadata: id=%s extractor=%s title=%s", remoteInfo.ID, remoteInfo.Extractor, remoteInfo.Title)
		extractor = remoteInfo.Extractor
		videoID = remoteInfo.ID
		identifier = CanonicalRemoteIdentifier(rawURL, extractor, videoID)
		title = remoteInfo.Title
		artist = remoteInfo.Artist
	} else {
		logf("yt-dlp metadata query failed: %v", queryErr)

		if ytID := ExtractYouTubeID(rawURL); ytID != "" {
			extractor = "youtube"
			videoID = ytID
		} else {
			host := u.Hostname()
			pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(pathParts) > 0 && pathParts[len(pathParts)-1] != "" {
				videoID = pathParts[len(pathParts)-1]
				extractor = ExtractPlatformFromHost(host)
			}
		}

		if videoID == "" {
			return RegisterLocalFileResult{}, fmt.Errorf("could not determine video ID from URL: %s", rawURL)
		}

		identifier = CanonicalRemoteIdentifier(rawURL, extractor, videoID)
	}

	// Check if already cached.
	if existing, ok := idx.GetByIdentifier(identifier); ok {
		return RegisterLocalFileResult{Entry: existing, AlreadyCached: true}, nil
	}

	// Apply metadata: flags > yt-dlp > (none — plan fallback is the caller's job).
	if params.Title != "" {
		title = params.Title
	}
	if params.Artist != "" {
		artist = params.Artist
	}

	normalized := NormalizeMetadata(LoadNormalizationConfig(), NormalizationInput{
		Title:    title,
		Artist:   artist,
		Track:    remoteInfo.Track,
		Album:    remoteInfo.Album,
		Uploader: remoteInfo.Uploader,
		Channel:  remoteInfo.Channel,
	})
	title = normalized.Title
	artist = normalized.Artist

	// Determine cache filename.
	baseName := SanitizeSegment(videoID)
	if baseName == "" {
		baseName = SanitizeSegment(HashIdentifier(rawURL)[:12])
	}
	ext := filepath.Ext(absFile)
	targetPath := filepath.Join(pp.CacheDir, baseName+ext)

	if params.DryRun {
		return RegisterLocalFileResult{
			DryRun:     true,
			TargetPath: targetPath,
			Entry: Entry{
				Identifier: identifier,
				ID:         videoID,
				Extractor:  extractor,
				Source:     rawURL,
				CachedPath: targetPath,
				Title:      title,
				Artist:     artist,
			},
		}, nil
	}

	// Copy file to cache.
	status("Copying file to cache...")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return RegisterLocalFileResult{}, fmt.Errorf("ensure cache dir: %w", err)
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return RegisterLocalFileResult{}, fmt.Errorf("remove existing cache file: %w", err)
	}
	if _, err := TryLinkOrCopy(absFile, targetPath); err != nil {
		return RegisterLocalFileResult{}, fmt.Errorf("copy to cache: %w", err)
	}

	// Probe.
	var probe *ProbeMetadata
	if !params.NoProbe {
		status("Running ffprobe...")
		probe, err = svc.ProbeFile(ctx, targetPath)
		if err != nil {
			logf("ffprobe failed: %v", err)
		}
	}

	// Build and save entry.
	now := time.Now()
	entry := Entry{
		Key:         HashIdentifier(identifier),
		Identifier:  identifier,
		ID:          videoID,
		Extractor:   extractor,
		Source:      rawURL,
		SourceType:  SourceTypeURL,
		CachedPath:  targetPath,
		RetrievedAt: now,
		SizeBytes:   info.Size(),
		Probe:       probe,
		Title:       title,
		Artist:      artist,
		Uploader:    remoteInfo.Uploader,
		Channel:     remoteInfo.Channel,
		Track:       remoteInfo.Track,
		Album:       remoteInfo.Album,
		Notes:       []string{"manually cached"},
		Links:       []string{rawURL},
		LastUsedAt:  now,
	}
	if probe != nil {
		entry.LastProbeAt = now
	}

	idx.SetEntry(entry)
	idx.SetLink(rawURL, identifier)

	status("Saving index...")
	if err := Save(pp, idx); err != nil {
		return RegisterLocalFileResult{}, fmt.Errorf("save index: %w", err)
	}

	return RegisterLocalFileResult{Entry: entry, TargetPath: targetPath}, nil
}
