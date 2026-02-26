package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/tools"
	"powerhour/pkg/csvplan"
)

type Logger interface {
	Printf(format string, v ...any)
}

type noopLogger struct{}

func (noopLogger) Printf(string, ...any) {}

func (s *Service) logf(format string, v ...any) {
	if s == nil || s.Logger == nil {
		return
	}
	s.Logger.Printf(format, v...)
}

type Service struct {
	Paths            paths.ProjectPaths
	Logger           Logger
	Runner           Runner
	ytDLP            string
	ffprobe          string
	CookiesPath      string
	ytDLPProxy       string
	logOutput        io.Writer
	filenameTemplate string
}

type ResolveOptions struct {
	Force      bool
	Reprobe    bool
	NoDownload bool
}

type ResolveStatus string

const (
	ResolveStatusCached     ResolveStatus = "cached"
	ResolveStatusDownloaded ResolveStatus = "downloaded"
	ResolveStatusCopied     ResolveStatus = "copied"
	ResolveStatusMatched    ResolveStatus = "matched"
	ResolveStatusMissing    ResolveStatus = "missing"
)

type ResolveResult struct {
	Entry      Entry
	Status     ResolveStatus
	Probed     bool
	Updated    bool
	ID         string
	Identifier string
}

type sourceInfo struct {
	Raw        string
	Type       SourceType
	Identifier string
	LocalPath  string
	ID         string
	Extractor  string

	// yt-dlp metadata
	Title       string
	Artist      string
	Album       string
	Track       string
	Uploader    string
	Channel     string
	UploadDate  string
	Description string
}

// LocalSourceMissingError is returned when a local file reference doesn't exist.
// This is distinct from other errors because local files aren't "fetched" —
// they're just validated, so a missing local file is a warning, not a failure.
type LocalSourceMissingError struct {
	Path string
}

func (e *LocalSourceMissingError) Error() string {
	return fmt.Sprintf("local source not found: %s", e.Path)
}

type filenameParts struct {
	Remote string
	Local  string
}

var nowFunc = time.Now

func NewService(ctx context.Context, pp paths.ProjectPaths, logger Logger, runner Runner) (*Service, error) {
	return NewServiceWithStatus(ctx, pp, logger, runner, nil)
}

// NewServiceWithStatus is like NewService but accepts a StatusFunc callback
// to report per-tool progress during tool detection and installation.
func NewServiceWithStatus(ctx context.Context, pp paths.ProjectPaths, logger Logger, runner Runner, statusFn tools.StatusFunc) (*Service, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = noopLogger{}
	}
	if runner == nil {
		runner = CmdRunner{}
	}
	if err := pp.EnsureMetaDirs(); err != nil {
		return nil, err
	}

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return nil, err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())
	if err := os.MkdirAll(pp.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	ctx = tools.WithMinimums(ctx, cfg.ToolMinimums())

	cookiesPath := ""
	if exists, _ := paths.FileExists(pp.CookiesFile); exists {
		cookiesPath = pp.CookiesFile
		logger.Printf("using cookies file: %s", cookiesPath)
	}
	ytProxy := cfg.ToolProxy("yt-dlp")

	toolStatuses, err := tools.EnsureAll(ctx, []string{"yt-dlp", "ffmpeg"}, statusFn)
	if err != nil {
		return nil, err
	}

	ytStatus := toolStatuses["yt-dlp"]
	ytPath := firstNonEmpty(ytStatus.Path, ytStatus.Paths["yt-dlp"])
	if ytPath == "" {
		return nil, errors.New("yt-dlp path not resolved")
	}

	ffStatus := toolStatuses["ffmpeg"]
	ffprobePath := ffStatus.Paths["ffprobe"]
	if ffprobePath == "" {
		return nil, errors.New("ffprobe path not recorded in manifest")
	}

	svc := &Service{
		Paths:            pp,
		Logger:           logger,
		Runner:           runner,
		ytDLP:            ytPath,
		ffprobe:          ffprobePath,
		CookiesPath:      cookiesPath,
		ytDLPProxy:       ytProxy,
		filenameTemplate: cfg.DownloadFilenameTemplate(),
	}
	return svc, nil
}

// SetLogOutput configures a secondary writer for fetch logs.
func (s *Service) SetLogOutput(w io.Writer) {
	if s == nil {
		return
	}
	s.logOutput = w
}

func (s *Service) templateString() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.filenameTemplate)
}

func (s *Service) Resolve(ctx context.Context, idx *Index, row csvplan.Row, opts ResolveOptions) (ResolveResult, error) {
	if s == nil {
		return ResolveResult{}, errors.New("cache service is nil")
	}
	if idx == nil {
		return ResolveResult{}, errors.New("cache index is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	src, err := s.resolveSource(ctx, idx, row, opts.Force)
	if err != nil {
		var localMissing *LocalSourceMissingError
		if errors.As(err, &localMissing) {
			return ResolveResult{
				Status:     ResolveStatusMissing,
				Identifier: localMissing.Path,
				Entry:      Entry{Source: localMissing.Path, SourceType: SourceTypeLocal},
			}, nil
		}
		return ResolveResult{}, err
	}
	key := hashIdentifier(src.Identifier)
	names := s.buildFilenameParts(row, src, key)

	var (
		linkKeyBefore string
		linkKnown     bool
	)
	if src.Type == SourceTypeURL {
		linkKeyBefore, linkKnown = idx.LookupLink(row.Link)
	}

	existing, ok := idx.GetByIdentifier(src.Identifier)
	now := nowFunc().UTC()
	result := ResolveResult{}
	entry := existing
	entry.Key = key
	entry.Identifier = src.Identifier
	entry.SourceType = src.Type
	entry.Source = sourceDisplayValue(src)
	if src.Type == SourceTypeURL {
		if src.ID != "" {
			entry.ID = src.ID
		}
		if src.Extractor != "" {
			entry.Extractor = src.Extractor
		}
		entry.Links = appendUnique(entry.Links, src.Raw)
		// Populate yt-dlp metadata (prefer artist over uploader, track over title)
		if src.Title != "" {
			entry.Title = src.Title
		}
		entry.Artist = resolveArtist(src)
		if src.Track != "" {
			entry.Track = src.Track
		}
		if src.Album != "" {
			entry.Album = src.Album
		}
		if src.UploadDate != "" {
			entry.UploadDate = src.UploadDate
		}
		if src.Description != "" {
			entry.Description = src.Description
		}
	} else {
		entry.ID = ""
		entry.Extractor = ""
		entry.Links = nil
	}
	entry.LastUsedAt = now
	result.Identifier = entry.Identifier
	result.ID = entry.ID

	metaChanged := hasMetadataChanged(existing, entry)

	cached := false
	if ok && !opts.Force && existing.CachedPath != "" && fileExists(existing.CachedPath) {
		cached = true
		result.Status = ResolveStatusCached
		entry.CachedPath = existing.CachedPath
		entry.SizeBytes = existing.SizeBytes
		entry.RetrievedAt = existing.RetrievedAt
		entry.Notes = existing.Notes
		entry.LastProbeAt = existing.LastProbeAt
		entry.Probe = existing.Probe
	}

	// Stale path recovery: if the index has an entry but the file moved (e.g.
	// project directory was relocated), check the current cache dir for a file
	// with the same basename before falling through to re-download.
	if !cached && !opts.Force && ok && existing.CachedPath != "" {
		if recovered, info := s.recoverStaleEntry(existing.CachedPath); recovered != "" {
			entry.CachedPath = recovered
			entry.SizeBytes = info.Size()
			entry.RetrievedAt = existing.RetrievedAt
			entry.LastProbeAt = existing.LastProbeAt
			entry.Probe = existing.Probe
			entry.Notes = appendUnique(existing.Notes, "recovered stale cached_path")
			result.Status = ResolveStatusCached
			result.Updated = true
			cached = true
		}
	}

	if !cached && !opts.Force {
		if expectedBase, err := s.ExpectedFilenameBase(row, entry); err == nil {
			if matchPath, matchInfo := s.locateCachedFile(expectedBase); matchPath != "" {
				entry.CachedPath = matchPath
				entry.SizeBytes = matchInfo.Size()
				entry.RetrievedAt = now
				entry.Notes = appendUnique(entry.Notes, "matched existing cache file")
				result.Status = ResolveStatusMatched
				result.Updated = true
				cached = true
			}
		}
	}

	if !cached && opts.NoDownload {
		result.Status = ResolveStatusMissing
		if src.Type == SourceTypeURL {
			idx.SetLink(row.Link, src.Identifier)
			if !linkKnown || linkKeyBefore != src.Identifier {
				result.Updated = true
			}
		}
		if entry.Identifier != "" {
			idx.DeleteEntry(entry.Identifier)
		}
		result.Entry = entry
		return result, nil
	}

	if !cached {
		if src.Type == SourceTypeURL {
			fetchRes, fetchErr := s.fetchURL(ctx, row, names.Remote, src)
			if fetchErr != nil {
				return ResolveResult{}, fetchErr
			}
			entry.CachedPath = fetchRes.Path
			entry.SizeBytes = fetchRes.SizeBytes
			entry.ETag = fetchRes.ETag
			entry.RetrievedAt = now
			entry.Notes = fetchRes.Notes
			result.Status = ResolveStatusDownloaded
		} else {
			copyRes, copyErr := s.fetchLocal(ctx, row, names.Local, src)
			if copyErr != nil {
				return ResolveResult{}, copyErr
			}
			entry.CachedPath = copyRes.Path
			entry.SizeBytes = copyRes.SizeBytes
			entry.RetrievedAt = now
			entry.Notes = copyRes.Notes
			result.Status = ResolveStatusCopied
		}
		result.Updated = true
	}

	needProbe := entry.CachedPath != "" && (!cached || opts.Reprobe || entry.Probe == nil)
	if needProbe {
		probeMeta, probeErr := s.probe(ctx, row, entry.CachedPath)
		if probeErr != nil {
			return ResolveResult{}, probeErr
		}
		entry.LastProbeAt = now
		entry.Probe = probeMeta
		result.Probed = true
		result.Updated = true
	}

	linkChanged := src.Type == SourceTypeURL && (!linkKnown || linkKeyBefore != src.Identifier)
	if metaChanged || linkChanged {
		result.Updated = true
	}

	if strings.TrimSpace(entry.CachedPath) != "" {
		idx.SetEntry(entry)
	} else {
		idx.DeleteEntry(entry.Identifier)
	}
	if src.Type == SourceTypeURL {
		idx.SetLink(row.Link, src.Identifier)
	}
	result.Entry = entry
	result.ID = entry.ID
	result.Identifier = entry.Identifier
	if result.Status == "" {
		result.Status = ResolveStatusCached
	}
	return result, nil
}

func (s *Service) buildFilenameParts(row csvplan.Row, src sourceInfo, key string) filenameParts {
	template := s.templateString()
	if template == "" {
		template = "$ID"
	}

	shortHash := truncateHash(key, 10)
	fallback := fmt.Sprintf("%03d_%s", row.Index, shortHash)

	remoteValues, localValues := filenameTemplateValues(row, src, key, shortHash)

	if id := sanitizeSegment(src.ID); id != "" {
		remoteValues["ID"] = id
		remoteValues["REMOTE_ID"] = id
	}

	remote := cleanupFilename(applyFilenameTemplate(template, remoteValues))
	if remote == "" {
		remote = fallback
	}

	local := cleanupFilename(applyFilenameTemplate(template, localValues))
	if local == "" {
		local = fallback
	}

	return filenameParts{
		Remote: remote,
		Local:  local,
	}
}

// ExpectedFilenameBase returns the sanitized base name that should be used for the cached file.
func (s *Service) ExpectedFilenameBase(row csvplan.Row, entry Entry) (string, error) {
	if s == nil {
		return "", errors.New("cache service is nil")
	}
	template := s.templateString()
	if template == "" {
		template = "$ID"
	}

	identifier := strings.TrimSpace(entry.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(row.Link)
		if identifier == "" {
			identifier = fmt.Sprintf("row:%03d", row.Index)
		}
	}
	key := hashIdentifier(identifier)
	src := sourceInfoFromEntry(row, entry, identifier)

	shortHash := truncateHash(key, 10)
	remoteVals, localVals := filenameTemplateValues(row, src, key, shortHash)

	var values map[string]string
	switch entry.SourceType {
	case SourceTypeLocal:
		values = localVals
	default:
		values = remoteVals
		if id := resolveRemoteID(src, entry, shortHash); id != "" {
			values["ID"] = sanitizeSegment(id)
		}
	}

	base := cleanupFilename(applyFilenameTemplate(template, values))
	if base == "" {
		base = fmt.Sprintf("%03d_%s", row.Index, shortHash)
	}
	return base, nil
}

func (s *Service) resolveSource(ctx context.Context, idx *Index, row csvplan.Row, force bool) (sourceInfo, error) {
	raw := strings.TrimSpace(row.Link)
	if raw == "" {
		return sourceInfo{}, fmt.Errorf("row %d missing link", row.Index)
	}

	if looksLikeURL(raw) {
		info, err := s.resolveRemoteSource(ctx, idx, raw, force)
		if err != nil {
			return sourceInfo{}, err
		}
		info.Raw = raw
		info.Type = SourceTypeURL
		return info, nil
	}

	path := raw
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Paths.Root, raw)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return sourceInfo{}, fmt.Errorf("resolve path %q: %w", raw, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return sourceInfo{}, &LocalSourceMissingError{Path: abs}
	}
	return sourceInfo{
		Raw:        raw,
		Type:       SourceTypeLocal,
		Identifier: abs,
		LocalPath:  abs,
	}, nil
}

func (s *Service) resolveRemoteSource(ctx context.Context, idx *Index, link string, force bool) (sourceInfo, error) {
	if idx != nil && !force {
		if existing, ok := idx.LookupLink(link); ok && strings.TrimSpace(existing) != "" {
			extractor, id := splitCanonicalIdentifier(existing)
			return sourceInfo{
				Identifier: existing,
				ID:         id,
				Extractor:  extractor,
			}, nil
		}
	}

	info, err := s.queryRemoteID(ctx, link)
	if err != nil {
		return sourceInfo{}, err
	}

	identifier := canonicalRemoteIdentifier(link, info.Extractor, info.ID)
	return sourceInfo{
		Identifier:  identifier,
		ID:          strings.TrimSpace(info.ID),
		Extractor:   strings.TrimSpace(info.Extractor),
		Title:       info.Title,
		Artist:      info.Artist,
		Album:       info.Album,
		Track:       info.Track,
		Uploader:    info.Uploader,
		Channel:     info.Channel,
		UploadDate:  info.UploadDate,
		Description: info.Description,
	}, nil
}

type remoteIDInfo struct {
	ID          string
	Extractor   string
	Title       string
	Artist      string
	Album       string
	Track       string
	Uploader    string
	Channel     string
	UploadDate  string
	Description string
}

func (s *Service) queryRemoteID(ctx context.Context, link string) (remoteIDInfo, error) {
	if s == nil {
		return remoteIDInfo{}, errors.New("cache service is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	args := []string{
		"--no-playlist",
		"--no-progress",
		"--skip-download",
		"--dump-json",
		"--no-warnings",
		"--no-color",
	}
	if s.CookiesPath != "" {
		args = append(args, "--cookies", s.CookiesPath)
	}
	if s.ytDLPProxy != "" {
		args = append(args, "--proxy", s.ytDLPProxy)
	}

	args = append(args, link)

	res, err := s.Runner.Run(ctx, s.ytDLP, args, RunOptions{Dir: s.Paths.Root})
	if err != nil {
		return remoteIDInfo{}, fmt.Errorf("yt-dlp id probe: %w", err)
	}

	raw := bytes.TrimSpace(res.Stdout)
	if len(raw) == 0 {
		return remoteIDInfo{}, errors.New("yt-dlp id probe returned empty output")
	}

	var payload struct {
		ID           string `json:"id"`
		Extractor    string `json:"extractor_key"`
		ExtractorAlt string `json:"extractor"`
		Title        string `json:"title"`
		Artist       string `json:"artist"`
		Track        string `json:"track"`
		Album        string `json:"album"`
		Uploader     string `json:"uploader"`
		Channel      string `json:"channel"`
		UploadDate   string `json:"upload_date"`
		Description  string `json:"description"`
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&payload); err != nil {
		firstLine := raw
		if idx := bytes.IndexByte(raw, '\n'); idx >= 0 {
			firstLine = raw[:idx]
		}
		if err := json.Unmarshal(firstLine, &payload); err != nil {
			return remoteIDInfo{}, fmt.Errorf("parse yt-dlp id response: %w", err)
		}
	}

	extractor := strings.TrimSpace(payload.Extractor)
	if extractor == "" {
		extractor = strings.TrimSpace(payload.ExtractorAlt)
	}
	extractor = strings.ToLower(extractor)
	if extractor == "" {
		extractor = "unknown"
	}

	desc := strings.TrimSpace(payload.Description)
	if len(desc) > 500 {
		desc = desc[:500]
	}

	return remoteIDInfo{
		ID:          strings.TrimSpace(payload.ID),
		Extractor:   extractor,
		Title:       strings.TrimSpace(payload.Title),
		Artist:      strings.TrimSpace(payload.Artist),
		Album:       strings.TrimSpace(payload.Album),
		Track:       strings.TrimSpace(payload.Track),
		Uploader:    strings.TrimSpace(payload.Uploader),
		Channel:     strings.TrimSpace(payload.Channel),
		UploadDate:  strings.TrimSpace(payload.UploadDate),
		Description: desc,
	}, nil
}

func canonicalRemoteIdentifier(link, extractor, id string) string {
	id = strings.TrimSpace(id)
	extractor = strings.TrimSpace(extractor)
	if extractor == "" {
		extractor = "unknown"
	}
	if id != "" {
		return fmt.Sprintf("%s:%s", extractor, id)
	}
	return fmt.Sprintf("urlhash:%s", hashIdentifier(link))
}

func splitCanonicalIdentifier(identifier string) (string, string) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return "", ""
	}
	parts := strings.SplitN(identifier, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", identifier
}

func sourceInfoFromEntry(row csvplan.Row, entry Entry, identifier string) sourceInfo {
	raw := strings.TrimSpace(row.Link)
	if raw == "" {
		raw = strings.TrimSpace(entry.Source)
	}
	info := sourceInfo{
		Raw:        raw,
		Type:       entry.SourceType,
		Identifier: identifier,
		ID:         strings.TrimSpace(entry.ID),
		Extractor:  strings.TrimSpace(entry.Extractor),
	}
	if entry.SourceType == SourceTypeLocal {
		path := strings.TrimSpace(entry.Source)
		if path == "" {
			path = identifier
		}
		info.LocalPath = path
	}
	return info
}

// resolveArtist picks the best artist value from yt-dlp metadata.
// Priority: artist → uploader → channel.
func resolveArtist(src sourceInfo) string {
	if src.Artist != "" {
		return src.Artist
	}
	if src.Uploader != "" {
		return src.Uploader
	}
	return src.Channel
}

func sourceDisplayValue(src sourceInfo) string {
	if src.Type == SourceTypeLocal {
		if src.LocalPath != "" {
			return src.LocalPath
		}
	}
	return src.Raw
}

func appendUnique(values []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), candidate) {
			return values
		}
	}
	return append(values, candidate)
}

func hasMetadataChanged(existing, updated Entry) bool {
	if strings.TrimSpace(existing.Identifier) != strings.TrimSpace(updated.Identifier) {
		return true
	}
	if existing.SourceType != updated.SourceType {
		return true
	}
	if strings.TrimSpace(existing.Source) != strings.TrimSpace(updated.Source) {
		return true
	}
	if strings.TrimSpace(existing.ID) != strings.TrimSpace(updated.ID) {
		return true
	}
	if strings.TrimSpace(existing.Extractor) != strings.TrimSpace(updated.Extractor) {
		return true
	}
	if len(existing.Links) != len(updated.Links) {
		return true
	}
	for i := range existing.Links {
		if strings.TrimSpace(existing.Links[i]) != strings.TrimSpace(updated.Links[i]) {
			return true
		}
	}
	return false
}

func (s *Service) locateCachedFile(base string) (string, os.FileInfo) {
	base = strings.TrimSpace(base)
	if base == "" || s == nil {
		return "", nil
	}
	for _, dir := range s.candidateCacheDirs() {
		exact := filepath.Join(dir, base)
		if info, err := os.Stat(exact); err == nil && info.Mode().IsRegular() {
			return exact, info
		}

		pattern := filepath.Join(dir, base+".*")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			if info.Mode().IsRegular() {
				return match, info
			}
		}
	}
	return "", nil
}

// recoverStaleEntry checks the current cache directory for a file matching the
// basename of a stale cached_path. This handles cases where the project was
// moved or the cache directory changed.
func (s *Service) recoverStaleEntry(stalePath string) (string, os.FileInfo) {
	if s == nil || stalePath == "" {
		return "", nil
	}
	base := filepath.Base(stalePath)
	for _, dir := range s.candidateCacheDirs() {
		candidate := filepath.Join(dir, base)
		if candidate == stalePath {
			continue // same path, already failed
		}
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, info
		}
	}
	return "", nil
}

func (s *Service) candidateCacheDirs() []string {
	if s == nil {
		return nil
	}
	dir := strings.TrimSpace(s.Paths.CacheDir)
	if dir == "" {
		return nil
	}
	return []string{dir}
}

func looksLikeURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return true
	}
	return false
}

func filenameTemplateValues(row csvplan.Row, src sourceInfo, key, shortHash string) (map[string]string, map[string]string) {
	duration := ""
	if row.DurationSeconds > 0 {
		duration = strconv.Itoa(row.DurationSeconds)
	}

	indexPadded := fmt.Sprintf("%03d", row.Index)
	indexRaw := strconv.Itoa(row.Index)

	title := sanitizeSegment(row.Title)
	artist := sanitizeSegment(row.Artist)
	name := sanitizeSegment(row.Name)
	start := sanitizeSegment(row.StartRaw)

	host := ""
	if src.Type == SourceTypeURL {
		if parsed, err := url.Parse(src.Raw); err == nil {
			host = parsed.Hostname()
		}
	}

	sourceID := sanitizeSegment(src.Identifier)
	canonicalID := sourceID
	if canonicalID == "" {
		canonicalID = sanitizeSegment(key)
	}

	common := map[string]string{
		"INDEX":         indexPadded,
		"INDEX_PAD3":    indexPadded,
		"INDEX_RAW":     indexRaw,
		"HASH":          key,
		"HASH10":        shortHash,
		"KEY":           key,
		"KEY10":         shortHash,
		"TITLE":         title,
		"ARTIST":        artist,
		"NAME":          name,
		"START":         start,
		"DURATION":      duration,
		"SOURCE_HOST":   sanitizeSegment(host),
		"SOURCE_ID":     sourceID,
		"ROW_ID":        indexRaw,
		"PLAN_TITLE":    title,
		"PLAN_ARTIST":   artist,
		"PLAN_NAME":     name,
		"PLAN_START":    start,
		"PLAN_DURATION": duration,
	}
	common["CANONICAL_ID"] = canonicalID

	if remoteID := sanitizeSegment(src.ID); remoteID != "" {
		common["REMOTE_ID"] = remoteID
	}
	if extractor := sanitizeSegment(src.Extractor); extractor != "" {
		common["SOURCE_EXTRACTOR"] = extractor
	}

	remote := cloneTemplateValues(common)
	remote["ID"] = "%(id)s"
	if remoteID := sanitizeSegment(src.ID); remoteID != "" {
		remote["REMOTE_ID"] = remoteID
	}

	local := cloneTemplateValues(common)
	localID := localIdentifier(src, shortHash)
	local["ID"] = localID

	return remote, local
}

func cloneTemplateValues(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func localIdentifier(src sourceInfo, shortHash string) string {
	if src.Type == SourceTypeLocal {
		base := filepath.Base(src.LocalPath)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		if seg := sanitizeSegment(name); seg != "" {
			return seg
		}
	}
	if seg := sanitizeSegment(shortHash); seg != "" {
		return seg
	}
	return "source"
}

func resolveRemoteID(src sourceInfo, entry Entry, shortHash string) string {
	if src.Type != SourceTypeURL {
		return ""
	}

	if id := strings.TrimSpace(src.ID); id != "" {
		return id
	}
	if id := strings.TrimSpace(entry.ID); id != "" {
		return id
	}

	if _, id := splitCanonicalIdentifier(src.Identifier); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}

	if id := extractYouTubeID(src.Raw); id != "" {
		return id
	}
	if id := extractYouTubeID(entry.Source); id != "" {
		return id
	}
	for _, link := range entry.Links {
		if id := extractYouTubeID(link); id != "" {
			return id
		}
	}

	if entry.CachedPath != "" {
		base := strings.TrimSuffix(filepath.Base(entry.CachedPath), filepath.Ext(entry.CachedPath))
		if seg := sanitizeSegment(base); seg != "" {
			return seg
		}
	}

	return shortHash
}

func extractYouTubeID(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	host := strings.ToLower(u.Hostname())
	switch host {
	case "www.youtube.com", "youtube.com", "m.youtube.com", "music.youtube.com":
		if v := strings.TrimSpace(u.Query().Get("v")); v != "" {
			return v
		}
		// handle /shorts/<id> or /embed/<id> etc
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 {
			switch parts[0] {
			case "embed", "v", "shorts":
				return parts[1]
			}
		}
	case "youtu.be":
		segments := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(segments) > 0 && segments[0] != "" {
			return segments[0]
		}
	}
	return ""
}

func applyFilenameTemplate(template string, values map[string]string) string {
	var builder strings.Builder
	for i := 0; i < len(template); {
		ch := template[i]
		if ch != '$' {
			builder.WriteByte(ch)
			i++
			continue
		}

		if i+1 < len(template) && template[i+1] == '$' {
			builder.WriteByte('$')
			i += 2
			continue
		}

		j := i + 1
		for j < len(template) {
			c := template[j]
			switch {
			case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
				j++
				continue
			case c == '_':
				if j+1 < len(template) {
					next := template[j+1]
					if (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') {
						j++
						continue
					}
				}
				fallthrough
			default:
				break
			}
			break
		}

		if j == i+1 {
			builder.WriteByte('$')
			i++
			continue
		}

		token := template[i+1 : j]
		if val, ok := values[token]; ok {
			builder.WriteString(val)
		}
		i = j
	}
	return builder.String()
}

func cleanupFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, ":", "_")
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	value = strings.Trim(value, " .-")
	return value
}

func sanitizeSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		case r == '-' || r == '.':
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	result := builder.String()
	result = strings.Trim(result, ".-")
	if len(result) > 150 {
		result = result[:150]
	}
	return result
}

func truncateHash(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[:n]
}

func hashIdentifier(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
