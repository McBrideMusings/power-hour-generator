package dashboard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"powerhour/internal/tools"
	"powerhour/internal/tui"
)

// isURL returns true if the string looks like a URL (http, https, or youtube shortlink).
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "youtu")
}

// videoFileExtensions lists extensions treated as playable video sources
// for the Add Clip slot's local-file path.
var videoFileExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".webm": true, ".avi": true, ".mov": true,
	".m4v": true, ".flv": true, ".wmv": true, ".mpg": true, ".mpeg": true,
	".ts": true, ".m2ts": true,
}

// isLocalVideoFile reports whether s is a path to an existing, readable video
// file on disk (not a URL). Used to let the Add Clip slot accept any local
// video path — not just ones under the project root — without routing it
// through the cache-suggestion search.
func isLocalVideoFile(s string) bool {
	if s == "" || isURL(s) {
		return false
	}
	if !videoFileExtensions[strings.ToLower(filepath.Ext(s))] {
		return false
	}
	info, err := os.Stat(s)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

// probeLocalVideoFile runs ffprobe against path and reports whether it
// contains a decodable video stream. isLocalVideoFile only checks the
// extension, which passes a renamed non-video file; this catches that case
// at add time instead of at render time. Bounded by a timeout since path may
// be on a slow network mount.
func probeLocalVideoFile(path string) error {
	ffprobePath, err := tools.Lookup("ffprobe")
	if err != nil {
		return nil // tool not detected yet; don't block add on this
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_type",
		"-of", "csv=p=0",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("not a readable video file: %w", err)
	}
	if !strings.Contains(string(out), "video") {
		return fmt.Errorf("file has no video stream")
	}
	return nil
}

func truncateCollectionValue(value string, max int) string {
	value = strings.TrimSpace(value)
	if value == "" || max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	if !looksLikeFilesystemPath(value) || isURL(value) {
		return tui.TruncateWithEllipsis(value, max)
	}

	lastSep := strings.LastIndexAny(value, `/\`)
	if lastSep < 0 || lastSep >= len(value)-1 {
		return tui.TruncateWithEllipsis(value, max)
	}

	dir := value[:lastSep+1]
	base := value[lastSep+1:]
	if utf8.RuneCountInString(base) >= max {
		return tui.TruncateWithEllipsis(base, max)
	}

	remaining := max - utf8.RuneCountInString(base)
	if utf8.RuneCountInString(dir) <= remaining {
		return dir + base
	}
	if remaining <= 3 {
		return tui.TruncateWithEllipsis(base, max)
	}

	suffix := trailingPathSuffix(dir, remaining-3)
	return "..." + suffix + base
}

func looksLikeFilesystemPath(s string) bool {
	if strings.ContainsAny(s, `/\`) {
		return true
	}
	return strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

func trailingPathSuffix(dir string, max int) string {
	if max <= 0 || dir == "" {
		return ""
	}
	if len(dir) <= max {
		return dir
	}

	parts := strings.FieldsFunc(dir, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		if max > len(dir) {
			return dir
		}
		return dir[len(dir)-max:]
	}

	suffix := string(dir[len(dir)-1])
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := string(dir[len(dir)-1]) + parts[i] + suffix
		if len(candidate) > max {
			break
		}
		suffix = candidate
	}
	if len(suffix) > max {
		return suffix[len(suffix)-max:]
	}
	return suffix
}
