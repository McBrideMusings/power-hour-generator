07_incremental_builds.md

Objective
- Avoid re-rendering unchanged segments when only order changes.

Tasks
- Extend .powerhour/index.json to include:
  - hash of CSV row fields (title, artist, start_time, duration, name, link)
  - hash of config relevant to rendering
  - output file path
- On render:
  - If cache hit and output already exists with matching hashes, skip render
  - Else re-render and update index.json
- Command flags:
  - --force (re-render all)
  - --changed-only (default)

Acceptance
- Reordering CSV only regenerates filenames if necessary
- Skipped jobs are logged as "cached"