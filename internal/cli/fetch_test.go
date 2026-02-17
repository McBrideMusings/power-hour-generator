package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteFetchJSON(t *testing.T) {
	cmd := newFetchCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	rows := []fetchRowResult{{
		Index:      1,
		Title:      "Song",
		Status:     "copied",
		CachedPath: "/tmp/cache/001_hash.mp4",
		Link:       "https://example.com/video",
		Identifier: "youtube:videoid",
		MediaID:    "videoid",
		SizeBytes:  1234,
		Probed:     true,
	}}
	counts := fetchCounts{Copied: 1, Probed: 1}

	if err := writeFetchJSON(cmd, "/project", rows, counts); err != nil {
		t.Fatalf("writeFetchJSON: %v", err)
	}

	got := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("\"project\"")) {
		t.Fatalf("expected project field in output: %s", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("copied")) {
		t.Fatalf("expected status in json output: %s", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("\"missing\"")) {
		t.Fatalf("expected missing field in json output: %s", got)
	}
}

func TestWriteFetchTable(t *testing.T) {
	cmd := newFetchCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	rows := []fetchRowResult{{
		Index:      5,
		Status:     "downloaded",
		CachedPath: "/p/cache/005_hash.mp4",
		Link:       "https://example.com/video",
		Identifier: "youtube:videoid",
		MediaID:    "videoid",
		SizeBytes:  2048,
		Probed:     true,
	}}
	counts := fetchCounts{Downloaded: 1, Probed: 1}

	writeFetchTable(cmd, "/project", rows, counts)

	got := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("Project: /project")) {
		t.Fatalf("expected project line, got %s", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Downloaded: 1")) {
		t.Fatalf("expected summary counts, got %s", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("Missing: 0")) {
		t.Fatalf("expected missing count, got %s", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("005")) {
		t.Fatalf("expected row index, got %s", got)
	}
}

func TestWriteFetchFailures(t *testing.T) {
	cmd := newFetchCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	rows := []fetchRowResult{
		{Index: 7, Title: "Missing File", Status: "error", Link: "https://example.com/video", Error: "stat local source"},
		{Index: 8, Title: "OK", Status: "cached"},
	}

	writeFetchFailures(cmd, rows)

	got := buf.String()
	if !strings.Contains(got, "Failures:") {
		t.Fatalf("expected failures header, got %s", got)
	}
	if !strings.Contains(got, "007 Missing File (https://example.com/video): stat local source") {
		t.Fatalf("expected failure details, got %s", got)
	}
}

// Index filter tests moved to index_filter_test.go
