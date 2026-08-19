# 1. Two-level hash for smart re-rendering (global config + per-segment)

Date: 2026-08-19

## Status

Accepted

## Context

Smart re-rendering needs to detect two different kinds of change that invalidate cached segments differently:

- A global encoding/config change (video codec, resolution, fps, crf, loudnorm, etc.) invalidates *every* segment at once.
- A per-row change (CSV field, overlay profile, fade, filename template) invalidates *only that segment*.

## Decision

Use two separate hashes rather than one combined hash per segment:

- `GlobalConfigHash` (`internal/render/state/hash.go`) — one hash over the video/audio/encoding config sections, stored once on the render state root.
- `SegmentInputHash` (`internal/render/hash.go`) — one hash per segment over its CSV row fields, overlay profile, fade, and filename template.

`DetectChanges` (`internal/render/state/detect.go:41-47`) checks the global hash first: a mismatch marks every segment for re-render in one comparison (`ReasonConfigChanged`), before ever touching per-segment state. Only when the global hash still matches does it fall through to comparing each segment's own hash individually.

## Rejected alternative

A single combined hash per segment, folding the global config into each segment's hash input. This would still functionally detect a config change (the config bytes are part of every segment's hash), but loses the O(1) global-invalidate path — every segment's hash has to be recomputed and compared individually just to discover the same global change N times, once per segment, instead of once.

## Consequences

- Config changes invalidate cheaply and uniformly; no risk of a segment silently surviving a config change because its own hash computation missed a field.
- Two hash functions to keep in sync when new config fields are added — a field added to encoding config but not `GlobalConfigHash`'s input struct silently fails to invalidate.
