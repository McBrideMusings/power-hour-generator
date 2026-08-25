package dashboard

import (
	"errors"
	"strings"
	"testing"

	"powerhour/internal/tools"
)

func toolTargets() []ToolStatus {
	return []ToolStatus{
		{Name: "ffmpeg", InstallMethod: tools.InstallMethodHomebrew, UpdateAvail: "9.0.1 → 9.1.0"},
		{Name: "yt-dlp", InstallMethod: tools.InstallMethodManaged, UpdateAvail: "2026.03.17 → 2026.08.19"},
	}
}

func TestStartToolUpdatesQueuesRestBehindFirst(t *testing.T) {
	var m Model
	m.toolsView = newToolsView(toolTargets())

	updated, cmd := m.startToolUpdates(toolTargets())
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected the first update to be dispatched")
	}
	if len(m.toolUpdateQueue) != 1 || m.toolUpdateQueue[0].Name != "yt-dlp" {
		t.Fatalf("queue = %+v, want the second tool held back", m.toolUpdateQueue)
	}
}

func TestAdvanceToolUpdatesDrainsThenRefreshes(t *testing.T) {
	var m Model
	m.toolsView = newToolsView(toolTargets())
	updated, _ := m.startToolUpdates(toolTargets())
	m = updated.(Model)

	m, cmd := m.advanceToolUpdates(toolUpdateDoneMsg{tool: "ffmpeg"})
	if cmd == nil {
		t.Fatal("expected the queued second update to be dispatched")
	}
	if len(m.toolUpdateQueue) != 0 {
		t.Fatalf("queue = %+v, want it drained", m.toolUpdateQueue)
	}

	m, cmd = m.advanceToolUpdates(toolUpdateDoneMsg{tool: "yt-dlp"})
	if cmd == nil {
		t.Fatal("expected a re-detect once the queue drained")
	}

	// Each note lands on the tool it is about, never on the cursor row.
	if got := m.toolsView.rowStatus["ffmpeg"]; got != "note:Updated." {
		t.Errorf("ffmpeg note = %q, want %q", got, "note:Updated.")
	}
	if got := m.toolsView.rowStatus["yt-dlp"]; got != "note:Updated." {
		t.Errorf("yt-dlp note = %q, want %q", got, "note:Updated.")
	}
}

func TestToolNotesExpire(t *testing.T) {
	var m Model
	m.toolsView = newToolsView(toolTargets())
	m = m.setToolNote("yt-dlp", "Updated.")

	if m.toolsView.rowStatus["yt-dlp"] == "" {
		t.Fatal("note should be set")
	}

	m = m.expireTransientRowNotes()
	if m.toolsView.rowStatus["yt-dlp"] == "" {
		t.Error("note should survive while its deadline is in the future")
	}

	m.tick += 15
	m = m.expireTransientRowNotes()
	if got := m.toolsView.rowStatus["yt-dlp"]; got != "" {
		t.Errorf("note = %q, want it expired", got)
	}
}

func TestAdvanceToolUpdatesReportsFailures(t *testing.T) {
	var m Model
	m.toolsView = newToolsView(toolTargets())
	updated, _ := m.startToolUpdates(toolTargets()[:1])
	m = updated.(Model)

	m, _ = m.advanceToolUpdates(toolUpdateDoneMsg{tool: "ffmpeg", err: errors.New("exit status 1")})

	got := m.toolsView.rowStatus["ffmpeg"]
	if !strings.Contains(got, "ERROR - update failed") {
		t.Errorf("ffmpeg note = %q, want an ERROR note", got)
	}
	if !isErrorRowNote(got) {
		t.Error("a failed update should style as an error note")
	}
}

func TestStartToolUpdatesRefusesToolWithNoUpdatePath(t *testing.T) {
	target := ToolStatus{Name: "vlc", InstallMethod: tools.InstallMethodSystem}

	var m Model
	m.toolsView = newToolsView([]ToolStatus{target})

	updated, cmd := m.startToolUpdates([]ToolStatus{target})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("expected no command for a tool powerhour cannot update")
	}
	got := m.toolsView.rowStatus["vlc"]
	if !strings.Contains(got, "no update path") {
		t.Errorf("vlc note = %q, want an explanation on the vlc row", got)
	}
}

func TestToolsViewOutdatedSkipsUnsupported(t *testing.T) {
	v := newToolsView([]ToolStatus{
		{Name: "ffmpeg", InstallMethod: tools.InstallMethodHomebrew, UpdateAvail: "9.0.1 → 9.1.0"},
		{Name: "vlc", InstallMethod: tools.InstallMethodSystem, UpdateAvail: "3.0.23 → 3.0.24"},
		{Name: "yt-dlp", InstallMethod: tools.InstallMethodManaged},
	})

	got := v.outdated()
	if len(got) != 1 || got[0].Name != "ffmpeg" {
		t.Errorf("outdated = %+v, want just ffmpeg", got)
	}
}

func TestRenderFooterToolsListsUpdateKeys(t *testing.T) {
	m := testTimelineModel(t)
	m.activeView = len(m.collectionNames) + 2 // tools tab

	footer := renderFooter(m)

	for _, want := range []string{"r refresh", "u update", "U update all"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer = %q, want it to list %q", footer, want)
		}
	}
	if strings.Contains(footer, "u refresh") {
		t.Errorf("footer = %q, but u updates in the tools view", footer)
	}
}
