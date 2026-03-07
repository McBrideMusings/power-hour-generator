package project

import (
	"testing"
)

func TestResolveProjectPath(t *testing.T) {
	tests := []struct {
		name  string
		root  string
		value string
		want  string
	}{
		{"empty value", "/root", "", ""},
		{"whitespace value", "/root", "   ", ""},
		{"absolute path", "/root", "/abs/path/file.csv", "/abs/path/file.csv"},
		{"relative path", "/root", "plans/songs.csv", "/root/plans/songs.csv"},
		{"relative with dots", "/root", "../other/file.csv", "/other/file.csv"},
		{"absolute with redundant slashes", "/root", "/abs//path/./file.csv", "/abs/path/file.csv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveProjectPath(tt.root, tt.value)
			if got != tt.want {
				t.Errorf("resolveProjectPath(%q, %q) = %q, want %q", tt.root, tt.value, got, tt.want)
			}
		})
	}
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
