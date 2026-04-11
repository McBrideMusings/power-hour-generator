package dashboard

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// textInput is a lightweight footer input that supports both single-line and
// multi-line paste workflows.
type textInput struct {
	prompt    string
	value     string
	active    bool
	multiline bool
}

func newTextInput(prompt string) textInput {
	return textInput{prompt: prompt, active: true}
}

func newMultilineInput(prompt string) textInput {
	return textInput{prompt: prompt, active: true, multiline: true}
}

func (ti textInput) update(msg tea.KeyMsg) (textInput, inputResult) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEscape:
		ti.active = false
		return ti, inputResult{cancelled: true}
	case tea.KeyCtrlS:
		val := strings.TrimSpace(ti.value)
		ti.active = false
		if val == "" {
			return ti, inputResult{cancelled: true}
		}
		return ti, inputResult{submitted: true, value: val}
	case tea.KeyEnter:
		if !ti.multiline {
			val := strings.TrimSpace(ti.value)
			ti.active = false
			if val == "" {
				return ti, inputResult{cancelled: true}
			}
			return ti, inputResult{submitted: true, value: val}
		}
		ti.value += "\n"
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
	if !ti.multiline {
		return footerStyle.Render(ti.prompt) + " " + ti.value + cursor
	}

	var b strings.Builder
	b.WriteString(footerStyle.Render(ti.prompt))
	b.WriteByte('\n')
	if ti.value == "" {
		b.WriteString(cursor)
		return b.String()
	}
	b.WriteString(ti.value)
	b.WriteString(cursor)
	return b.String()
}

type inputResult struct {
	submitted bool
	cancelled bool
	value     string
}
