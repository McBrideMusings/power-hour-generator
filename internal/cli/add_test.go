package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestProjectFiles(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "powerhour.yaml"), []byte(renderDefaultConfigYAML("yaml")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "songs.yaml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "interstitials.yaml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddCommandAppendsRowsToYAMLCollectionFromFile(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	outputJSON = false
	t.Cleanup(func() {
		projectDir = ""
		outputJSON = false
	})

	writeTestProjectFiles(t, dir)

	importPath := filepath.Join(dir, "import.csv")
	importBody := "title,artist,start_time,duration,link\nSong,Artist,0:10,62,https://example.com\n"
	if err := os.WriteFile(importPath, []byte(importBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--collection", "songs", "--file", importPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "songs.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	for _, want := range []string{
		"title: Song",
		"artist: Artist",
		"start_time: \"0:10\"",
		"duration: \"62\"",
		"link: https://example.com",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("songs.yaml missing %q\n%s", want, content)
		}
	}
}

func TestAddCommandAddsSingleURLArgument(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	outputJSON = false
	t.Cleanup(func() {
		projectDir = ""
		outputJSON = false
	})

	writeTestProjectFiles(t, dir)

	cmd := newAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--collection", "songs", "https://www.youtube.com/watch?v=abc123&list=playlist"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "songs.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "link: https://www.youtube.com/watch?v=abc123") {
		t.Fatalf("songs.yaml missing cleaned link\n%s", content)
	}
	if !strings.Contains(content, "start_time: \"0:00\"") {
		t.Fatalf("songs.yaml missing default start_time\n%s", content)
	}
}

func TestAddCommandReadsSingleURLFromStdinWithTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	outputJSON = false
	t.Cleanup(func() {
		projectDir = ""
		outputJSON = false
	})

	writeTestProjectFiles(t, dir)

	cmd := newAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("https://youtu.be/abc123?si=test\n"))
	cmd.SetArgs([]string{"--collection", "songs"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "songs.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "link: https://youtu.be/abc123") {
		t.Fatalf("songs.yaml missing cleaned stdin link\n%s", content)
	}
	if !strings.Contains(content, "start_time: \"0:00\"") {
		t.Fatalf("songs.yaml missing default start_time from stdin add\n%s", content)
	}
}
