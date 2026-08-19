package cache

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeYouTubeID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"HWl1Tu9oZmY", true},
		{"H-l1_u9oZmY", true},
		{"tooshort", false},
		{"waaaaaaaaaaytoolong", false},
		{"has spaces!", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := LooksLikeYouTubeID(tc.in); got != tc.want {
			t.Errorf("LooksLikeYouTubeID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestExtractVideoIDFromFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"HWl1Tu9oZmY.webm", "HWl1Tu9oZmY"},
		{"Artist - Title [HWl1Tu9oZmY].webm", "HWl1Tu9oZmY"},
		{"not-a-video-id.mp4", ""},
		{"Artist - Title [tooshort].webm", ""},
	}
	for _, tc := range cases {
		if got := ExtractVideoIDFromFilename(tc.in); got != tc.want {
			t.Errorf("ExtractVideoIDFromFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractPlatformFromHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"www.youtube.com", "youtube"},
		{"youtu.be", "youtu"}, // no "youtube" substring; falls back to first host label
		{"vimeo.com", "vimeo"},
		{"www.dailymotion.com", "dailymotion"},
		{"twitch.tv", "twitch"},
		{"example.com", "example"},
		{"localhost", "unknown"},
	}
	for _, tc := range cases {
		if got := ExtractPlatformFromHost(tc.in); got != tc.want {
			t.Errorf("ExtractPlatformFromHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRegisterLocalFileRegistersNewEntry(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	runner := &fakeRunner{}
	svc := &Service{
		Paths:            pp,
		Logger:           log.New(io.Discard, "", 0),
		Runner:           runner,
		ytDLP:            "yt-dlp",
		ffprobe:          "ffprobe",
		filenameTemplate: "$ID",
	}

	srcFile := filepath.Join(t.TempDir(), "song.webm")
	if err := os.WriteFile(srcFile, []byte("media"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	result, err := RegisterLocalFile(context.Background(), svc, pp, idx, RegisterLocalFileParams{
		FilePath:  srcFile,
		SourceURL: "https://example.com/watch?v=videoid",
		Title:     "My Title",
		Artist:    "My Artist",
	})
	if err != nil {
		t.Fatalf("RegisterLocalFile: %v", err)
	}
	if result.AlreadyCached {
		t.Fatalf("expected new registration, got AlreadyCached")
	}
	if result.DryRun {
		t.Fatalf("expected non-dry-run result")
	}
	if result.Entry.Title != "My Title" || result.Entry.Artist != "My Artist" {
		t.Fatalf("unexpected metadata: title=%q artist=%q", result.Entry.Title, result.Entry.Artist)
	}
	if _, err := os.Stat(result.Entry.CachedPath); err != nil {
		t.Fatalf("cached file missing: %v", err)
	}
	if result.Entry.Probe == nil {
		t.Fatalf("expected probe metadata")
	}

	entry, ok := idx.GetByIdentifier(result.Entry.Identifier)
	if !ok {
		t.Fatalf("index missing entry")
	}
	if entry.CachedPath != result.Entry.CachedPath {
		t.Fatalf("index cached path mismatch")
	}
}

func TestRegisterLocalFileDedupesAgainstExistingEntry(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	runner := &fakeRunner{}
	svc := &Service{
		Paths:            pp,
		Logger:           log.New(io.Discard, "", 0),
		Runner:           runner,
		ytDLP:            "yt-dlp",
		ffprobe:          "ffprobe",
		filenameTemplate: "$ID",
	}

	srcFile := filepath.Join(t.TempDir(), "song.webm")
	if err := os.WriteFile(srcFile, []byte("media"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	params := RegisterLocalFileParams{
		FilePath:  srcFile,
		SourceURL: "https://example.com/watch?v=videoid",
	}

	first, err := RegisterLocalFile(context.Background(), svc, pp, idx, params)
	if err != nil {
		t.Fatalf("first RegisterLocalFile: %v", err)
	}
	if first.AlreadyCached {
		t.Fatalf("first call should not be AlreadyCached")
	}

	second, err := RegisterLocalFile(context.Background(), svc, pp, idx, params)
	if err != nil {
		t.Fatalf("second RegisterLocalFile: %v", err)
	}
	if !second.AlreadyCached {
		t.Fatalf("expected second call to report AlreadyCached")
	}
	if second.Entry.Identifier != first.Entry.Identifier {
		t.Fatalf("identifier mismatch between calls")
	}
	if runner.downloadCalls != 0 {
		t.Fatalf("expected no downloads, got %d", runner.downloadCalls)
	}
}

func TestRegisterLocalFileDryRunMakesNoChanges(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	runner := &fakeRunner{}
	svc := &Service{
		Paths:            pp,
		Logger:           log.New(io.Discard, "", 0),
		Runner:           runner,
		ytDLP:            "yt-dlp",
		ffprobe:          "ffprobe",
		filenameTemplate: "$ID",
	}

	srcFile := filepath.Join(t.TempDir(), "song.webm")
	if err := os.WriteFile(srcFile, []byte("media"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	result, err := RegisterLocalFile(context.Background(), svc, pp, idx, RegisterLocalFileParams{
		FilePath:  srcFile,
		SourceURL: "https://example.com/watch?v=videoid",
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("RegisterLocalFile: %v", err)
	}
	if !result.DryRun {
		t.Fatalf("expected DryRun result")
	}
	if _, err := os.Stat(result.TargetPath); err == nil {
		t.Fatalf("dry run should not create cache file at %s", result.TargetPath)
	}
	if len(idx.Entries) != 0 {
		t.Fatalf("dry run should not write index entries, got %d", len(idx.Entries))
	}
}

func TestRegisterLocalFileRejectsInvalidURL(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	svc := &Service{
		Paths:   pp,
		Logger:  log.New(io.Discard, "", 0),
		Runner:  &fakeRunner{},
		ytDLP:   "yt-dlp",
		ffprobe: "ffprobe",
	}

	srcFile := filepath.Join(t.TempDir(), "song.webm")
	if err := os.WriteFile(srcFile, []byte("media"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	_, err = RegisterLocalFile(context.Background(), svc, pp, idx, RegisterLocalFileParams{
		FilePath:  srcFile,
		SourceURL: "not-a-url",
	})
	if err == nil {
		t.Fatalf("expected error for invalid URL")
	}
}

func TestRegisterLocalFileRejectsMissingFile(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	svc := &Service{
		Paths:   pp,
		Logger:  log.New(io.Discard, "", 0),
		Runner:  &fakeRunner{},
		ytDLP:   "yt-dlp",
		ffprobe: "ffprobe",
	}

	_, err = RegisterLocalFile(context.Background(), svc, pp, idx, RegisterLocalFileParams{
		FilePath:  filepath.Join(t.TempDir(), "missing.webm"),
		SourceURL: "https://example.com/watch?v=videoid",
	})
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}
