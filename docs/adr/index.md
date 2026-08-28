# Architecture Decision Records

Records of decisions with genuine rejected alternatives — not a log of everything ever chosen, only the ones a future reader couldn't recover from the code alone.

- [1. Two-level hash for smart re-rendering](./0001-two-level-render-hash.md) — global config hash + per-segment hash, instead of one combined hash per segment.
- [2. Collections are opaque to orchestration logic](./0002-collections-are-opaque-to-orchestration.md) — resolution, render and display code never branch on a collection name, a literal column name, or a structural role; only `Default()` and the `init` template name concrete collections.
- [3. CLI and TUI have full parity over a shared core](./0003-cli-tui-parity-over-a-shared-core.md) — every operation is a domain-package function both front ends call; every TUI gesture ships with a `--json`-capable command in the same change.
