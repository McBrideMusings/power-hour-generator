package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ToolInfo captures availability and version details for an external tool.
type ToolInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// Probe discovers tool availability and version information.
func Probe(ctx context.Context) map[string]ToolInfo {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	names := []string{"yt-dlp", "ffmpeg", "ffprobe"}
	result := make(map[string]ToolInfo, len(names))
	for _, name := range names {
		info := probeOne(ctx, name)
		result[name] = info
	}
	return result
}

func probeOne(ctx context.Context, name string) ToolInfo {
	path, err := exec.LookPath(name)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ToolInfo{Name: name, Available: false, Error: "not found"}
		}
		return ToolInfo{Name: name, Available: false, Error: err.Error()}
	}

	version, err := readVersion(ctx, path, name)
	if err != nil {
		return ToolInfo{Name: name, Path: path, Available: true, Error: err.Error()}
	}

	return ToolInfo{Name: name, Path: path, Version: version, Available: true}
}

func readVersion(ctx context.Context, path, name string) (string, error) {
	var args []string
	switch name {
	case "yt-dlp":
		args = []string{"--version"}
	case "ffmpeg", "ffprobe":
		args = []string{"-version"}
	default:
		return "", fmt.Errorf("unsupported tool: %s", name)
	}

	cmd := exec.CommandContext(ctx, path, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	line := firstLine(strings.TrimSpace(string(output)))
	return normalizeVersionLine(name, line), nil
}

func firstLine(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return text[:idx]
	}
	return text
}

func normalizeVersionLine(name, line string) string {
	switch name {
	case "yt-dlp":
		return line
	case "ffmpeg", "ffprobe":
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			return fields[2]
		}
	}
	return line
}
