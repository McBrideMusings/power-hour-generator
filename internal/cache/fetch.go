package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"powerhour/pkg/csvplan"
)

type fetchResult struct {
	Path      string
	SizeBytes int64
	ETag      string
	Notes     []string
}

func (s *Service) fetchURL(ctx context.Context, row csvplan.Row, baseName string, src sourceInfo) (fetchResult, error) {
	if err := os.MkdirAll(s.Paths.SrcDir, 0o755); err != nil {
		return fetchResult{}, fmt.Errorf("ensure src dir: %w", err)
	}
	if err := os.MkdirAll(s.Paths.LogsDir, 0o755); err != nil {
		return fetchResult{}, fmt.Errorf("ensure logs dir: %w", err)
	}

	logPath := filepath.Join(s.Paths.LogsDir, fmt.Sprintf("fetch_%03d.log", row.Index))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fetchResult{}, fmt.Errorf("open fetch log: %w", err)
	}
	defer logFile.Close()

	logWriter := s.logWriter(logFile)

	writeProxyBanner(ctx, logWriter, s.ytDLPProxy)

	pathFile, err := os.CreateTemp(s.Paths.LogsDir, "yt-dlp-path-*.txt")
	if err != nil {
		return fetchResult{}, fmt.Errorf("create yt-dlp path temp: %w", err)
	}
	pathFilePath := pathFile.Name()
	pathFile.Close()
	defer os.Remove(pathFilePath)

	template := filepath.Join(s.Paths.SrcDir, baseName+".%(ext)s")

	args := []string{
		"--no-playlist",
		"--no-progress",
		"--force-overwrites",
		"--output", template,
		"--print-to-file", "after_move:filepath", pathFilePath,
	}

	if s.CookiesPath != "" {
		args = append(args, "--cookies", s.CookiesPath)
	}
	if s.ytDLPProxy != "" {
		args = append(args, "--proxy", s.ytDLPProxy)
	}

	args = append(args, src.Raw)

	s.logf("yt-dlp row=%d source=%s", row.Index, src.Raw)
	_, runErr := s.Runner.Run(ctx, s.ytDLP, args, RunOptions{Stdout: logWriter, Stderr: logWriter})
	if runErr != nil {
		return fetchResult{}, fmt.Errorf("yt-dlp: %w (see %s)", runErr, logPath)
	}

	data, err := os.ReadFile(pathFilePath)
	if err != nil {
		return fetchResult{}, fmt.Errorf("read yt-dlp output file: %w", err)
	}

	targetPath := strings.TrimSpace(string(data))
	if targetPath == "" {
		return fetchResult{}, fmt.Errorf("yt-dlp did not report output path (see %s)", logPath)
	}

	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(s.Paths.SrcDir, targetPath)
	}

	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return fetchResult{}, fmt.Errorf("resolve downloaded path: %w", err)
	}
	targetPath = abs

	rel, err := filepath.Rel(s.Paths.SrcDir, targetPath)
	if err != nil {
		return fetchResult{}, fmt.Errorf("resolve downloaded relative path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return fetchResult{}, fmt.Errorf("downloaded path %q outside src dir", targetPath)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return fetchResult{}, fmt.Errorf("stat downloaded file: %w", err)
	}

	res := fetchResult{
		Path:      targetPath,
		SizeBytes: info.Size(),
		Notes:     []string{"downloaded via yt-dlp"},
	}
	return res, nil
}

type proxyLocation struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	Region  string `json:"region"`
	City    string `json:"city"`
}

var lookupProxyLocation = fetchProxyLocation

func writeProxyBanner(ctx context.Context, w io.Writer, proxy string) {
	if w == nil || strings.TrimSpace(proxy) == "" {
		return
	}

	fmt.Fprintf(w, "[powerhour] yt-dlp proxy: %s\n", proxy)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	loc, err := lookupProxyLocation(ctx, proxy)
	if err != nil {
		fmt.Fprintf(w, "[powerhour] proxy lookup failed: %v\n", err)
		return
	}

	descParts := make([]string, 0, 3)
	if loc.City != "" {
		descParts = append(descParts, loc.City)
	}
	if loc.Region != "" {
		descParts = append(descParts, loc.Region)
	}
	if loc.Country != "" {
		descParts = append(descParts, loc.Country)
	}
	desc := strings.Join(descParts, ", ")

	switch {
	case loc.IP != "" && desc != "":
		fmt.Fprintf(w, "[powerhour] proxy exit IP %s (%s)\n", loc.IP, desc)
	case loc.IP != "":
		fmt.Fprintf(w, "[powerhour] proxy exit IP %s\n", loc.IP)
	case desc != "":
		fmt.Fprintf(w, "[powerhour] proxy location %s\n", desc)
	default:
		fmt.Fprintf(w, "[powerhour] proxy location unknown\n")
	}
}

func fetchProxyLocation(ctx context.Context, proxy string) (proxyLocation, error) {
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return proxyLocation{}, fmt.Errorf("parse proxy url: %w", err)
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipwho.is", nil)
	if err != nil {
		return proxyLocation{}, fmt.Errorf("create proxy lookup request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return proxyLocation{}, fmt.Errorf("proxy lookup request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return proxyLocation{}, fmt.Errorf("proxy lookup unexpected status: %s", resp.Status)
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		IP      string `json:"ip"`
		Country string `json:"country"`
		Region  string `json:"region"`
		City    string `json:"city"`
	}

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return proxyLocation{}, fmt.Errorf("decode proxy lookup response: %w", err)
	}

	if !payload.Success && payload.Message != "" {
		return proxyLocation{}, fmt.Errorf("proxy lookup error: %s", payload.Message)
	}

	loc := proxyLocation{
		IP:      payload.IP,
		Country: payload.Country,
		Region:  payload.Region,
		City:    payload.City,
	}
	return loc, nil
}

func (s *Service) logWriter(base io.Writer) io.Writer {
	if base == nil {
		base = io.Discard
	}
	if s == nil || s.logOutput == nil {
		return base
	}
	if base == s.logOutput {
		return base
	}
	return io.MultiWriter(base, s.logOutput)
}

func (s *Service) fetchLocal(_ context.Context, row csvplan.Row, baseName string, src sourceInfo) (fetchResult, error) {
	if err := os.MkdirAll(s.Paths.SrcDir, 0o755); err != nil {
		return fetchResult{}, fmt.Errorf("ensure src dir: %w", err)
	}

	ext := filepath.Ext(src.LocalPath)
	targetPath := filepath.Join(s.Paths.SrcDir, baseName+ext)

	s.logf("cache copy row=%d source=%s target=%s", row.Index, src.LocalPath, targetPath)

	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fetchResult{}, fmt.Errorf("remove existing cache file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fetchResult{}, fmt.Errorf("ensure target dir: %w", err)
	}

	hardLinked, err := tryLinkOrCopy(src.LocalPath, targetPath)
	if err != nil {
		return fetchResult{}, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return fetchResult{}, fmt.Errorf("stat cached copy: %w", err)
	}

	note := "copied from %s"
	if hardLinked {
		note = "hardlinked from %s"
	}

	res := fetchResult{
		Path:      targetPath,
		SizeBytes: info.Size(),
		Notes:     []string{fmt.Sprintf(note, src.LocalPath)},
	}
	return res, nil
}

func tryLinkOrCopy(src, dest string) (bool, error) {
	if err := os.Link(src, dest); err == nil {
		return true, nil
	}
	if err := copyFile(src, dest); err != nil {
		return false, err
	}
	return false, nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp dest: %w", err)
	}

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("copy data: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("close temp dest: %w", err)
	}

	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("chmod temp dest: %w", err)
	}

	if err := os.Rename(tmp.Name(), dest); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("rename temp dest: %w", err)
	}
	return nil
}
