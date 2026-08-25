package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/tools"
)

// toolsRefreshedMsg carries a fresh tool detection pass back to the model.
type toolsRefreshedMsg struct {
	statuses []ToolStatus
	warning  string
}

// toolUpdateDoneMsg reports one finished update. `U` produces one per tool.
type toolUpdateDoneMsg struct {
	tool string
	err  error
}

// toolsDetectTimeout bounds a refresh so a hung network probe cannot wedge
// the dashboard's spinner forever.
const toolsDetectTimeout = 60 * time.Second

// handleToolsKey owns the tools view's keys. It is dispatched before the
// global bindings because `u` means "update" here and "refresh from disk"
// everywhere else.
func (m Model) handleToolsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.toolsView.cursor > 0 {
			m.toolsView.cursor--
		}
		m.toolsView.note = ""
		m.toolsView.noteIsErr = false
		return m, nil

	case "down", "j":
		if m.toolsView.cursor < len(m.toolsView.tools)-1 {
			m.toolsView.cursor++
		}
		m.toolsView.note = ""
		m.toolsView.noteIsErr = false
		return m, nil

	case "r":
		m.toolsView.note = "Refreshing tool status…"
		m.toolsView.noteIsErr = false
		return m, detectToolsCmd()

	case "u":
		t, ok := m.toolsView.selected()
		if !ok {
			return m, nil
		}
		return m.startToolUpdates([]ToolStatus{t})

	case "U":
		outdated := m.toolsView.outdated()
		if len(outdated) == 0 {
			m.toolsView.note = "Every tool is up to date."
			m.toolsView.noteIsErr = false
			return m, nil
		}
		return m.startToolUpdates(outdated)
	}

	return m, nil
}

// startToolUpdates suspends the dashboard and runs each tool's update command
// with the terminal attached, so package managers that prompt for sudo or
// stream progress reach a real TTY.
//
// Updates are queued and advanced by toolUpdateDoneMsg rather than batched
// into one command: tea.ExecProcess returns its message as soon as the
// process is *scheduled*, so tea.Sequence would fire the trailing refresh
// while the first package manager was still running.
func (m Model) startToolUpdates(targets []ToolStatus) (tea.Model, tea.Cmd) {
	var queue []ToolStatus
	var skipped []string

	for _, t := range targets {
		if !tools.UpdateSupported(t.Name, t.InstallMethod) {
			skipped = append(skipped, t.Name)
			continue
		}
		queue = append(queue, t)
	}

	if len(queue) == 0 {
		m.toolsView.note = fmt.Sprintf("No update path for %s (installed outside powerhour).", joinNames(skipped))
		m.toolsView.noteIsErr = true
		return m, nil
	}

	m.toolUpdateQueue = queue[1:]
	m.toolUpdateFailed = nil
	m.toolUpdateOK = nil
	m.toolsView.note = ""
	m.toolsView.noteIsErr = false

	return m, updateToolCmd(queue[0], m.pp.Root)
}

// advanceToolUpdates records one finished update and starts the next queued
// one, falling through to a re-detect when the queue drains.
func (m Model) advanceToolUpdates(msg toolUpdateDoneMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.toolUpdateFailed = append(m.toolUpdateFailed, msg.tool)
	} else {
		m.toolUpdateOK = append(m.toolUpdateOK, msg.tool)
	}

	if len(m.toolUpdateQueue) > 0 {
		next := m.toolUpdateQueue[0]
		m.toolUpdateQueue = m.toolUpdateQueue[1:]
		return m, updateToolCmd(next, m.pp.Root)
	}

	switch {
	case len(m.toolUpdateFailed) > 0 && len(m.toolUpdateOK) > 0:
		m.toolsView.note = fmt.Sprintf("Updated %s; %s failed.",
			strings.Join(m.toolUpdateOK, ", "), strings.Join(m.toolUpdateFailed, ", "))
		m.toolsView.noteIsErr = true
	case len(m.toolUpdateFailed) > 0:
		m.toolsView.note = fmt.Sprintf("%s failed to update — run it by hand to see why.",
			strings.Join(m.toolUpdateFailed, ", "))
		m.toolsView.noteIsErr = true
	default:
		m.toolsView.note = "Updated " + strings.Join(m.toolUpdateOK, ", ") + "."
		m.toolsView.noteIsErr = false
	}

	return m, detectToolsCmd()
}

// updateToolCmd builds the suspend-and-run command for a single tool.
func updateToolCmd(t ToolStatus, projectRoot string) tea.Cmd {
	argv := tools.UpdateArgv(t.Name, t.InstallMethod, projectRoot)
	c := execCommand(argv[0], argv[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err == nil {
			tools.ClearUpdateNotice(t.Name)
		}
		return toolUpdateDoneMsg{tool: t.Name, err: err}
	})
}

// detectToolsCmd re-runs detection off the UI goroutine.
func detectToolsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), toolsDetectTimeout)
		defer cancel()
		statuses, warning := DetectToolStatuses(ctx)
		return toolsRefreshedMsg{statuses: statuses, warning: warning}
	}
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return "that tool"
	case 1:
		return names[0]
	default:
		return fmt.Sprintf("%s and %d more", names[0], len(names)-1)
	}
}
