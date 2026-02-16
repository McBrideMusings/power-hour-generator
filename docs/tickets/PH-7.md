---
id: 7
title: Tool detection performance optimization
status: done
priority: high
tags:
  - performance
  - phase-1
---

# Tool Detection Performance Optimization

Optimized tool detection from ~20s to ~0.5s in steady state.

## What Was Built

- **`EnsureAll()`** — Single `Detect()` call for all tools instead of per-tool `Ensure()` calls
- **Checksum-based manifest trust** — Skips `yt-dlp --version` shell-out (6-7s) when binary SHA-256 matches manifest
- **Release cache** (`release_cache.json`) — Caches GitHub API responses for `minimum_version: latest` with 1-hour TTL
- **Version-qualified downloads** — Filenames include version (e.g. `yt-dlp_macos.2026.02.04`) to prevent stale binary reuse
- **`SkipInitialCheck`** — Install skips redundant pre-lock Detect when called from EnsureAll
