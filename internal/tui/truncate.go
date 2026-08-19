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
	// WindowStart switches TruncateToWidth into windowed mode: instead of
	// truncating from the front (or back, with KeepSuffix), it returns the
	// slice of value whose visual columns fall within
	// [WindowStart, WindowStart+max) — an arbitrary sliding viewport, as
	// used by a fixed-width edit cell whose visible window scrolls with the
	// cursor. Windowed mode ignores Ellipsis/KeepSuffix/EllipsisWhenTight;
	// there is no ellipsis on either edge of a window. WindowStart 0 is the
	// front-of-value window, which is exactly the non-windowed default
	// behavior with no ellipsis configured, so it flows through the same
	// path.
	WindowStart int
}

// TruncateToWidth truncates value so its terminal visual width (via
// go-runewidth, where wide runes such as emoji/CJK occupy 2 columns) does
// not exceed max, appending opts.Ellipsis when a cut is made. The second
// return value is the visual column the returned text actually starts at —
// always 0 outside windowed mode; see TruncateOptions.WindowStart.
//
// value is used exactly as given — callers that want whitespace trimmed
// must trim before calling; trimming here would desynchronize byte-offset
// cursor math in editable-cell callers.
func TruncateToWidth(value string, max int, opts TruncateOptions) (string, int) {
	if opts.WindowStart > 0 {
		return windowByColumn(value, opts.WindowStart, max)
	}

	if max <= 0 {
		return "", 0
	}
	if runewidth.StringWidth(value) <= max {
		return value, 0
	}

	ellipsisWidth := runewidth.StringWidth(opts.Ellipsis)
	budget := max - ellipsisWidth

	if budget <= 0 {
		if opts.EllipsisWhenTight {
			return opts.Ellipsis, 0
		}
		return widthFitRunes(value, max, opts.KeepSuffix), 0
	}

	fit := widthFitRunes(value, budget, opts.KeepSuffix)
	if opts.KeepSuffix {
		return opts.Ellipsis + fit, 0
	}
	return fit + opts.Ellipsis, 0
}

// windowByColumn returns the slice of value whose visual columns fall
// within [startCol, startCol+width) plus actualStart, the visual column the
// returned text actually begins at. actualStart matches startCol unless a
// wide rune straddles that boundary; such a rune is dropped rather than
// split (mirroring the drop-not-split rule at the trailing edge), which
// pushes the real start one column later than requested. Callers must
// measure cursor position against actualStart, not startCol, or a cursor
// highlight lands one cell off whenever that drop happens.
func windowByColumn(value string, startCol, width int) (string, int) {
	if width <= 0 {
		return "", startCol
	}
	endCol := startCol + width
	actualStart := -1
	col := 0
	var b strings.Builder
	for _, r := range value {
		rw := runewidth.RuneWidth(r)
		if col >= startCol {
			if col+rw > endCol {
				break
			}
			if actualStart < 0 {
				actualStart = col
			}
			b.WriteRune(r)
		}
		col += rw
	}
	if actualStart < 0 {
		actualStart = startCol
	}
	return b.String(), actualStart
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
