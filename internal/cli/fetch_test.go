package cli

import (
	"bytes"
	"testing"

	"powerhour/pkg/csvplan"
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

func TestFilterRowsByIndex(t *testing.T) {
	rows := []csvplan.Row{
		{Index: 1, Title: "One"},
		{Index: 2, Title: "Two"},
		{Index: 3, Title: "Three"},
	}

	filtered, err := filterRowsByIndex(rows, []int{2})
	if err != nil {
		t.Fatalf("filterRowsByIndex: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Index != 2 {
		t.Fatalf("unexpected filtered rows: %+v", filtered)
	}
}

func TestFilterRowsByIndexMissing(t *testing.T) {
	rows := []csvplan.Row{
		{Index: 1, Title: "One"},
	}

	_, err := filterRowsByIndex(rows, []int{2})
	if err == nil {
		t.Fatal("expected error for missing index")
	}
}
