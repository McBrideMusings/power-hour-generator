package tools

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func readVersion(ctx context.Context, def ToolDefinition, paths map[string]string) (string, error) {
	if len(def.Binaries) == 0 {
		return "", fmt.Errorf("tool %s has no binary definition", def.Name)
	}
	mainBinary := def.Binaries[0]
	path, ok := paths[mainBinary.ID]
	if !ok {
		return "", fmt.Errorf("main binary %s missing", mainBinary.ID)
	}

	args := []string{mainBinary.VersionSwitch}
	cmd := exec.CommandContext(ctx, path, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s version: %w", def.Name, err)
	}

	line := firstLine(strings.TrimSpace(string(output)))
	switch def.Name {
	case "yt-dlp":
		return line, nil
	case "ffmpeg":
		return normalizeFFmpegVersion(line), nil
	default:
		return line, nil
	}
}

func firstLine(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return text[:idx]
	}
	return text
}

var ffmpegVersionRegex = regexp.MustCompile(`([0-9]+)(?:\.([0-9]+))?(?:\.([0-9]+))?`)

func normalizeFFmpegVersion(line string) string {
	match := ffmpegVersionRegex.FindString(line)
	if match == "" {
		return line
	}
	return match
}

func meetsMinimum(version, minimum string) bool {
	if minimum == "" {
		return true
	}
	if version == "" {
		return false
	}

	vParts := numericParts(version)
	mParts := numericParts(minimum)
	for len(vParts) < len(mParts) {
		vParts = append(vParts, 0)
	}
	for len(mParts) < len(vParts) {
		mParts = append(mParts, 0)
	}
	for i := 0; i < len(vParts) && i < len(mParts); i++ {
		if vParts[i] > mParts[i] {
			return true
		}
		if vParts[i] < mParts[i] {
			return false
		}
	}
	return true
}

func numericParts(version string) []int {
	var parts []int
	current := strings.Builder{}
	for _, r := range version {
		if r >= '0' && r <= '9' {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			val, _ := strconv.Atoi(current.String())
			parts = append(parts, val)
			current.Reset()
		}
	}
	if current.Len() > 0 {
		val, _ := strconv.Atoi(current.String())
		parts = append(parts, val)
	}
	return parts
}
