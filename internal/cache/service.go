package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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
	Paths       paths.ProjectPaths
	Logger      Logger
	Runner      Runner
	ytDLP       string
	ffprobe     string
	CookiesPath string
}

type ResolveOptions struct {
	Force   bool
	Reprobe bool
}

type ResolveStatus string

const (
	ResolveStatusCached     ResolveStatus = "cached"
	ResolveStatusDownloaded ResolveStatus = "downloaded"
	ResolveStatusCopied     ResolveStatus = "copied"
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
		Paths:       pp,
		Logger:      logger,
		Runner:      runner,
		ytDLP:       ytPath,
		ffprobe:     ffprobePath,
		CookiesPath: cookiesPath,
	}
	return svc, nil
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

	src, err := s.resolveSource(row)
	if err != nil {
		return ResolveResult{}, err
	}

	key := hashIdentifier(src.Identifier)
	baseName := fmt.Sprintf("%03d_%s", row.Index, key[:10])

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

	if !cached {
		if src.Type == SourceTypeURL {
			fetchRes, fetchErr := s.fetchURL(ctx, row, baseName, src)
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
			copyRes, copyErr := s.fetchLocal(ctx, row, baseName, src)
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
