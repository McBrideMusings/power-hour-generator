package dashboard

import (
	"os"
	"path/filepath"
	"testing"
)

// A pasted path arrives decorated: Finder-style quotes, terminal-drag
// backslash escapes, a file:// URL, or a filename with a comma that the CSV
// import heuristic would otherwise claim. Every form must resolve to the file.
func TestResolveLocalVideoFileAcceptsPastedForms(t *testing.T) {
	dir := t.TempDir()
	name := "Video by 27jaykerr [DalMMpsIUuW]-clip-0.mp4"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("not really video"), 0o644); err != nil {
		t.Fatal(err)
	}

	commaName := "Artist, The - clip.mp4"
	commaPath := filepath.Join(dir, commaName)
	if err := os.WriteFile(commaPath, []byte("not really video"), 0o644); err != nil {
		t.Fatal(err)
	}

	escaped := filepath.Join(dir, `Video\ by\ 27jaykerr\ \[DalMMpsIUuW\]-clip-0.mp4`)

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bare", path, path},
		{"single quoted", "'" + path + "'", path},
		{"double quoted", `"` + path + `"`, path},
		{"surrounding spaces", "  " + path + "  ", path},
		{"backslash escaped", escaped, path},
		{"file url", "file://" + path, path},
		{"comma in filename", commaPath, commaPath},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveLocalVideoFile(tc.input)
			if !ok {
				t.Fatalf("resolveLocalVideoFile(%q) = _, false; want the file to resolve", tc.input)
			}
			if got != tc.want {
				t.Fatalf("resolveLocalVideoFile(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveLocalVideoFileRejectsNonFiles(t *testing.T) {
	dir := t.TempDir()
	notVideo := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notVideo, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{
		"",
		"https://youtu.be/abc123",
		"Never Gonna Give You Up",
		notVideo,
		filepath.Join(dir, "missing.mp4"),
		dir,
	} {
		if got, ok := resolveLocalVideoFile(input); ok {
			t.Fatalf("resolveLocalVideoFile(%q) = %q, true; want false", input, got)
		}
	}
}

func TestLooksLikeMissingPathDistinguishesPathsFromQueries(t *testing.T) {
	if !looksLikeMissingPath("/tmp/does-not-exist.mp4") {
		t.Fatal("absolute path to a missing video should read as a missing path")
	}
	if looksLikeMissingPath("Never Gonna Give You Up") {
		t.Fatal("a search query should not read as a missing path")
	}
}
