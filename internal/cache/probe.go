package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"powerhour/pkg/csvplan"
)

type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams json.RawMessage `json:"streams"`
}

type ffprobeFormat struct {
	FormatName     string `json:"format_name"`
	FormatLongName string `json:"format_long_name"`
	Duration       string `json:"duration"`
}

func (s *Service) probe(ctx context.Context, row csvplan.Row, target string) (*ProbeMetadata, error) {
	if err := os.MkdirAll(s.Paths.LogsDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure logs dir: %w", err)
	}

	logPath := filepath.Join(s.Paths.LogsDir, fmt.Sprintf("probe_%03d.log", row.Index))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open probe log: %w", err)
	}
	defer logFile.Close()

	args := []string{
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-print_format", "json",
		target,
	}

	s.logf("ffprobe row=%d target=%s", row.Index, target)
	result, runErr := s.Runner.Run(ctx, s.ffprobe, args, RunOptions{Stdout: logFile, Stderr: logFile})
	if runErr != nil {
		return nil, fmt.Errorf("ffprobe: %w (see %s)", runErr, logPath)
	}

	raw := json.RawMessage(result.Stdout)
	if len(raw) == 0 {
		return nil, fmt.Errorf("ffprobe produced no output (see %s)", logPath)
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode ffprobe output: %w", err)
	}

	formatRaw, err := json.Marshal(parsed.Format)
	if err != nil {
		return nil, fmt.Errorf("encode ffprobe format: %w", err)
	}

	durationSeconds := 0.0
	if parsed.Format.Duration != "" {
		if v, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
			durationSeconds = v
		}
	}

	meta := &ProbeMetadata{
		FormatName:      parsed.Format.FormatName,
		FormatLongName:  parsed.Format.FormatLongName,
		DurationSeconds: durationSeconds,
		Streams:         cloneRaw(parsed.Streams),
		FormatRaw:       cloneRaw(json.RawMessage(formatRaw)),
		Raw:             cloneRaw(raw),
	}

	return meta, nil
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return json.RawMessage(out)
}
