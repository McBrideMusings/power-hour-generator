package cli

import (
	"fmt"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/paths"
)

func TestJoinComma(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a, b"},
		{[]string{"a", "b", "c"}, "a, b, c"},
	}

	for _, tt := range tests {
		got := joinComma(tt.input)
		if got != tt.want {
			t.Errorf("joinComma(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCheckConfigWithError(t *testing.T) {
	pp, _ := paths.Resolve(t.TempDir())
	var emptyCfg config.Config
	result := checkConfig(pp, emptyCfg, fmt.Errorf("config file not found"))

	if result.Status != "error" {
		t.Errorf("got status=%q, want error", result.Status)
	}
	if result.Name != "Config" {
		t.Errorf("got name=%q, want Config", result.Name)
	}
}

func TestCheckConfigValid(t *testing.T) {
	pp, _ := paths.Resolve(t.TempDir())
	cfg := config.Config{Version: 1}
	result := checkConfig(pp, cfg, nil)

	if result.Status != "ok" {
		t.Errorf("got status=%q, want ok", result.Status)
	}
}
