package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseSampleTime(timeStr string) (float64, error) {
	timeStr = strings.TrimSpace(timeStr)
	if timeStr == "" {
		return 0, fmt.Errorf("empty time string")
	}

	if d, err := parseDuration(timeStr); err == nil {
		return d.Seconds(), nil
	}

	if seconds, err := parseTimecode(timeStr); err == nil {
		return seconds, nil
	}

	if seconds, err := strconv.ParseFloat(timeStr, 64); err == nil {
		return seconds, nil
	}

	return 0, fmt.Errorf("could not parse as duration, timecode, or seconds")
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	return 0, fmt.Errorf("invalid duration format")
}

func parseTimecode(s string) (float64, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid timecode format")
	}

	var totalSeconds float64
	for i, part := range parts {
		val, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid timecode component %q: %w", part, err)
		}

		if len(parts) == 2 {
			if i == 0 {
				totalSeconds += val * 60
			} else {
				totalSeconds += val
			}
		} else if len(parts) == 3 {
			if i == 0 {
				totalSeconds += val * 3600
			} else if i == 1 {
				totalSeconds += val * 60
			} else {
				totalSeconds += val
			}
		} else {
			return 0, fmt.Errorf("timecode must have 2 or 3 components")
		}
	}

	return totalSeconds, nil
}
