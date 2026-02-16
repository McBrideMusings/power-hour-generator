---
id: 1
title: Remove legacy clips + overrides system
status: done
priority: high
tags:
  - cleanup
  - phase-0
---

# Remove Legacy Clips + Overrides System

Remove the fully implemented but unused legacy clips + overrides architecture that has been superseded by the collections system.

## What to Remove

**Config structs** (`internal/config/config.go`):
- `ClipsConfig` and all child structs
- `ClipOverride`, `ClipMatch`, `ClipRenderOverride`, `ClipOverlayOverride`
- All `merge*` functions for clip configs
- `normalizeClipOverrides`
- The `Clips` field on the root `Config` struct

**Resolver** (`internal/project/resolver.go`):
- `SegmentOverride` struct
- `applyOverrides` and `matchesOverride`
- The `overrides` field on `Resolver`
- Evaluate if `resolver.go` still serves a purpose or if `collections.go` has fully replaced it

**Config YAML**: Remove the `clips:` block from sample configs, README examples, and docs.

## Acceptance Criteria

- No references to `clips.song`, `clips.interstitial`, `clips.overrides`, or `ClipOverride` remain
- Code builds clean, vet clean, all tests pass
