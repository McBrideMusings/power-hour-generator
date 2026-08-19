package tui

import (
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		name  string
		value string
		max   int
		opts  TruncateOptions
		want  string
	}{
		{
			name:  "ascii fits",
			value: "short",
			max:   10,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "short",
		},
		{
			name:  "ascii needs cut",
			value: "a longer string here",
			max:   10,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "a longe...",
		},
		{
			name:  "exact-fit boundary",
			value: "abc",
			max:   3,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "abc",
		},
		{
			name:  "tight budget without EllipsisWhenTight falls back to plain cut",
			value: "abcd",
			max:   3,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "abc",
		},
		{
			name:  "tight budget with EllipsisWhenTight returns bare ellipsis",
			value: "abcd",
			max:   3,
			opts:  TruncateOptions{Ellipsis: "...", EllipsisWhenTight: true},
			want:  "...",
		},
		{
			name:  "no ellipsis configured",
			value: "abcdef",
			max:   3,
			opts:  TruncateOptions{},
			want:  "abc",
		},
		{
			name:  "empty value",
			value: "",
			max:   5,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "",
		},
		{
			name:  "max zero",
			value: "hello",
			max:   0,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "",
		},
		{
			name:  "max negative",
			value: "hello",
			max:   -1,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "",
		},
		// Wide characters (emoji, CJK) occupy 2 terminal columns per rune.
		{
			name:  "emoji fits exactly",
			value: "🎵 Hello",
			max:   8,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "🎵 Hello",
		},
		{
			name:  "emoji needs cut",
			value: "🎵 Hello",
			max:   7,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "🎵 H...",
		},
		{
			name:  "cjk needs cut",
			value: "日本語テスト",
			max:   7,
			opts:  TruncateOptions{Ellipsis: "..."},
			want:  "日本...",
		},
		{
			name:  "single-column ellipsis with EllipsisWhenTight at width 1",
			value: "🎬movie",
			max:   1,
			opts:  TruncateOptions{Ellipsis: "…", EllipsisWhenTight: true},
			want:  "…",
		},
		{
			name:  "wide rune straddling cut is dropped, not split",
			value: "🎵🎵🎵",
			max:   3,
			opts:  TruncateOptions{Ellipsis: "…"},
			want:  "🎵…",
		},
		{
			name:  "KeepSuffix keeps trailing runes",
			value: "/very/long/path/to/file.mp4",
			max:   10,
			opts:  TruncateOptions{KeepSuffix: true},
			want:  "o/file.mp4",
		},
		{
			name:  "KeepSuffix exact fit",
			value: "abc",
			max:   3,
			opts:  TruncateOptions{KeepSuffix: true},
			want:  "abc",
		},
		{
			name:  "KeepSuffix with CJK",
			value: "日本語テスト",
			max:   7,
			opts:  TruncateOptions{KeepSuffix: true},
			want:  "テスト",
		},
		{
			name:  "ellipsis wider than max with EllipsisWhenTight false",
			value: "abcdefgh",
			max:   2,
			opts:  TruncateOptions{Ellipsis: "...", EllipsisWhenTight: false},
			want:  "ab",
		},
		{
			name:  "ellipsis wider than max with EllipsisWhenTight true",
			value: "abcdefgh",
			max:   2,
			opts:  TruncateOptions{Ellipsis: "...", EllipsisWhenTight: true},
			want:  "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := TruncateToWidth(tt.value, tt.max, tt.opts)
			if got != tt.want {
				t.Errorf("TruncateToWidth(%q, %d, %+v) = %q, want %q", tt.value, tt.max, tt.opts, got, tt.want)
			}
			if tt.max > 0 {
				if width := runewidth.StringWidth(got); width > tt.max && !tt.opts.EllipsisWhenTight {
					t.Errorf("TruncateToWidth(%q, %d, %+v) visual width = %d, want <= %d", tt.value, tt.max, tt.opts, width, tt.max)
				}
			}
		})
	}
}
