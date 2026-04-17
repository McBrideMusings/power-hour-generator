package config

import "testing"

func TestResolveTimelineSlice(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		total    int
		wantFrom int
		wantTo   int
	}{
		{name: "default", raw: "", total: 10, wantFrom: 0, wantTo: 10},
		{name: "first ten", raw: "start:10", total: 20, wantFrom: 0, wantTo: 10},
		{name: "thirty one to end", raw: "31:end", total: 60, wantFrom: 30, wantTo: 60},
		{name: "last five", raw: "-5:end", total: 12, wantFrom: 7, wantTo: 12},
		{name: "first half percent", raw: "0%:50%", total: 9, wantFrom: 0, wantTo: 4},
		{name: "second half percent", raw: "50%:100%", total: 9, wantFrom: 4, wantTo: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFrom, gotTo, err := ResolveTimelineSlice(tt.raw, tt.total)
			if err != nil {
				t.Fatalf("ResolveTimelineSlice(%q): %v", tt.raw, err)
			}
			if gotFrom != tt.wantFrom || gotTo != tt.wantTo {
				t.Fatalf("ResolveTimelineSlice(%q) = [%d,%d), want [%d,%d)", tt.raw, gotFrom, gotTo, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

func TestParseTimelineSliceRejectsInvalidSyntax(t *testing.T) {
	tests := []string{
		"oops",
		"start",
		"0:end",
		"start:101%",
		"start:",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseTimelineSlice(raw); err == nil {
				t.Fatalf("ParseTimelineSlice(%q) unexpectedly succeeded", raw)
			}
		})
	}
}
