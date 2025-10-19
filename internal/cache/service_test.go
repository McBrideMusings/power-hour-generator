package cache

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

type fakeRunner struct {
	downloadCalls    int
	probeCalls       int
	lastDownloadArgs []string
}

func (f *fakeRunner) Run(_ context.Context, command string, args []string, opts RunOptions) (RunResult, error) {
	base := filepath.Base(command)
	switch base {
	case "yt-dlp":
		var template string
		var pathFile string
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--output":
				if i+1 < len(args) {
					template = args[i+1]
					i++
				}
			case "--print-to-file":
				if i+2 < len(args) && args[i+1] == "after_move:filepath" {
					pathFile = args[i+2]
					i += 2
				}
			}
		}
		if template == "" || pathFile == "" {
			return RunResult{}, fmt.Errorf("fake runner: missing expected args")
		}
		target := strings.Replace(template, ".%(ext)s", ".mp4", 1)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return RunResult{}, err
		}
		if err := os.WriteFile(target, []byte("media"), 0o644); err != nil {
			return RunResult{}, err
		}
		if err := os.WriteFile(pathFile, []byte(target), 0o644); err != nil {
			return RunResult{}, err
		}
		if opts.Stdout != nil {
			_, _ = opts.Stdout.Write([]byte("downloaded\n"))
		}
		f.downloadCalls++
		f.lastDownloadArgs = append([]string(nil), args...)
		return RunResult{Stdout: []byte("downloaded\n")}, nil
	case "ffprobe":
		output := `{"format":{"format_name":"mp4","format_long_name":"MPEG","duration":"10.0"},"streams":[]}`
		if opts.Stdout != nil {
			_, _ = opts.Stdout.Write([]byte(output))
		}
		if opts.Stderr != nil {
			_, _ = opts.Stderr.Write([]byte{})
		}
		f.probeCalls++
		return RunResult{Stdout: []byte(output)}, nil
	default:
		return RunResult{}, fmt.Errorf("fake runner: unexpected command %s", base)
	}
}

func testPaths(t *testing.T) paths.ProjectPaths {
	t.Helper()
	root := t.TempDir()
	meta := filepath.Join(root, ".powerhour")
	return paths.ProjectPaths{
		Root:        root,
		ConfigFile:  filepath.Join(root, "powerhour.yaml"),
		CSVFile:     filepath.Join(root, "powerhour.csv"),
		CookiesFile: filepath.Join(root, "cookies.txt"),
		MetaDir:     meta,
		SrcDir:      filepath.Join(meta, "src"),
		SegmentsDir: filepath.Join(meta, "segments"),
		LogsDir:     filepath.Join(meta, "logs"),
		IndexFile:   filepath.Join(meta, "index.json"),
	}
}

func TestServiceResolveDownload(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	runner := &fakeRunner{}
	svc := &Service{
		Paths:   pp,
		Logger:  log.New(io.Discard, "", 0),
		Runner:  runner,
		ytDLP:   "yt-dlp",
		ffprobe: "ffprobe",
	}

	row := csvplan.Row{Index: 1, Title: "Example", Link: "https://example.com/video"}
	res, err := svc.Resolve(context.Background(), idx, row, ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Status != ResolveStatusDownloaded {
		t.Fatalf("expected status downloaded, got %s", res.Status)
	}
	if res.Entry.CachedPath == "" {
		t.Fatalf("expected cached path")
	}
	if _, err := os.Stat(res.Entry.CachedPath); err != nil {
		t.Fatalf("cached file missing: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(res.Entry.CachedPath), "001_") {
		t.Fatalf("unexpected cache filename: %s", res.Entry.CachedPath)
	}
	if !res.Probed {
		t.Fatalf("expected probe to run")
	}
	if runner.downloadCalls != 1 {
		t.Fatalf("expected 1 download call, got %d", runner.downloadCalls)
	}
	if runner.probeCalls != 1 {
		t.Fatalf("expected 1 probe call, got %d", runner.probeCalls)
	}

	entry, ok := idx.Get(1)
	if !ok {
		t.Fatalf("index missing entry")
	}
	if entry.CachedPath != res.Entry.CachedPath {
		t.Fatalf("index cached path mismatch")
	}
}

func TestServiceResolveLocalReuse(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	source := filepath.Join(pp.Root, "source.mp4")
	if err := os.WriteFile(source, []byte("data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	runner := &fakeRunner{}
	svc := &Service{
		Paths:   pp,
		Logger:  log.New(io.Discard, "", 0),
		Runner:  runner,
		ytDLP:   "yt-dlp",
		ffprobe: "ffprobe",
	}

	row := csvplan.Row{Index: 2, Title: "Local", Link: source}
	first, err := svc.Resolve(context.Background(), idx, row, ResolveOptions{})
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if first.Status != ResolveStatusCopied {
		t.Fatalf("expected copied status, got %s", first.Status)
	}
	if runner.probeCalls != 1 {
		t.Fatalf("expected probe call on first run, got %d", runner.probeCalls)
	}

	second, err := svc.Resolve(context.Background(), idx, row, ResolveOptions{})
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if second.Status != ResolveStatusCached {
		t.Fatalf("expected cached status, got %s", second.Status)
	}
	if second.Probed {
		t.Fatalf("did not expect probe on cache hit")
	}
	if runner.probeCalls != 1 {
		t.Fatalf("probe count changed on cache hit: %d", runner.probeCalls)
	}
}

func TestServiceResolveReprobe(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	source := filepath.Join(pp.Root, "source.mp4")
	if err := os.WriteFile(source, []byte("data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	runner := &fakeRunner{}
	svc := &Service{
		Paths:   pp,
		Logger:  log.New(io.Discard, "", 0),
		Runner:  runner,
		ytDLP:   "yt-dlp",
		ffprobe: "ffprobe",
	}

	row := csvplan.Row{Index: 3, Title: "Reprobe", Link: source}
	if _, err := svc.Resolve(context.Background(), idx, row, ResolveOptions{}); err != nil {
		t.Fatalf("initial resolve: %v", err)
	}
	if runner.probeCalls != 1 {
		t.Fatalf("expected initial probe call, got %d", runner.probeCalls)
	}

	reprobeRes, err := svc.Resolve(context.Background(), idx, row, ResolveOptions{Reprobe: true})
	if err != nil {
		t.Fatalf("reprobe resolve: %v", err)
	}
	if reprobeRes.Status != ResolveStatusCached {
		t.Fatalf("expected cached status on reprobe, got %s", reprobeRes.Status)
	}
	if !reprobeRes.Probed {
		t.Fatalf("expected reprobe flag")
	}
	if runner.probeCalls != 2 {
		t.Fatalf("expected second probe call, got %d", runner.probeCalls)
	}
}

func TestServiceResolveDownloadWithCookies(t *testing.T) {
	pp := testPaths(t)
	idx, err := Load(pp)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	cookiesPath := filepath.Join(pp.Root, "custom_cookies.txt")
	if err := os.WriteFile(cookiesPath, []byte("cookies"), 0o644); err != nil {
		t.Fatalf("write cookies: %v", err)
	}

	runner := &fakeRunner{}
	svc := &Service{
		Paths:       pp,
		Logger:      log.New(io.Discard, "", 0),
		Runner:      runner,
		ytDLP:       "yt-dlp",
		ffprobe:     "ffprobe",
		CookiesPath: cookiesPath,
	}

	row := csvplan.Row{Index: 1, Title: "Example", Link: "https://example.com/video"}
	if _, err := svc.Resolve(context.Background(), idx, row, ResolveOptions{}); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if !containsCookiesArg(runner.lastDownloadArgs, cookiesPath) {
		t.Fatalf("expected yt-dlp args to include cookies path, got %v", runner.lastDownloadArgs)
	}
}

func containsCookiesArg(args []string, path string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--cookies" && args[i+1] == path {
			return true
		}
	}
	return false
}
