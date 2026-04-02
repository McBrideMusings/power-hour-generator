package dashboard

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// textInput is a simple single-line text input rendered above the footer.
type textInput struct {
	prompt string
	value  string
	active bool
}

func newTextInput(prompt string) textInput {
	return textInput{prompt: prompt, active: true}
}

func (ti textInput) update(msg tea.KeyMsg) (textInput, inputResult) {
	switch msg.Type {
	case tea.KeyEnter:
		val := strings.TrimSpace(ti.value)
		ti.active = false
		if val == "" {
			return ti, inputResult{cancelled: true}
		}
		return ti, inputResult{submitted: true, value: val}
	case tea.KeyEscape:
		ti.active = false
		return ti, inputResult{cancelled: true}
	case tea.KeyBackspace:
		if len(ti.value) > 0 {
			ti.value = ti.value[:len(ti.value)-1]
		}
	case tea.KeyRunes:
		ti.value += string(msg.Runes)
	}
	return ti, inputResult{}
}

func (ti textInput) view() string {
	cursor := "█"
	return footerStyle.Render(ti.prompt) + " " + ti.value + cursor
}

type inputResult struct {
	submitted bool
	cancelled bool
	value     string
}
