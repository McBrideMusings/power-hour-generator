# Feature / Bug Ticket – Render Progress Table Refresh

- **Issue**: The interactive render progress table writes an initial “starting” snapshot and then redraws the live table, leaving duplicate output in the terminal. The second snapshot is correct, but the extra leading table is confusing.
- **Impact**: CLI users running `powerhour render` without `--no-progress` see redundant output and may misinterpret the first table as the final state.
- **Acceptance Criteria**:
  1. Interactive progress output should occupy a single table that updates in place (no extra snapshot printed before the live refresh loop begins or when the command finishes).
  2. Non-interactive output (`--no-progress`, `--json`, or non-TTY stdout) must remain unchanged.
  3. Automated tests (unit/integration) continue to pass; add coverage if practical to guard against regressions.
- **Notes**:
  - Investigate whether the first render attempt is printing the initial state because we call `render()` before the goroutines start writing results.
  - Consider aligning fetch/render progress printers so both share the same cursor-handling logic once the fix is in.
