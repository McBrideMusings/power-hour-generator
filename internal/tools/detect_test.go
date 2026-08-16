package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeBinary writes an executable shell script that prints version and
// appends a marker line to markerPath each time it runs, so tests can detect
// whether the version subprocess actually executed.
func writeFakeBinary(t *testing.T, dir, name, version, markerPath string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho ran >> " + markerPath + "\necho '" + version + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return path
}

func fakeToolDef(name string) ToolDefinition {
	return ToolDefinition{
		Name: name,
		Binaries: []BinarySpec{
			{ID: "main", Executable: name, VersionSwitch: "--version"},
		},
	}
}

// setupFakeSystemTool creates an isolated PATH containing only the fake
// binary, and an empty POWERHOUR_TOOLS_DIR so locateCache never fires.
func setupFakeSystemTool(t *testing.T, toolName, version, markerPath string) {
	t.Helper()
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, toolName, version, markerPath)

	toolsDir := t.TempDir()
	t.Setenv("POWERHOUR_TOOLS_DIR", toolsDir)
	t.Setenv("PATH", binDir)
}

func countRuns(t *testing.T, markerPath string) int {
	t.Helper()
	data, err := os.ReadFile(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read marker: %v", err)
	}
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}

func TestDetectOneSystemRecordsChecksum(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.txt")
	setupFakeSystemTool(t, "faketool", "1.2.3", marker)
	def := fakeToolDef("faketool")

	status, entry, dirty := detectOne(context.Background(), def, ManifestEntry{})

	if !dirty {
		t.Fatalf("expected dirty=true for newly discovered system tool")
	}
	if entry.Checksum == "" {
		t.Fatalf("expected ManifestEntry.Checksum to be populated for system-sourced tool")
	}
	if status.Checksum == "" {
		t.Fatalf("expected Status.Checksum to be populated for system-sourced tool")
	}
	if entry.Source != SourceSystem {
		t.Fatalf("expected source=system, got %v", entry.Source)
	}
	if status.Version != "1.2.3" {
		t.Fatalf("expected version 1.2.3, got %q", status.Version)
	}
}

func TestDetectOneSystemSkipsVersionOnRepeat(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.txt")
	setupFakeSystemTool(t, "faketool", "1.2.3", marker)
	def := fakeToolDef("faketool")

	_, entry, _ := detectOne(context.Background(), def, ManifestEntry{})
	if countRuns(t, marker) != 1 {
		t.Fatalf("expected exactly 1 run after first detect, got %d", countRuns(t, marker))
	}

	// Feed the resulting entry back in - this simulates a second `powerhour
	// check` invocation against an unchanged install.
	status, _, dirty := detectOne(context.Background(), def, entry)

	if countRuns(t, marker) != 1 {
		t.Fatalf("expected version script NOT to run again on repeat detect, but run count is %d", countRuns(t, marker))
	}
	if status.Version != "1.2.3" {
		t.Fatalf("expected trusted version 1.2.3, got %q", status.Version)
	}
	if dirty {
		t.Fatalf("expected dirty=false when nothing changed")
	}
}

func TestDetectOneSystemRereadsVersionWhenBinaryChanges(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.txt")
	binDir := t.TempDir()
	writeFakeBinary(t, binDir, "faketool", "1.2.3", marker)

	toolsDir := t.TempDir()
	t.Setenv("POWERHOUR_TOOLS_DIR", toolsDir)
	t.Setenv("PATH", binDir)

	def := fakeToolDef("faketool")
	_, entry, _ := detectOne(context.Background(), def, ManifestEntry{})
	if countRuns(t, marker) != 1 {
		t.Fatalf("expected 1 run after first detect, got %d", countRuns(t, marker))
	}

	// Replace the binary in place with a new version - changes the on-disk
	// bytes and thus the checksum.
	writeFakeBinary(t, binDir, "faketool", "9.9.9", marker)

	status, newEntry, dirty := detectOne(context.Background(), def, entry)

	if countRuns(t, marker) != 2 {
		t.Fatalf("expected version script to re-run after binary changed, run count is %d", countRuns(t, marker))
	}
	if status.Version != "9.9.9" {
		t.Fatalf("expected re-read version 9.9.9, got %q", status.Version)
	}
	if !dirty {
		t.Fatalf("expected dirty=true after version change")
	}
	if newEntry.Checksum == entry.Checksum {
		t.Fatalf("expected checksum to change after binary replacement")
	}
}

func TestDetectOneBackfillsMissingChecksum(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker.txt")
	binDir := t.TempDir()
	path := writeFakeBinary(t, binDir, "faketool", "1.2.3", marker)

	toolsDir := t.TempDir()
	t.Setenv("POWERHOUR_TOOLS_DIR", toolsDir)
	t.Setenv("PATH", binDir)

	def := fakeToolDef("faketool")

	// Simulate a legacy manifest entry from before this fix: version and
	// install method recorded, but no checksum.
	legacyEntry := ManifestEntry{
		Tool:          "faketool",
		Version:       "1.2.3",
		Source:        SourceSystem,
		Paths:         map[string]string{"main": path},
		InstallMethod: "system",
	}

	status, entry, dirty := detectOne(context.Background(), def, legacyEntry)

	if !dirty {
		t.Fatalf("expected dirty=true to persist the backfilled checksum")
	}
	if entry.Checksum == "" {
		t.Fatalf("expected checksum to be backfilled on legacy entry")
	}
	if status.Checksum == "" {
		t.Fatalf("expected status checksum to be populated after backfill")
	}
	// The version script runs once here because the legacy entry has no
	// checksum, so the trust fast path can't fire yet - that's expected for
	// this one run. A subsequent run (covered by the repeat test) is what
	// must skip it.
	if countRuns(t, marker) != 1 {
		t.Fatalf("expected exactly 1 version run during backfill, got %d", countRuns(t, marker))
	}
}
