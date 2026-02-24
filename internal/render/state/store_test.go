package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	rs, err := Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs.GlobalConfigHash != "" {
		t.Errorf("expected empty global hash, got %q", rs.GlobalConfigHash)
	}
	if len(rs.Segments) != 0 {
		t.Errorf("expected empty segments, got %d", len(rs.Segments))
	}
}

func TestLoadCorruptFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	rs, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs.GlobalConfigHash != "" {
		t.Errorf("expected empty global hash, got %q", rs.GlobalConfigHash)
	}
	if len(rs.Segments) != 0 {
		t.Errorf("expected empty segments, got %d", len(rs.Segments))
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	now := time.Now().Truncate(time.Second)
	rs := &RenderState{
		GlobalConfigHash: "sha256:abc123",
		Segments: map[string]SegmentState{
			"/output/seg001.mp4": {
				InputHash:  "sha256:def456",
				RenderedAt: now,
				SourcePath: "/cache/video.mp4",
				DurationS:  60.5,
			},
		},
	}

	if err := rs.Save(path); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if loaded.GlobalConfigHash != rs.GlobalConfigHash {
		t.Errorf("global hash: got %q, want %q", loaded.GlobalConfigHash, rs.GlobalConfigHash)
	}

	seg, ok := loaded.Segments["/output/seg001.mp4"]
	if !ok {
		t.Fatal("segment not found after round trip")
	}
	if seg.InputHash != "sha256:def456" {
		t.Errorf("input hash: got %q, want %q", seg.InputHash, "sha256:def456")
	}
	if !seg.RenderedAt.Equal(now) {
		t.Errorf("rendered_at: got %v, want %v", seg.RenderedAt, now)
	}
	if seg.SourcePath != "/cache/video.mp4" {
		t.Errorf("source path: got %q, want %q", seg.SourcePath, "/cache/video.mp4")
	}
	if seg.DurationS != 60.5 {
		t.Errorf("duration: got %f, want %f", seg.DurationS, 60.5)
	}
}

func TestSaveAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	rs := &RenderState{
		GlobalConfigHash: "sha256:test",
		Segments:         map[string]SegmentState{},
	}

	if err := rs.Save(path); err != nil {
		t.Fatalf("save error: %v", err)
	}

	// Verify no .tmp file left behind
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected .tmp file to not exist, but it does")
	}

	// Verify the actual file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected state file to exist: %v", err)
	}
}
