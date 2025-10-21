04_project_cache_fetch.md

Objective
- Populate project-local cache/ from CSV.

Rules
- URL: download with yt-dlp into cache/.
- Local path: copy (or hardlink) into cache/.
- Naming:
  - key = SHA256(link or abs path)
  - File: {index:03}_{key[0:10]}{ext}
- Maintain .powerhour/index.json mapping rows → cached path, metadata.

Tasks
- internal/cache:
  - Resolve(row)
  - FetchURL(row)
  - FetchLocal(row)
  - Probe(path) with ffprobe.
  - Read/write index.json.
- Command: powerhour fetch [--force] [--reprobe].

Acceptance
- Cached videos not re-downloaded.
- index.json updated with metadata.
