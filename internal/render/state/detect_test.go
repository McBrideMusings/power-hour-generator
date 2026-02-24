package state

import (
	"os"
	"path/filepath"
	"testing"

	"powerhour/internal/render"
)

func detectTestSegment(outputPath string) render.Segment {
	seg := testSegment()
	seg.OutputPath = outputPath
	return seg
}

func TestDetectChangesForceRendersAll(t *testing.T) {
	rs := emptyState()
	seg := detectTestSegment("/output/seg001.mp4")
	actions := DetectChanges(rs, []render.Segment{seg}, testConfig(), "$INDEX", true)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != ActionRender {
		t.Errorf("action: got %q, want %q", actions[0].Action, ActionRender)
	}
	if actions[0].Reason != ReasonForced {
		t.Errorf("reason: got %q, want %q", actions[0].Reason, ReasonForced)
	}
}

func TestDetectChangesNewSegment(t *testing.T) {
	cfg := testConfig()
	rs := &RenderState{
		GlobalConfigHash: GlobalConfigHash(cfg),
		Segments:         map[string]SegmentState{},
	}
	seg := detectTestSegment("/output/seg001.mp4")
	actions := DetectChanges(rs, []render.Segment{seg}, cfg, "$INDEX", false)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != ActionRender {
		t.Errorf("action: got %q, want %q", actions[0].Action, ActionRender)
	}
	if actions[0].Reason != ReasonNew {
		t.Errorf("reason: got %q, want %q", actions[0].Reason, ReasonNew)
	}
}

func TestDetectChangesConfigChanged(t *testing.T) {
	rs := &RenderState{
		GlobalConfigHash: "sha256:oldconfighash",
		Segments:         map[string]SegmentState{},
	}
	seg := detectTestSegment("/output/seg001.mp4")
	actions := DetectChanges(rs, []render.Segment{seg}, testConfig(), "$INDEX", false)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != ActionRender {
		t.Errorf("action: got %q, want %q", actions[0].Action, ActionRender)
	}
	if actions[0].Reason != ReasonConfigChanged {
		t.Errorf("reason: got %q, want %q", actions[0].Reason, ReasonConfigChanged)
	}
}

func TestDetectChangesInputChanged(t *testing.T) {
	cfg := testConfig()
	seg := detectTestSegment(filepath.Join(t.TempDir(), "seg001.mp4"))

	// Create the output file so it exists
	if err := os.WriteFile(seg.OutputPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	rs := &RenderState{
		GlobalConfigHash: GlobalConfigHash(cfg),
		Segments: map[string]SegmentState{
			seg.OutputPath: {InputHash: "sha256:oldinputhash"},
		},
	}

	actions := DetectChanges(rs, []render.Segment{seg}, cfg, "$INDEX", false)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != ActionRender {
		t.Errorf("action: got %q, want %q", actions[0].Action, ActionRender)
	}
	if actions[0].Reason != ReasonInputChanged {
		t.Errorf("reason: got %q, want %q", actions[0].Reason, ReasonInputChanged)
	}
}

func TestDetectChangesOutputMissing(t *testing.T) {
	cfg := testConfig()
	template := "$INDEX"
	seg := detectTestSegment(filepath.Join(t.TempDir(), "seg001.mp4"))

	currentHash := SegmentInputHash(seg, template)
	rs := &RenderState{
		GlobalConfigHash: GlobalConfigHash(cfg),
		Segments: map[string]SegmentState{
			seg.OutputPath: {InputHash: currentHash},
		},
	}

	actions := DetectChanges(rs, []render.Segment{seg}, cfg, template, false)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != ActionRender {
		t.Errorf("action: got %q, want %q", actions[0].Action, ActionRender)
	}
	if actions[0].Reason != ReasonOutputMissing {
		t.Errorf("reason: got %q, want %q", actions[0].Reason, ReasonOutputMissing)
	}
}

func TestDetectChangesUpToDate(t *testing.T) {
	cfg := testConfig()
	template := "$INDEX"
	seg := detectTestSegment(filepath.Join(t.TempDir(), "seg001.mp4"))

	// Create the output file
	if err := os.WriteFile(seg.OutputPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	currentHash := SegmentInputHash(seg, template)
	rs := &RenderState{
		GlobalConfigHash: GlobalConfigHash(cfg),
		Segments: map[string]SegmentState{
			seg.OutputPath: {InputHash: currentHash},
		},
	}

	actions := DetectChanges(rs, []render.Segment{seg}, cfg, template, false)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != ActionSkip {
		t.Errorf("action: got %q, want %q", actions[0].Action, ActionSkip)
	}
	if actions[0].Reason != ReasonUpToDate {
		t.Errorf("reason: got %q, want %q", actions[0].Reason, ReasonUpToDate)
	}
}

func TestPruneRemovesOldEntries(t *testing.T) {
	rs := &RenderState{
		Segments: map[string]SegmentState{
			"/output/seg001.mp4": {InputHash: "sha256:aaa"},
			"/output/seg002.mp4": {InputHash: "sha256:bbb"},
			"/output/seg003.mp4": {InputHash: "sha256:ccc"},
		},
	}

	currentKeys := map[string]bool{
		"/output/seg001.mp4": true,
		"/output/seg003.mp4": true,
	}

	Prune(rs, currentKeys)

	if len(rs.Segments) != 2 {
		t.Fatalf("expected 2 segments after prune, got %d", len(rs.Segments))
	}
	if _, ok := rs.Segments["/output/seg002.mp4"]; ok {
		t.Error("seg002 should have been pruned")
	}
	if _, ok := rs.Segments["/output/seg001.mp4"]; !ok {
		t.Error("seg001 should still exist")
	}
	if _, ok := rs.Segments["/output/seg003.mp4"]; !ok {
		t.Error("seg003 should still exist")
	}
}
