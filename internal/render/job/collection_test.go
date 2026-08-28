package job

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/project"
	"powerhour/internal/render"
	"powerhour/internal/render/state"
	"powerhour/pkg/csvplan"
)

// fakeReporter records every render.ProgressReporter callback it receives so
// tests can assert exactly which ones fired.
type fakeReporter struct {
	starts      []render.Segment
	completes   []render.Result
	fetching    []project.CollectionClip
	fetched     []project.CollectionClip
	fetchErrors []error
}

func (f *fakeReporter) Start(segment render.Segment)       { f.starts = append(f.starts, segment) }
func (f *fakeReporter) Progress(render.Segment, float64)   {}
func (f *fakeReporter) Complete(result render.Result)      { f.completes = append(f.completes, result) }
func (f *fakeReporter) Fetching(cc project.CollectionClip) { f.fetching = append(f.fetching, cc) }
func (f *fakeReporter) Fetched(cc project.CollectionClip, _ render.Segment) {
	f.fetched = append(f.fetched, cc)
}
func (f *fakeReporter) FetchError(_ project.CollectionClip, err error) {
	f.fetchErrors = append(f.fetchErrors, err)
}

// fakeCacheResolver implements CacheResolver without touching real yt-dlp or
// ffmpeg binaries.
type fakeCacheResolver struct {
	resolve func(ctx context.Context, idx *cache.Index, row csvplan.Row, opts cache.ResolveOptions) (cache.ResolveResult, error)
	calls   int
}

func (f *fakeCacheResolver) Resolve(ctx context.Context, idx *cache.Index, row csvplan.Row, opts cache.ResolveOptions) (cache.ResolveResult, error) {
	f.calls++
	return f.resolve(ctx, idx, row, opts)
}

// testPaths builds a ProjectPaths rooted at a fresh t.TempDir(), mirroring
// internal/cache's testPaths helper.
func testPaths(t *testing.T) paths.ProjectPaths {
	t.Helper()
	root := t.TempDir()
	meta := filepath.Join(root, ".powerhour")
	return paths.ProjectPaths{
		Root:            root,
		ConfigFile:      filepath.Join(root, "powerhour.yaml"),
		CSVFile:         filepath.Join(root, "powerhour.csv"),
		CookiesFile:     filepath.Join(root, "cookies.txt"),
		MetaDir:         meta,
		CacheDir:        filepath.Join(root, "cache"),
		SegmentsDir:     filepath.Join(root, "segments"),
		LogsDir:         filepath.Join(root, "logs"),
		IndexFile:       filepath.Join(meta, "index.json"),
		RenderStateFile: filepath.Join(meta, "render-state.json"),
	}
}

func emptyIndex() *cache.Index {
	return &cache.Index{Entries: map[string]cache.Entry{}, Links: map[string]string{}}
}

// localClip builds a CollectionClip whose row.Link is a local, relative
// filename. writeSource controls whether the backing file is written to
// disk (so preflight can succeed).
func localClip(pp paths.ProjectPaths, seq int, title, filename string, writeSource bool) project.CollectionClip {
	if writeSource {
		if err := os.WriteFile(filepath.Join(pp.Root, filename), []byte("fake video"), 0o644); err != nil {
			panic(err)
		}
	}
	row := csvplan.Row{Index: seq, Title: title, Link: filename}
	return project.CollectionClip{
		CollectionName: "songs",
		Clip: project.Clip{
			Sequence:        seq,
			ClipType:        project.ClipTypeSong,
			TypeIndex:       seq,
			Row:             row,
			DurationSeconds: 30,
		},
		OutputDir: "songs",
	}
}

func urlClip(seq int, title, link string) project.CollectionClip {
	row := csvplan.Row{Index: seq, Title: title, Link: link}
	return project.CollectionClip{
		CollectionName: "songs",
		Clip: project.Clip{
			Sequence:        seq,
			ClipType:        project.ClipTypeSong,
			TypeIndex:       seq,
			Row:             row,
			DurationSeconds: 30,
		},
		OutputDir: "songs",
	}
}

// 1. Successful preflight for a local-file clip.
func TestRunCollectionJob_PreflightSuccessLocalFile(t *testing.T) {
	pp := testPaths(t)
	cfg := config.Default()
	idx := emptyIndex()
	clip := localClip(pp, 1, "Song One", "song-one.mp4", true)

	result, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:  pp,
		Config: cfg,
		Index:  idx,
		Clips:  []project.CollectionClip{clip},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("RunCollectionJob: %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	if result.Actions[0].Action != state.ActionRender {
		t.Fatalf("expected ActionRender, got %s (reason %s)", result.Actions[0].Action, result.Actions[0].Reason)
	}
	if result.Segments[0].SourcePath == "" {
		t.Fatalf("expected resolved source path")
	}
}

// 2. Preflight error for a non-fetchable missing local file (non-URL).
func TestRunCollectionJob_PreflightErrorNonFetchable(t *testing.T) {
	pp := testPaths(t)
	cfg := config.Default()
	idx := emptyIndex()
	clip := localClip(pp, 1, "Missing Song", "missing.mp4", false) // never written to disk

	reporter := &fakeReporter{}
	result, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:    pp,
		Config:   cfg,
		Index:    idx,
		Clips:    []project.CollectionClip{clip},
		Reporter: reporter,
	})
	if err != nil {
		t.Fatalf("RunCollectionJob: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Err == nil || !errors.Is(result.Results[0].Err, ErrMissingCachedSource) {
		t.Fatalf("expected ErrMissingCachedSource, got %v", result.Results[0].Err)
	}
	if len(reporter.completes) != 1 {
		t.Fatalf("expected 1 immediate Complete call for non-fetchable error, got %d", len(reporter.completes))
	}
	if len(reporter.fetching) != 0 {
		t.Fatalf("expected no Fetching calls for a local (non-URL) source, got %d", len(reporter.fetching))
	}
}

// 3. Auto-fetch skipped when CacheService is nil for a missing URL clip.
func TestRunCollectionJob_AutoFetchSkippedWhenNoCacheService(t *testing.T) {
	pp := testPaths(t)
	cfg := config.Default()
	idx := emptyIndex()
	clip := urlClip(1, "Remote Song", "https://example.com/watch?v=abc123")

	reporter := &fakeReporter{}
	result, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:        pp,
		Config:       cfg,
		Index:        idx,
		Clips:        []project.CollectionClip{clip},
		CacheService: nil,
		Reporter:     reporter,
	})
	if err != nil {
		t.Fatalf("RunCollectionJob: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Err == nil || !errors.Is(result.Results[0].Err, ErrMissingCachedSource) {
		t.Fatalf("expected ErrMissingCachedSource left in place, got %v", result.Results[0].Err)
	}
	if len(reporter.fetching) != 0 {
		t.Fatalf("expected no Fetching calls when CacheService is nil, got %d", len(reporter.fetching))
	}
	if len(reporter.completes) != 0 {
		t.Fatalf("expected no Complete calls (fetchable errors are deferred to the fetch phase), got %d", len(reporter.completes))
	}
}

// 4. Auto-fetch success: index updated, cache.Save invoked (dirty), segment
// promoted into the render set.
func TestRunCollectionJob_AutoFetchSuccess(t *testing.T) {
	pp := testPaths(t)
	cfg := config.Default()
	idx := emptyIndex()
	link := "https://example.com/watch?v=abc123"
	clip := urlClip(1, "Remote Song", link)

	resolver := &fakeCacheResolver{
		resolve: func(_ context.Context, idx *cache.Index, row csvplan.Row, _ cache.ResolveOptions) (cache.ResolveResult, error) {
			entry := cache.Entry{
				Key:        "abc123",
				Identifier: "abc123",
				Source:     row.Link,
				SourceType: cache.SourceTypeURL,
				CachedPath: filepath.Join(pp.CacheDir, "abc123.mp4"),
			}
			idx.SetEntry(entry)
			idx.SetLink(row.Link, entry.Identifier)
			return cache.ResolveResult{Status: cache.ResolveStatusDownloaded, Updated: true, Entry: entry}, nil
		},
	}

	reporter := &fakeReporter{}
	result, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:        pp,
		Config:       cfg,
		Index:        idx,
		Clips:        []project.CollectionClip{clip},
		CacheService: resolver,
		Reporter:     reporter,
	})
	if err != nil {
		t.Fatalf("RunCollectionJob: %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected Resolve to be called once, got %d", resolver.calls)
	}
	if len(reporter.fetching) != 1 || len(reporter.fetched) != 1 {
		t.Fatalf("expected one Fetching and one Fetched call, got fetching=%d fetched=%d", len(reporter.fetching), len(reporter.fetched))
	}
	if len(reporter.fetchErrors) != 0 {
		t.Fatalf("expected no fetch errors, got %d", len(reporter.fetchErrors))
	}
	entry, ok := idx.GetByIdentifier("abc123")
	if !ok || entry.CachedPath == "" {
		t.Fatalf("expected index to be updated with the fetched entry")
	}
	if _, statErr := os.Stat(pp.IndexFile); statErr != nil {
		t.Fatalf("expected cache.Save to persist the dirty index: %v", statErr)
	}
	if len(result.Actions) != 1 || result.Actions[0].Action != state.ActionRender {
		t.Fatalf("expected the newly fetched segment to be promoted into the render set, got actions=%+v", result.Actions)
	}
	if result.Segments[0].CachedPath == "" {
		t.Fatalf("expected segment to carry the resolved cached path")
	}
}

// 5. Auto-fetch error: Resolve returns an error, leaving the clip in
// preflight-error state and calling Reporter.FetchError.
func TestRunCollectionJob_AutoFetchError(t *testing.T) {
	pp := testPaths(t)
	cfg := config.Default()
	idx := emptyIndex()
	link := "https://example.com/watch?v=deadbeef"
	clip := urlClip(1, "Unavailable Song", link)

	fetchErr := errors.New("video unavailable")
	resolver := &fakeCacheResolver{
		resolve: func(context.Context, *cache.Index, csvplan.Row, cache.ResolveOptions) (cache.ResolveResult, error) {
			return cache.ResolveResult{}, fetchErr
		},
	}

	reporter := &fakeReporter{}
	result, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:        pp,
		Config:       cfg,
		Index:        idx,
		Clips:        []project.CollectionClip{clip},
		CacheService: resolver,
		Reporter:     reporter,
	})
	if err != nil {
		t.Fatalf("RunCollectionJob: %v", err)
	}
	if len(reporter.fetchErrors) != 1 || !errors.Is(reporter.fetchErrors[0], fetchErr) {
		t.Fatalf("expected Reporter.FetchError with the resolve error, got %v", reporter.fetchErrors)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Err == nil || !errors.Is(result.Results[0].Err, ErrMissingCachedSource) {
		t.Fatalf("expected the clip to remain in preflight-error state, got %v", result.Results[0].Err)
	}
}

// 6. Skip branch via state.DetectChanges when the stored hash matches an
// existing output file.
func TestRunCollectionJob_SkipsUpToDateSegment(t *testing.T) {
	pp := testPaths(t)
	cfg := config.Default()
	idx := emptyIndex()
	clip := localClip(pp, 1, "Song One", "song-one.mp4", true)

	seg, err := BuildCollectionRenderSegment(pp, cfg, idx, clip)
	if err != nil {
		t.Fatalf("BuildCollectionRenderSegment: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(seg.OutputPath), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(seg.OutputPath, []byte("already rendered"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	filenameTemplate := cfg.SegmentFilenameTemplate()
	rs := &state.RenderState{
		GlobalConfigHash: state.GlobalConfigHash(cfg),
		Segments: map[string]state.SegmentState{
			seg.OutputPath: {InputHash: state.SegmentInputHash(seg, filenameTemplate)},
		},
	}
	if err := rs.Save(pp.RenderStateFile); err != nil {
		t.Fatalf("save render state: %v", err)
	}

	reporter := &fakeReporter{}
	result, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:    pp,
		Config:   cfg,
		Index:    idx,
		Clips:    []project.CollectionClip{clip},
		Reporter: reporter,
	})
	if err != nil {
		t.Fatalf("RunCollectionJob: %v", err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Action != state.ActionSkip {
		t.Fatalf("expected ActionSkip, got %+v", result.Actions)
	}
	if len(result.Results) != 1 || !result.Results[0].Skipped {
		t.Fatalf("expected a skipped Result, got %+v", result.Results)
	}
	found := false
	for _, res := range reporter.completes {
		if res.Skipped && res.OutputPath == seg.OutputPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Reporter.Complete(Skipped=true) for the up-to-date segment, got %+v", reporter.completes)
	}
}

// 7. DryRun short-circuit: returns Segments/Actions/State without Results.
func TestRunCollectionJob_DryRunShortCircuits(t *testing.T) {
	pp := testPaths(t)
	cfg := config.Default()
	idx := emptyIndex()
	clip := localClip(pp, 1, "Song One", "song-one.mp4", true)

	result, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:  pp,
		Config: cfg,
		Index:  idx,
		Clips:  []project.CollectionClip{clip},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("RunCollectionJob: %v", err)
	}
	if result.Results != nil {
		t.Fatalf("expected DryRun to leave Results nil, got %+v", result.Results)
	}
	if len(result.Segments) != 1 {
		t.Fatalf("expected Segments to be populated, got %d", len(result.Segments))
	}
	if len(result.Actions) != 1 {
		t.Fatalf("expected Actions to be populated, got %d", len(result.Actions))
	}
	if result.State == nil {
		t.Fatalf("expected State to be populated")
	}
}

// 8. Render-state save/prune round trip: an entry not among the current
// run's clips is pruned from the persisted state.
func TestRunCollectionJob_StateSaveAndPruneRoundTrip(t *testing.T) {
	pp := testPaths(t)
	cfg := config.Default()
	idx := emptyIndex()

	clipA := localClip(pp, 1, "Song A", "song-a.mp4", true)
	segA, err := BuildCollectionRenderSegment(pp, cfg, idx, clipA)
	if err != nil {
		t.Fatalf("BuildCollectionRenderSegment A: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(segA.OutputPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(segA.OutputPath, []byte("rendered"), 0o644); err != nil {
		t.Fatalf("write output A: %v", err)
	}

	filenameTemplate := cfg.SegmentFilenameTemplate()
	rs := &state.RenderState{
		GlobalConfigHash: state.GlobalConfigHash(cfg),
		Segments: map[string]state.SegmentState{
			state.SegmentKey(segA): {InputHash: state.SegmentInputHash(segA, filenameTemplate)},
		},
	}
	if err := rs.Save(pp.RenderStateFile); err != nil {
		t.Fatalf("save render state: %v", err)
	}

	// First run: only clip A, which should skip and leave its state entry
	// intact.
	if _, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:  pp,
		Config: cfg,
		Index:  idx,
		Clips:  []project.CollectionClip{clipA},
	}); err != nil {
		t.Fatalf("RunCollectionJob (first): %v", err)
	}

	reloaded, err := state.Load(pp.RenderStateFile)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if _, ok := reloaded.Segments[state.SegmentKey(segA)]; !ok {
		t.Fatalf("expected clip A's state entry to survive a run that includes it")
	}

	// Second run: only clip B. Clip A's entry is no longer part of the
	// current key set and should be pruned.
	clipB := localClip(pp, 2, "Song B", "song-b.mp4", true)
	if _, err := RunCollectionJob(context.Background(), CollectionJobParams{
		Paths:  pp,
		Config: cfg,
		Index:  idx,
		Clips:  []project.CollectionClip{clipB},
	}); err != nil {
		t.Fatalf("RunCollectionJob (second): %v", err)
	}

	reloaded, err = state.Load(pp.RenderStateFile)
	if err != nil {
		t.Fatalf("state.Load (after prune): %v", err)
	}
	if _, ok := reloaded.Segments[state.SegmentKey(segA)]; ok {
		t.Fatalf("expected clip A's state entry to be pruned once it dropped out of the run")
	}
}
