package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// TruncateOptions configures TruncateToWidth's behaviour at the three axes
// that differ across its call sites (ellipsis style, anchor, tight-budget
// handling). See TruncateToWidth for the exact semantics of each field.
type TruncateOptions struct {
	// Ellipsis is appended (or prepended, with KeepSuffix) when value is cut.
	// Empty means no ellipsis is ever added.
	Ellipsis string
	// KeepSuffix truncates from the front, keeping the trailing runes of
	// value instead of the leading ones.
	KeepSuffix bool
	// EllipsisWhenTight controls what happens when the ellipsis itself does
	// not fit within max (budget <= 0). false (default) falls back to a
	// plain width-fit prefix/suffix with no ellipsis. true returns just the
	// ellipsis.
	EllipsisWhenTight bool
}

// TruncateToWidth truncates value so its terminal visual width (via
// go-runewidth, where wide runes such as emoji/CJK occupy 2 columns) does
// not exceed max, appending opts.Ellipsis when a cut is made.
//
// value is used exactly as given — callers that want whitespace trimmed
// must trim before calling; trimming here would desynchronize byte-offset
// cursor math in editable-cell callers.
func TruncateToWidth(value string, max int, opts TruncateOptions) string {
	if max <= 0 {
		return ""
	}
	if runewidth.StringWidth(value) <= max {
		return value
	}

	ellipsisWidth := runewidth.StringWidth(opts.Ellipsis)
	budget := max - ellipsisWidth

	if budget <= 0 {
		if opts.EllipsisWhenTight {
			return opts.Ellipsis
		}
		return widthFitRunes(value, max, opts.KeepSuffix)
	}

	fit := widthFitRunes(value, budget, opts.KeepSuffix)
	if opts.KeepSuffix {
		return opts.Ellipsis + fit
	}
	return fit + opts.Ellipsis
}

// widthFitRunes returns the longest run of value's runes (from the front, or
// from the back when keepSuffix) whose combined visual width is <= max. A
// rune that would straddle the budget cutoff is dropped rather than split.
func widthFitRunes(value string, max int, keepSuffix bool) string {
	if max <= 0 {
		return ""
	}
	if !keepSuffix {
		var b strings.Builder
		width := 0
		for _, r := range value {
			rw := runewidth.RuneWidth(r)
			if width+rw > max {
				break
			}
			b.WriteRune(r)
			width += rw
		}
		return b.String()
	}

	runes := []rune(value)
	width := 0
	start := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(runes[i])
		if width+rw > max {
			break
		}
		width += rw
		start = i
	}
	return string(runes[start:])
}
