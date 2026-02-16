# Spec: Index Badge at Concat Time

> Status: Deferred. Revisit once Phase 3 (Concatenation) is complete.

## Problem

The default overlay config includes a persistent `{index}` badge burned into each segment during render. If the user reorders rows in the CSV, every segment whose index changed needs re-rendering — even though the video content, trim, and all other overlays are identical. For a 60-song power hour, moving one song from position 5 to position 30 triggers re-rendering of 26 segments just to update a number.

## Idea

Instead of burning the index badge during segment render, burn it during concatenation. Segments would be rendered without the index overlay. The concat step would apply the index as a per-segment overlay based on the segment's position in the timeline.

This means reordering only requires re-running concat (fast with stream copy? no — overlay requires re-encode of at least the affected frames) rather than re-rendering individual segments.

## Trade-offs

**Pros:**
- Reordering is cheaper (no per-segment re-render)
- Segments are position-independent and more reusable

**Cons:**
- Concat can no longer be a simple stream copy — it needs per-segment overlay injection, which means re-encoding
- The concat step becomes significantly more complex (not just `-c copy`, but a filter graph per segment)
- Users configure overlay timing/position per-profile, and the index badge shares that config. Splitting it out requires special-casing which overlays are "segment-time" vs "concat-time"
- Preview of a single segment won't show the index badge (misleading)

## Conclusion

The complexity cost is high and the benefit is narrow (only matters when reordering). The current approach (re-render affected segments on reorder) is acceptable. If re-rendering speed becomes a bottleneck, revisit this or consider lighter optimizations first (faster FFmpeg preset for re-renders, only re-render the index overlay layer).
