package render

import (
	"strings"
	"testing"
	"time"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

func TestBuildFilterGraphIncludesOverlays(t *testing.T) {
	cfg := config.Default()
	row := csvplan.Row{
		Index:           1,
		Title:           "Don't Stop, Believin'",
		Artist:          "Journey",
		Name:            "O'Brien",
		DurationSeconds: 60,
	}

	graph, err := BuildFilterGraph(row, cfg)
	if err != nil {
		t.Fatalf("BuildFilterGraph error: %v", err)
	}

	expectations := []string{
		"scale=w=1920:h=1080",
		"pad=w=1920:h=1080",
		"fps=30",
		"fade=t=in",
		"fade=t=out",
		"drawtext=text='Don\\'t Stop\\, Believin\\''",
		"drawtext=text='JOURNEY'",
		"drawtext=text='O\\'Brien'",
		"enable='between(t\\,0\\,4)'",
		"alpha='if(lt(t\\,0)",
		"drawtext=text='1'",
	}

	for _, expected := range expectations {
		if !strings.Contains(graph, expected) {
			t.Fatalf("expected filter graph to contain %q\ngraph: %s", expected, graph)
		}
	}
}

func TestBuildAudioFilters(t *testing.T) {
	cfg := config.Default()
	filters := BuildAudioFilters(cfg)

	expected := []string{
		"loudnorm=I=-14:TP=-1.5:LRA=11",
		"aresample=48000",
	}
	for _, token := range expected {
		if !strings.Contains(filters, token) {
			t.Fatalf("expected audio filters to contain %q, got %q", token, filters)
		}
	}
}

func TestBuildFFmpegCmd(t *testing.T) {
	cfg := config.Default()
	row := csvplan.Row{
		Index:           2,
		Title:           "Another Song",
		Artist:          "Performer",
		DurationSeconds: 45,
		Start:           time.Minute + 30*time.Second,
	}

	graph, err := BuildFilterGraph(row, cfg)
	if err != nil {
		t.Fatalf("BuildFilterGraph error: %v", err)
	}

	cmd, err := BuildFFmpegCmd(row, "/tmp/source.mp4", "/tmp/out.mp4", graph, "aresample=48000", cfg)
	if err != nil {
		t.Fatalf("BuildFFmpegCmd error: %v", err)
	}

	includes := [][]string{
		{"-ss", "1:30.000"},
		{"-i", "/tmp/source.mp4"},
		{"-t", "45"},
		{"-vf", graph},
		{"-af", "aresample=48000"},
		{"-ar", "48000"},
		{"-b:a", "192k"},
		{"-c:a", cfg.Audio.ACodec},
		{"-movflags", "+faststart"},
		{"/tmp/out.mp4"},
	}

	for _, pair := range includes {
		if len(pair) == 1 {
			found := false
			for _, arg := range cmd {
				if arg == pair[0] {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected command to include %q\ncommand: %#v", pair[0], cmd)
			}
			continue
		}

		found := false
		for i := 0; i < len(cmd)-1; i++ {
			if cmd[i] == pair[0] && cmd[i+1] == pair[1] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected command to include %q %q\ncommand: %#v", pair[0], pair[1], cmd)
		}
	}
}

func TestSafeFileSlug(t *testing.T) {
	cases := map[string]string{
		"Song Title!":    "song-title",
		"  MIXED_case  ": "mixed-case",
		"Symbols*&^%":    "symbols",
		"":               "",
	}

	for input, expected := range cases {
		if got := safeFileSlug(input); got != expected {
			t.Fatalf("safeFileSlug(%q) = %q; want %q", input, got, expected)
		}
	}
}
