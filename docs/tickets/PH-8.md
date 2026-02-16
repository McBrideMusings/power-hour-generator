---
id: 8
title: Define timeline config structs
status: backlog
priority: high
tags:
  - timeline
  - phase-2
---

# Define Timeline Config Structs

Add timeline configuration to the YAML config that defines how collections interleave into a playback timeline.

## Config Design

```yaml
timeline:
  sequence:
    - collection: intro
      count: 1
    - collection: songs
      interleave:
        collection: interstitials
        every: 1
    - collection: outro
      count: 1
```

## Interleave Options

- `every: N` — insert interstitial after every N songs
- Interstitials cycle if fewer than insertion points
- `count: 1` — play exactly once (intro/outro)
- No `interleave` — collection plays straight through

## Acceptance Criteria

- Timeline config structs defined in `internal/config/`
- Config parsing handles all interleave options
- Invalid timeline configs produce clear errors
