package render

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"powerhour/internal/tools"
)

func TestRunConcatCopiesSingleSegment(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	output := filepath.Join(dir, "out.mp4")
	concatFile := filepath.Join(dir, "concat.txt")

	want := []byte("pretend mp4 bytes")
	if err := os.WriteFile(source, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteConcatList(concatFile, []TimelineSegmentPath{{Path: source}}); err != nil {
		t.Fatal(err)
	}

	result, err := RunConcat(context.Background(), concatFile, output, tools.ResolvedEncoding{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.Method != "single_copy" {
		t.Fatalf("method = %q, want single_copy", result.Method)
	}

	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("output bytes = %q, want %q", got, want)
	}
}

func TestRunConcatSingleSegmentNoOpWhenOutputMatchesSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	concatFile := filepath.Join(dir, "concat.txt")

	want := []byte("pretend mp4 bytes")
	if err := os.WriteFile(source, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteConcatList(concatFile, []TimelineSegmentPath{{Path: source}}); err != nil {
		t.Fatal(err)
	}

	result, err := RunConcat(context.Background(), concatFile, source, tools.ResolvedEncoding{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.Method != "single_copy" {
		t.Fatalf("method = %q, want single_copy", result.Method)
	}

	got, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("source bytes = %q, want %q", got, want)
	}
}
