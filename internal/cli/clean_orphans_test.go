package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/internal/render"
)

// resolveCleanOrphansProject re-runs the path/config resolution runCleanOrphans
// does, so a test can ask what the project currently expects on disk.
func resolveCleanOrphansProject(t *testing.T, root string) (paths.ProjectPaths, config.Config) {
	t.Helper()
	pp, err := paths.Resolve(root)
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())
	return pp, cfg
}

// expectedSegmentFor returns the path the row whose title is want currently
// renders to, at its current playback position.
func expectedSegmentFor(t *testing.T, root, want string) string {
	t.Helper()
	pp, cfg := resolveCleanOrphansProject(t, root)
	exp, err := buildExpectedRender(pp, cfg)
	if err != nil {
		t.Fatalf("buildExpectedRender: %v", err)
	}
	for _, seg := range exp.segments {
		if strings.TrimSpace(seg.Clip.Row.Title) == want {
			return seg.OutputPath
		}
	}
	t.Fatalf("no expected segment for row %q", want)
	return ""
}

func writeFakeSegment(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runCleanOrphansCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Cleanup(func() {
		cleanDryRun = false
		cleanRenumbered = false
	})
	cmd := newCleanOrphansCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// TestCleanOrphansKeepsSegmentRenderedAtOlderPosition is #215: a segment
// rendered while its row sat at position 1 is still that row's render after
// the row moves to position 2. The TUI previewer plays it and
// state.DetectChanges renames it rather than re-encoding, so clean orphans
// must not delete it.
func TestCleanOrphansKeepsSegmentRenderedAtOlderPosition(t *testing.T) {
	root := setUpOrderTestProject(t)

	oldPath := expectedSegmentFor(t, root, "Song A")
	writeFakeSegment(t, oldPath, 64)

	// Slots: 1 intro (file), 2 Song A, 3 Song B, ... — trade Song A into the
	// second song slot so its playback position becomes 2.
	if _, err := runOrderCLI(t, "swap", "2", "3"); err != nil {
		t.Fatalf("order swap: %v", err)
	}

	newPath := expectedSegmentFor(t, root, "Song A")
	if newPath == oldPath {
		t.Fatalf("swap did not move Song A: still expects %s", oldPath)
	}

	cleanDryRun = true
	out, err := runCleanOrphansCLI(t)
	if err != nil {
		t.Fatalf("clean orphans: %v", err)
	}

	if strings.Contains(out, "would remove "+oldPath) {
		t.Fatalf("expected mis-numbered segment %s to survive, got:\n%s", oldPath, out)
	}
	if !strings.Contains(out, "kept "+oldPath) {
		t.Fatalf("expected mis-numbered segment %s to be reported as kept, got:\n%s", oldPath, out)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected %s to still exist: %v", oldPath, err)
	}
}

// TestCleanOrphansKeepsInlineFileSegment is #218: the normalized render of an
// inline file: timeline entry lives under segments/__inline__/ and belongs to
// no collection, so a keep-set built only from collection clips called every
// one of them an orphan.
func TestCleanOrphansKeepsInlineFileSegment(t *testing.T) {
	root := setUpOrderTestProject(t)
	pp, _ := resolveCleanOrphansProject(t, root)

	inlinePath := render.InlineSegmentPath(pp.SegmentsDir, 0, filepath.Join(root, "intro.mp4"))
	if filepath.Base(filepath.Dir(inlinePath)) != "__inline__" {
		t.Fatalf("expected an __inline__ path, got %s", inlinePath)
	}
	writeFakeSegment(t, inlinePath, 32)

	cleanDryRun = false
	out, err := runCleanOrphansCLI(t)
	if err != nil {
		t.Fatalf("clean orphans: %v", err)
	}

	if _, err := os.Stat(inlinePath); err != nil {
		t.Fatalf("expected inline segment %s to survive, got %v\noutput:\n%s", inlinePath, err, out)
	}
}

// TestCleanOrphansRemovesUnclaimedSegment guards the two fixes above from
// over-reaching: a file no live row and no timeline entry claims is still an
// orphan.
func TestCleanOrphansRemovesUnclaimedSegment(t *testing.T) {
	root := setUpOrderTestProject(t)
	pp, _ := resolveCleanOrphansProject(t, root)

	live := expectedSegmentFor(t, root, "Song A")
	orphan := filepath.Join(filepath.Dir(live), "047_Deleted_Song.mp4")
	writeFakeSegment(t, orphan, 16)

	stray := filepath.Join(pp.SegmentsDir, "__inline__", "099-gone.mp4")
	writeFakeSegment(t, stray, 16)

	cleanDryRun = false
	out, err := runCleanOrphansCLI(t)
	if err != nil {
		t.Fatalf("clean orphans: %v", err)
	}

	for _, path := range []string{orphan, stray} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected orphan %s to be removed, got %v\noutput:\n%s", path, err, out)
		}
	}
}

// TestCleanOrphansRenumberedFlagRemovesOnlySupersededDuplicates asserts the
// tiering: the newest mis-numbered render of a live row is never deleted, and
// the older ones beneath it go only when --renumbered is passed.
func TestCleanOrphansRenumberedFlagRemovesOnlySupersededDuplicates(t *testing.T) {
	root := setUpOrderTestProject(t)

	// Two renders of Song A at positions it no longer occupies. Song A sits at
	// position 1 here, so both of these are mis-numbered.
	current := expectedSegmentFor(t, root, "Song A")
	dir := filepath.Dir(current)
	base := filepath.Base(current)
	older := filepath.Join(dir, strings.Replace(base, "001", "005", 1))
	newer := filepath.Join(dir, strings.Replace(base, "001", "009", 1))
	if older == current || newer == current {
		t.Fatalf("expected a position-bearing segment name, got %s", base)
	}
	writeFakeSegment(t, older, 16)
	writeFakeSegment(t, newer, 16)

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	t.Run("without the flag both survive", func(t *testing.T) {
		cleanDryRun = false
		out, err := runCleanOrphansCLI(t)
		if err != nil {
			t.Fatalf("clean orphans: %v", err)
		}
		for _, path := range []string{older, newer} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("expected %s to survive, got %v\noutput:\n%s", path, err, out)
			}
		}
		if !strings.Contains(out, "--renumbered") {
			t.Fatalf("expected output to advertise --renumbered, got:\n%s", out)
		}
	})

	t.Run("with the flag only the superseded one goes", func(t *testing.T) {
		cleanDryRun = false
		out, err := runCleanOrphansCLI(t, "--renumbered")
		if err != nil {
			t.Fatalf("clean orphans --renumbered: %v", err)
		}
		if _, err := os.Stat(older); !os.IsNotExist(err) {
			t.Fatalf("expected superseded %s to be removed, got %v\noutput:\n%s", older, err, out)
		}
		if _, err := os.Stat(newer); err != nil {
			t.Fatalf("expected newest %s to survive --renumbered, got %v\noutput:\n%s", newer, err, out)
		}
	})
}
