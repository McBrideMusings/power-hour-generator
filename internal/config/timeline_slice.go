package config

import (
	"fmt"
	"strconv"
	"strings"
)

// TimelineSliceExpr represents a parsed collection slice expression.
// It resolves to a half-open interval [start, end) over the remaining rows.
type TimelineSliceExpr struct {
	start timelineSliceBound
	end   timelineSliceBound
}

type timelineSliceBoundKind int

const (
	timelineSliceBoundKeyword timelineSliceBoundKind = iota
	timelineSliceBoundIndex
	timelineSliceBoundPercent
)

type timelineSliceBound struct {
	kind    timelineSliceBoundKind
	keyword string
	value   int
}

// ParseTimelineSlice parses a collection-entry slice expression.
// Empty input defaults to "start:end".
func ParseTimelineSlice(raw string) (TimelineSliceExpr, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		raw = "start:end"
	}

	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return TimelineSliceExpr{}, fmt.Errorf("slice must use start:end syntax")
	}

	start, err := parseTimelineSliceBound(parts[0], true)
	if err != nil {
		return TimelineSliceExpr{}, err
	}
	end, err := parseTimelineSliceBound(parts[1], false)
	if err != nil {
		return TimelineSliceExpr{}, err
	}

	return TimelineSliceExpr{start: start, end: end}, nil
}

func parseTimelineSliceBound(raw string, isStart bool) (timelineSliceBound, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return timelineSliceBound{}, fmt.Errorf("slice endpoint cannot be empty")
	}
	if raw == "start" || raw == "end" {
		return timelineSliceBound{kind: timelineSliceBoundKeyword, keyword: raw}, nil
	}
	if strings.HasSuffix(raw, "%") {
		value, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(raw, "%")))
		if err != nil {
			return timelineSliceBound{}, fmt.Errorf("invalid percent endpoint %q", raw)
		}
		if value < 0 || value > 100 {
			return timelineSliceBound{}, fmt.Errorf("percent endpoint %q must be between 0%% and 100%%", raw)
		}
		return timelineSliceBound{kind: timelineSliceBoundPercent, value: value}, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return timelineSliceBound{}, fmt.Errorf("invalid slice endpoint %q", raw)
	}
	if value == 0 {
		if isStart {
			return timelineSliceBound{}, fmt.Errorf("numeric start endpoint cannot be 0")
		}
		return timelineSliceBound{}, fmt.Errorf("numeric end endpoint cannot be 0")
	}
	return timelineSliceBound{kind: timelineSliceBoundIndex, value: value}, nil
}

// ResolveTimelineSlice resolves a slice expression over a remaining span.
func ResolveTimelineSlice(raw string, total int) (int, int, error) {
	expr, err := ParseTimelineSlice(raw)
	if err != nil {
		return 0, 0, err
	}
	start, end := expr.Resolve(total)
	return start, end, nil
}

// Resolve converts the expression into a half-open interval [start, end).
func (e TimelineSliceExpr) Resolve(total int) (int, int) {
	start := e.start.resolveStart(total)
	end := e.end.resolveEnd(total)

	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	if end < 0 {
		end = 0
	}
	if end > total {
		end = total
	}
	if end < start {
		end = start
	}

	return start, end
}

func (e TimelineSliceExpr) String() string {
	return e.start.String() + ":" + e.end.String()
}

func (b timelineSliceBound) String() string {
	switch b.kind {
	case timelineSliceBoundKeyword:
		return b.keyword
	case timelineSliceBoundPercent:
		return fmt.Sprintf("%d%%", b.value)
	default:
		return strconv.Itoa(b.value)
	}
}

func (b timelineSliceBound) resolveStart(total int) int {
	switch b.kind {
	case timelineSliceBoundKeyword:
		if b.keyword == "end" {
			return total
		}
		return 0
	case timelineSliceBoundPercent:
		return total * b.value / 100
	default:
		if b.value > 0 {
			return b.value - 1
		}
		return total + b.value
	}
}

func (b timelineSliceBound) resolveEnd(total int) int {
	switch b.kind {
	case timelineSliceBoundKeyword:
		if b.keyword == "start" {
			return 0
		}
		return total
	case timelineSliceBoundPercent:
		return total * b.value / 100
	default:
		if b.value > 0 {
			return b.value
		}
		return total + b.value + 1
	}
}

// NormalizeTimelineSlice returns the canonical string form.
func NormalizeTimelineSlice(raw string) string {
	expr, err := ParseTimelineSlice(raw)
	if err != nil {
		return strings.TrimSpace(strings.ToLower(raw))
	}
	return expr.String()
}
