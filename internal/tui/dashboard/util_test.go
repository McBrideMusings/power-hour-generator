package dashboard

import (
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

func TestTruncateCollectionValuePrefersFileName(t *testing.T) {
	value := "/Users/pierce/Projects/power-hour-generator/powerhour-1/videos/ghibli-visuals.webm"
	got := truncateCollectionValue(value, 32)

	if got != ".../videos/ghibli-visuals.webm" {
		t.Fatalf("truncateCollectionValue() = %q", got)
	}
}

func TestTruncateCollectionValueFallsBackForURLs(t *testing.T) {
	value := "https://example.com/really/long/path/to/a/video/file"
	got := truncateCollectionValue(value, 20)

	if got != "https://example.c..." {
		t.Fatalf("truncateCollectionValue() = %q", got)
	}
}

func TestTruncateCollectionValueWideChars(t *testing.T) {
	tests := []struct {
		name  string
		value string
		max   int
	}{
		{"emoji title", "🎵 Hello there wide chars", 10},
		{"emoji title tight", "🎵 Hello there wide chars", 3},
		{"cjk path", "/Users/pierce/動画/日本語のファイル名/クリップ.mp4", 24},
		{"cjk url-like", "https://例え.みんな/長い/パス/を/持つ/動画", 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateCollectionValue(tt.value, tt.max)
			if !utf8.ValidString(got) {
				t.Fatalf("truncateCollectionValue(%q, %d) = %q, not valid UTF-8", tt.value, tt.max, got)
			}
			if width := runewidth.StringWidth(got); width > tt.max {
				t.Fatalf("truncateCollectionValue(%q, %d) = %q, visual width = %d, want <= %d", tt.value, tt.max, got, width, tt.max)
			}
		})
	}
}

func TestRenderCellWidthWithWideChars(t *testing.T) {
	values := []string{
		"🎵 Hello there wide chars",
		"日本語のとても長い動画タイトルです",
		"short",
	}
	widths := []int{5, 10, 20}
	plain := lipgloss.NewStyle()
	styled := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))

	for _, v := range values {
		for _, w := range widths {
			gotPlain := renderCell(v, w, plain)
			gotStyled := renderCell(v, w, styled)
			if pw := lipgloss.Width(gotPlain); pw != w {
				t.Errorf("renderCell(%q, %d, plain) width = %d, want %d", v, w, pw, w)
			}
			if sw := lipgloss.Width(gotStyled); sw != w {
				t.Errorf("renderCell(%q, %d, styled) width = %d, want %d", v, w, sw, w)
			}
			if lipgloss.Width(gotPlain) != lipgloss.Width(gotStyled) {
				t.Errorf("renderCell(%q, %d) width mismatch: plain=%d styled=%d", v, w, lipgloss.Width(gotPlain), lipgloss.Width(gotStyled))
			}
		}
	}
}
