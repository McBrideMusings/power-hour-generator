---
id: 6
title: RunWithWork goroutine bridge
status: done
priority: high
tags:
  - tui
  - phase-1
---

# RunWithWork Goroutine Bridge

Implemented `RunWithWork` in `internal/tui/run.go` — bridges work goroutines with the bubbletea event loop.

## What Was Built

- 50ms startup delay + 5ms per-send yield to avoid render race
- Prevents the issue where `p.Send()` before `p.Run()` queues messages without rendering
- Pattern used by fetch and render commands for progress display
