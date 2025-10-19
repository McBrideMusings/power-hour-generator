package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

	args = append(args, src.Raw)

	s.logf("yt-dlp row=%d source=%s", row.Index, src.Raw)
	_, runErr := s.Runner.Run(ctx, s.ytDLP, args, RunOptions{Stdout: logFile, Stderr: logFile})
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
