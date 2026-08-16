package render

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

// fakeRunner records every command invocation without executing anything,
// so tests can assert whether the render service actually shelled out to
// an encoder.
type fakeRunner struct {
	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	command string
	args    []string
}

func (f *fakeRunner) Run(_ context.Context, command string, args []string, _ cache.RunOptions) (cache.RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{command: command, args: args})
	return cache.RunResult{}, nil
}

func newSkipTestService(t *testing.T, runner cache.Runner) *Service {
	t.Helper()
	dir := t.TempDir()
	segmentsDir := filepath.Join(dir, "segments")
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(segmentsDir, 0o755); err != nil {
		t.Fatalf("mkdir segments dir: %v", err)
	}
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}

	return &Service{
		Paths: paths.ProjectPaths{
			Root:        dir,
			SegmentsDir: segmentsDir,
			LogsDir:     logsDir,
		},
		Config:     config.Default(),
		Runner:     runner,
		ffmpegPath: "ffmpeg-stub",
	}
}

func newSkipTestSegment(t *testing.T, svc *Service) Segment {
	t.Helper()
	cfg := svc.Config
	row := csvplan.Row{
		Index:           1,
		Title:           "Test Song",
		Artist:          "Test Artist",
		DurationSeconds: 60,
	}
	seg := newTestSegment(cfg, row)
	seg.Entry = cache.Entry{Probe: &cache.ProbeMetadata{DurationSeconds: 600}}
	seg.OutputPath = filepath.Join(svc.Paths.SegmentsDir, "seg-001.mp4")
	return seg
}

func TestRenderSkipsWhenHashMatchesAndOutputExists(t *testing.T) {
	runner := &fakeRunner{}
	svc := newSkipTestService(t, runner)
	seg := newSkipTestSegment(t, svc)

	if err := os.WriteFile(seg.OutputPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	seg.StoredHash = SegmentInputHash(seg, svc.Config.SegmentFilenameTemplate())

	results := svc.Render(context.Background(), []Segment{seg}, Options{Concurrency: 1})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if !res.Skipped {
		t.Fatalf("expected segment to be skipped")
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 0 {
		t.Fatalf("expected no runner calls, got %d: %+v", len(runner.calls), runner.calls)
	}
}

func TestRenderRunsWhenStoredHashMatchesButOutputMissing(t *testing.T) {
	runner := &fakeRunner{}
	svc := newSkipTestService(t, runner)
	seg := newSkipTestSegment(t, svc)

	// Do not write the output file - it is missing.
	seg.StoredHash = SegmentInputHash(seg, svc.Config.SegmentFilenameTemplate())

	results := svc.Render(context.Background(), []Segment{seg}, Options{Concurrency: 1})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Skipped {
		t.Fatalf("expected segment to be re-rendered, was skipped")
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %+v", len(runner.calls), runner.calls)
	}
	if runner.calls[0].command != svc.ffmpegPath {
		t.Fatalf("expected ffmpeg command %q, got %q", svc.ffmpegPath, runner.calls[0].command)
	}
}

func TestRenderRunsWhenStoredHashDiffersEvenIfOutputExists(t *testing.T) {
	runner := &fakeRunner{}
	svc := newSkipTestService(t, runner)
	seg := newSkipTestSegment(t, svc)

	if err := os.WriteFile(seg.OutputPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	seg.StoredHash = "sha256:stale"

	results := svc.Render(context.Background(), []Segment{seg}, Options{Concurrency: 1})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Skipped {
		t.Fatalf("expected segment to be re-rendered, was skipped")
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 1 {
		t.Fatalf("expected exactly 1 runner call, got %d: %+v", len(runner.calls), runner.calls)
	}
	if runner.calls[0].command != svc.ffmpegPath {
		t.Fatalf("expected ffmpeg command %q, got %q", svc.ffmpegPath, runner.calls[0].command)
	}
}
