# 3. CLI and TUI have full parity over a shared core

Date: 2026-08-28

## Status

Accepted

## Context

`powerhour` has two front ends over one system: a Cobra command set in `internal/cli/` and
a Bubbletea dashboard in `internal/tui/dashboard/`. Both fetch, both render, both mutate
plan files, both edit the cache index. They are peers, not a primary and a convenience
wrapper.

When an operation is implemented separately in each, the two implementations diverge
without anything failing. The dashboard's render path once carried its own segment
construction and fade resolution alongside the CLI's; the two drifted until the dashboard
silently skipped auto-fetch that the CLI performed. The `status` command and the dashboard
independently computed per-row fades, and disagreed about whether a row was stale. In both
cases the symptom was not an error — it was two surfaces confidently reporting different
truths about the same project.

The project also requires an agentic control surface: every feature must be programmatically
detectable and manipulable so a program can drive the project and read back its state. A
gesture that exists only in an interactive full-screen TUI is unreachable by a script, and
a TUI is the one surface that can never be automated.

## Decision

**Every operation is a function in a domain package. The CLI and the TUI are both thin
callers of it, and neither may hold logic the other lacks.**

Three obligations follow, and all three are binding on any new feature:

1. **Shared core.** The operation lives in `internal/<domain>/`, not in `internal/cli/` and
   not in `internal/tui/dashboard/`. Both front ends import it. Progress and status flow
   back through an interface each front end implements — `render.ProgressReporter` is the
   established shape.
2. **CLI parity.** Every gesture the TUI offers has a command, and every command honors
   `--json`. A user or a script that never opens the dashboard can do everything.
3. **Feature-time, not follow-up.** The command ships in the same change as the TUI
   gesture. A feature landed on one surface only is incomplete, not staged.

Existing code that already satisfies this is the pattern to copy:
`render/job.RunCollectionJob`, `cache.RegisterLocalFile` (`internal/cache/add.go`),
`dashboard.DetectToolStatuses`, `tools.UpdateArgv`, and
`project.EffectiveCollectionFades`.

Front-end code may still hold what is genuinely presentational — column widths, key
bindings, table layout, spinner state, flag parsing, output formatting. The test is whether
removing that surface would lose behavior. If it would, the behavior is in the wrong place.

## Rejected alternative

Treat the TUI as the rich surface and the CLI as a subset: interactive gestures that only
make sense with a cursor stay in the dashboard, and the CLI covers batch operations.

This is defensible per-gesture — swapping two rows really is nicer with a cursor than with
indices typed by hand — and it avoids designing a command-line grammar for every
interaction. It is rejected because the subset is never stable. Each individually
reasonable exception moves one more capability out of reach of automation, and the
resulting boundary is not a design anyone chose; it is the accumulated residue of which
gestures happened to feel awkward as commands on the day they were written.

## Consequences

- A pull of work that adds a TUI mode adds commands in the same change. Reviewing a
  dashboard-only feature means asking where its command is.
- The core function is written first and the two front ends are written against it. Writing
  the TUI handler first and extracting later is how the duplicated render path happened.
- Some commands will be nested where the flat-command preference would otherwise apply.
  A domain with six genuinely distinct actions gets a group; the flat rule targets a
  single-action command wearing a subcommand, not a real group.
- `--json` output is part of the feature's definition of done, since it is what makes the
  surface agentic rather than merely scriptable.
