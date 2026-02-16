package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRowUpdateMsg(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "INDEX", Width: 5},
		{Header: "STATUS", Width: 10},
		{Header: "NAME", Width: 10},
	})
	m.AddRow("row:001", []string{"001", "pending", "first"})
	m.AddRow("row:002", []string{"002", "pending", "second"})

	updated, _ := m.Update(RowUpdateMsg{
		Key:    "row:001",
		Fields: map[string]string{"STATUS": "downloaded", "NAME": "updated"},
	})
	m = updated.(ProgressModel)

	if m.rows[0].Fields[1] != "downloaded" {
		t.Errorf("expected STATUS=downloaded, got %q", m.rows[0].Fields[1])
	}
	if m.rows[0].Fields[2] != "updated" {
		t.Errorf("expected NAME=updated, got %q", m.rows[0].Fields[2])
	}
	// Second row unchanged.
	if m.rows[1].Fields[1] != "pending" {
		t.Errorf("expected row 2 STATUS=pending, got %q", m.rows[1].Fields[1])
	}
}

func TestRowUpdateMsg_UnknownKey(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "STATUS", Width: 10},
	})
	m.AddRow("row:001", []string{"pending"})

	updated, _ := m.Update(RowUpdateMsg{
		Key:    "row:999",
		Fields: map[string]string{"STATUS": "done"},
	})
	m = updated.(ProgressModel)

	if m.rows[0].Fields[0] != "pending" {
		t.Errorf("expected STATUS unchanged, got %q", m.rows[0].Fields[0])
	}
}

func TestWorkDoneMsg(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "STATUS", Width: 10},
	})

	updated, cmd := m.Update(WorkDoneMsg{})
	m = updated.(ProgressModel)

	if !m.Done() {
		t.Error("expected Done() to be true after WorkDoneMsg")
	}
	if cmd == nil {
		t.Error("expected tea.Quit command")
	}
}

func TestErrorMsg(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "STATUS", Width: 10},
	})

	updated, cmd := m.Update(ErrorMsg{Err: tea.ErrProgramKilled})
	m = updated.(ProgressModel)

	if !m.Done() {
		t.Error("expected Done() to be true after ErrorMsg")
	}
	if m.Err() == nil {
		t.Error("expected Err() to be non-nil")
	}
	if cmd == nil {
		t.Error("expected tea.Quit command")
	}
}

func TestView(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "INDEX", Width: 5},
		{Header: "STATUS", Width: 10},
		{Header: "TITLE", Width: 10},
	})
	m.AddRow("row:001", []string{"001", "pending", "First Song"})
	m.AddRow("row:002", []string{"002", "downloaded", "Second"})

	view := m.View()

	if !strings.Contains(view, "INDEX") {
		t.Error("expected view to contain INDEX header")
	}
	if !strings.Contains(view, "STATUS") {
		t.Error("expected view to contain STATUS header")
	}
	if !strings.Contains(view, "TITLE") {
		t.Error("expected view to contain TITLE header")
	}
	if !strings.Contains(view, "001") {
		t.Error("expected view to contain row data 001")
	}
	if !strings.Contains(view, "First Song") {
		t.Error("expected view to contain First Song")
	}
	if !strings.Contains(view, "pending") {
		t.Error("expected view to contain pending status")
	}
	if !strings.Contains(view, "downloaded") {
		t.Error("expected view to contain downloaded status")
	}
}

func TestNonEmptyOrDash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "-"},
		{"  ", "-"},
		{"hello", "hello"},
		{" hello ", "hello"},
	}
	for _, tt := range tests {
		got := NonEmptyOrDash(tt.input)
		if got != tt.want {
			t.Errorf("NonEmptyOrDash(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncateWithEllipsis(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"a longer string here", 10, "a longe..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"", 5, ""},
		{"hello", 0, ""},
	}
	for _, tt := range tests {
		got := TruncateWithEllipsis(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("TruncateWithEllipsis(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestMarqueeText(t *testing.T) {
	tests := []struct {
		text    string
		width   int
		tick    int
		want    string
		wantLen int // -1 means same as width
	}{
		// Text fits: returned as-is (no marquee)
		{"short", 10, 0, "short", 5},
		// Text exceeds: marquee sliding window, always width chars
		{"hello world here", 5, 0, "hello", 5},
		{"hello world here", 5, 1, "ello ", 5},
		{"hello world here", 5, 5, " worl", 5},
		// Wraps around with gap
		{"abcdef", 4, 0, "abcd", 4},
		{"abcdef", 4, 6, "   a", 4},
	}
	for _, tt := range tests {
		got := marqueeText(tt.text, tt.width, tt.tick)
		if len(got) != tt.wantLen {
			t.Errorf("marqueeText(%q, %d, %d) length = %d, want %d", tt.text, tt.width, tt.tick, len(got), tt.wantLen)
		}
		if got != tt.want {
			t.Errorf("marqueeText(%q, %d, %d) = %q, want %q", tt.text, tt.width, tt.tick, got, tt.want)
		}
	}
}

func TestTickMsg(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "STATUS", Width: 10},
	})
	m.AddRow("row:001", []string{"pending"})

	updated, cmd := m.Update(tickMsg{})
	m = updated.(ProgressModel)

	if m.tick != 1 {
		t.Errorf("expected tick=1 after tickMsg, got %d", m.tick)
	}
	if cmd == nil {
		t.Error("expected next tick command")
	}
}

func TestTickStopsAfterDone(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "STATUS", Width: 10},
	})
	// Mark done first
	updated, _ := m.Update(WorkDoneMsg{})
	m = updated.(ProgressModel)

	// Tick after done should not schedule another
	updated, cmd := m.Update(tickMsg{})
	m = updated.(ProgressModel)

	if cmd != nil {
		t.Error("expected no tick command after done")
	}
}

func TestProgressCounts(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "INDEX", Width: 5},
		{Header: "STATUS", Width: 10},
	})
	m.AddRow("row:001", []string{"001", "pending"})
	m.AddRow("row:002", []string{"002", "pending"})
	m.AddRow("row:003", []string{"003", "cached"})

	processed, total := m.progressCounts()
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if processed != 1 {
		t.Errorf("expected processed=1, got %d", processed)
	}
}

func TestViewShowsSpinnerWhenNotDone(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "STATUS", Width: 10},
	})
	m.AddRow("row:001", []string{"pending"})

	view := m.View()
	if !strings.Contains(view, "Processing") {
		t.Error("expected view to contain Processing footer when not done")
	}
}

func TestViewHidesSpinnerWhenDone(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "STATUS", Width: 10},
	})
	m.AddRow("row:001", []string{"cached"})
	updated, _ := m.Update(WorkDoneMsg{})
	m = updated.(ProgressModel)

	view := m.View()
	if strings.Contains(view, "Processing") {
		t.Error("expected view to NOT contain Processing footer when done")
	}
}

func TestCtrlC(t *testing.T) {
	m := NewProgressModel("test", []Column{
		{Header: "STATUS", Width: 10},
	})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(ProgressModel)

	if !m.Done() {
		t.Error("expected Done() to be true after ctrl+c")
	}
	if cmd == nil {
		t.Error("expected tea.Quit command")
	}
}
