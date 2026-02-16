# TUI System

The `internal/tui/` package provides the interactive progress display using [bubbletea](https://github.com/charmbracelet/bubbletea) (Elm architecture), [lipgloss](https://github.com/charmbracelet/lipgloss) (styling), and [bubbles](https://github.com/charmbracelet/bubbles) (components).

## Components

### StatusWriter

A pre-TUI spinner for setup phases. Runs in a background goroutine, rendering to stderr every 100ms:

```
⠋ Detecting tools (yt-dlp, ffmpeg)... (3.2s)
```

- `NewStatusWriter(w)` — starts the spinner
- `Update(msg)` — changes the message and resets the elapsed timer
- `Stop()` — clears the line and stops the goroutine

Used during config loading, tool detection, and cache initialization — before bubbletea takes over the terminal.

### ProgressModel

A bubbletea table model for tracking per-row progress:

- **Columns** — configurable with max-width truncation
- **Tick animation** — 150ms interval drives spinner and marquee
- **Marquee scrolling** — values that exceed column width scroll horizontally during processing
- **Ellipsis truncation** — long values get `...` suffix when processing is complete
- **Footer** — shows `⠋ Processing 34/61...` with a live spinner

### RunWithWork

Bridges a work goroutine with the bubbletea event loop:

```go
tui.RunWithWork(out, model, func(send func(tea.Msg)) {
    for _, row := range rows {
        // do work...
        send(tui.RowUpdateMsg{Key: key, Values: values})
    }
})
```

**Critical timing**: The work goroutine starts with a 50ms delay to let bubbletea initialize. Each `send()` call includes a 5ms yield so the renderer can process messages. Without these, `p.Send()` before `p.Run()` queues all messages before the first render, producing no visible output.

## Message Types

| Message | Purpose |
|---------|---------|
| `RowUpdateMsg` | Update a table row's values |
| `WorkDoneMsg` | Signal that work is complete |
| `tickMsg` | Internal timer for animation |

## Mode Detection

The TUI adapts to the output context:

- **TTY** — full bubbletea interactive display
- **`--json`** — structured JSON output, no TUI
- **`--no-progress`** — plain line-by-line output
- **Piped output** — graceful fallback to non-interactive

## Lifecycle

A typical command's display lifecycle:

1. `StatusWriter` starts → shows spinner during setup
2. Config loads, tools check, cache initializes (StatusWriter updates each phase)
3. `StatusWriter.Stop()` clears the line
4. `ProgressModel` + `RunWithWork` take over → live table
5. Work completes → final table state displayed
