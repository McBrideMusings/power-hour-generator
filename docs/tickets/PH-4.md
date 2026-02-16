---
id: 4
title: StatusWriter pre-TUI spinner
status: done
priority: high
tags:
  - tui
  - phase-1
---

# StatusWriter Pre-TUI Spinner

Implemented `StatusWriter` in `internal/tui/status.go` — a pre-TUI spinner with elapsed time per phase.

## What Was Built

- Renders `⠋ Detecting tools... (3.2s)` to stderr, updating every 100ms
- Each `Update()` call resets the phase timer
- Used during setup phases before the main bubbletea program starts
- StatusFunc callback pattern for passing progress updates to lower layers
