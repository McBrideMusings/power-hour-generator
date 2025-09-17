02_csv_loader_and_validation.md

Objective
- Load and validate powerhour.csv, preserving row order.

Features
- Accept CSV or TSV; auto-detect delimiter.
- Required headers: title,artist,start_time,duration,name,link.
- start_time: parse H:MM:SS[.ms] or M:SS[.ms].
- duration: int > 0.

Tasks
- pkg/csvplan:
  - Load(path) ([]Row, error) returning normalized rows with Index.
  - Validate with aggregated errors.
- Command: powerhour plan show → print table.

Acceptance
- Errors include row numbers.
- Order preserved; indices assigned.