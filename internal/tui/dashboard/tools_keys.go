package dashboard

import (
	"context"
	"fmt"
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
		return m, nil

	case "down", "j":
		if m.toolsView.cursor < len(m.toolsView.tools)-1 {
			m.toolsView.cursor++
		}
		return m, nil

	case "r":
		m.statusMsg = "Refreshing tool status…"
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
			m.statusMsg = "Every tool is up to date."
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
		note := "ERROR - no update path (installed outside powerhour)"
		for _, name := range skipped {
			m = m.setToolNote(name, note)
		}
		return m, nil
	}

	m.toolUpdateQueue = queue[1:]

	return m, updateToolCmd(queue[0], m.pp.Root)
}

// advanceToolUpdates records one finished update and starts the next queued
// one, falling through to a re-detect when the queue drains.
func (m Model) advanceToolUpdates(msg toolUpdateDoneMsg) (Model, tea.Cmd) {
	// The note lands on the tool it is about, not on whatever the cursor
	// happens to be sitting on.
	if msg.err != nil {
		m = m.setToolNote(msg.tool, fmt.Sprintf("ERROR - update failed: %v", msg.err))
	} else {
		m = m.setToolNote(msg.tool, "Updated.")
	}

	if len(m.toolUpdateQueue) > 0 {
		next := m.toolUpdateQueue[0]
		m.toolUpdateQueue = m.toolUpdateQueue[1:]
		return m, updateToolCmd(next, m.pp.Root)
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
