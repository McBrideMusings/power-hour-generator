package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Force   bool
	Reprobe bool
	DryRun  bool
}

type ResolveStatus string

const (
	ResolveStatusCached        ResolveStatus = "cached"
	ResolveStatusDownloaded    ResolveStatus = "downloaded"
	ResolveStatusCopied        ResolveStatus = "copied"
	ResolveStatusWouldDownload ResolveStatus = "would-download"
	ResolveStatusWouldCopy     ResolveStatus = "would-copy"
)

type ResolveResult struct {
	Entry   Entry
	Status  ResolveStatus
	Probed  bool
	Updated bool
}

type sourceInfo struct {
	Raw        string
	Type       SourceType
	Identifier string
	LocalPath  string
}

type filenameParts struct {
	Remote string
	Local  string
}

var nowFunc = time.Now

func NewService(ctx context.Context, pp paths.ProjectPaths, logger Logger, runner Runner) (*Service, error) {
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
	ctx = tools.WithMinimums(ctx, cfg.ToolMinimums())

	cookiesPath := ""
	if exists, _ := paths.FileExists(pp.CookiesFile); exists {
		cookiesPath = pp.CookiesFile
		logger.Printf("using cookies file: %s", cookiesPath)
	}
	ytProxy := cfg.ToolProxy("yt-dlp")

	ytStatus, err := tools.Ensure(ctx, "yt-dlp")
	if err != nil {
		return nil, fmt.Errorf("ensure yt-dlp: %w", err)
	}
	ytPath := firstNonEmpty(ytStatus.Path, ytStatus.Paths["yt-dlp"])
	if ytPath == "" {
		return nil, errors.New("yt-dlp path not resolved")
	}

	ffStatus, err := tools.Ensure(ctx, "ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ensure ffmpeg: %w", err)
	}
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

	src, key, names, err := s.resolveRow(row)
	if err != nil {
		return ResolveResult{}, err
	}

	existing, ok := idx.Get(row.Index)
	now := nowFunc().UTC()
	result := ResolveResult{}
	entry := existing
	entry.RowIndex = row.Index
	entry.Key = key
	entry.Source = src.Identifier
	entry.SourceType = src.Type

	cached := false
	if ok && !opts.Force && existing.Key == key && existing.SourceType == src.Type {
		if existing.CachedPath != "" && fileExists(existing.CachedPath) {
			cached = true
			result.Status = ResolveStatusCached
			entry = existing
		}
	}

	if opts.DryRun {
		if !cached {
			if src.Type == SourceTypeURL {
				result.Status = ResolveStatusWouldDownload
			} else {
				result.Status = ResolveStatusWouldCopy
			}
		}
		result.Entry = entry
		if result.Status == "" {
			result.Status = ResolveStatusCached
		}
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

	idx.Set(entry)
	result.Entry = entry
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

func (s *Service) resolveRow(row csvplan.Row) (sourceInfo, string, filenameParts, error) {
	src, err := s.resolveSource(row)
	if err != nil {
		return sourceInfo{}, "", filenameParts{}, err
	}
	key := hashIdentifier(src.Identifier)
	names := s.buildFilenameParts(row, src, key)
	return src, key, names, nil
}

// ExpectedFilenameParts returns the template-derived base names for a plan row.
func (s *Service) ExpectedFilenameParts(row csvplan.Row) (filenameParts, sourceInfo, error) {
	src, _, names, err := s.resolveRow(row)
	if err != nil {
		return filenameParts{}, sourceInfo{}, err
	}
	return names, src, nil
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

	src, key, _, err := s.resolveRow(row)
	if err != nil {
		return "", err
	}

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

func (s *Service) resolveSource(row csvplan.Row) (sourceInfo, error) {
	raw := strings.TrimSpace(row.Link)
	if raw == "" {
		return sourceInfo{}, fmt.Errorf("row %d missing link", row.Index)
	}

	if looksLikeURL(raw) {
		return sourceInfo{Raw: raw, Type: SourceTypeURL, Identifier: raw}, nil
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
		return sourceInfo{}, fmt.Errorf("stat local source %q: %w", abs, err)
	}
	return sourceInfo{
		Raw:        raw,
		Type:       SourceTypeLocal,
		Identifier: abs,
		LocalPath:  abs,
	}, nil
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

	remote := cloneTemplateValues(common)
	remote["ID"] = "%(id)s"

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

	if id := extractYouTubeID(src.Identifier); id != "" {
		return id
	}
	if id := extractYouTubeID(entry.Source); id != "" {
		return id
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
	value = strings.Trim(value, " _-.")
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
	result = strings.Trim(result, "_.-")
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
