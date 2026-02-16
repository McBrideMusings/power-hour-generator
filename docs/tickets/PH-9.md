---
id: 9
title: Implement TimelineResolver
status: backlog
priority: high
tags:
  - timeline
  - phase-2
---

# Implement TimelineResolver

Build the resolver that produces an ordered list of timeline entries from the config.

## Design

Create `internal/project/timeline.go` with:

```go
type TimelineEntry struct {
    Collection  string
    Index       int
    Sequence    int
    SegmentPath string
}
```

Produces: `intro → song1 → interstitial1 → song2 → interstitial2 → ... → songN → outro`

## Acceptance Criteria

- Resolver produces correct ordered sequence for all patterns
- Interstitials cycle when fewer than insertion points
- Intro/outro play exactly once
- Missing collection references produce clear errors
