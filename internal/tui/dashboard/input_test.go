package dashboard

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestTextInputUpdateHandlesKeySpace verifies that the footer text input
// (used by the add-clip slot and other single/multi-line prompts) accepts
// tea.KeySpace as its own key type, not just tea.KeyRunes. Bubbletea sends
// the space bar as tea.KeySpace, distinct from KeyRunes; see issue #109.
func TestTextInputUpdateHandlesKeySpace(t *testing.T) {
	ti := newTextInput("prompt")

	ti, _ = ti.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	ti, res := ti.update(tea.KeyMsg{Type: tea.KeySpace})
	if res.cancelled || res.submitted {
		t.Fatalf("unexpected inputResult from space key: %+v", res)
	}
	ti, _ = ti.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("world")})

	if ti.value != "hello world" {
		t.Fatalf("value = %q, want %q", ti.value, "hello world")
	}
}
