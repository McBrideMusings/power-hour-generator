package cli

import (
	"bytes"
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
		CachedPath: "/tmp/.powerhour/src/001_hash.mp4",
		Source:     "/tmp/source.mp4",
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
}

func TestWriteFetchTable(t *testing.T) {
	cmd := newFetchCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	rows := []fetchRowResult{{
		Index:      5,
		Status:     "downloaded",
		CachedPath: "/p/src/005_hash.mp4",
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
	if !bytes.Contains(buf.Bytes(), []byte("005")) {
		t.Fatalf("expected row index, got %s", got)
	}
}
