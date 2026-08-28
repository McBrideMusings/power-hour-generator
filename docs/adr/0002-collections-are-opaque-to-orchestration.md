# 2. Collections are opaque to orchestration logic

Date: 2026-08-28

## Status

Accepted

## Context

A power hour is not a fixed shape. The project already lets a user configure how many
clips there are, which pools they come from, how those pools interleave, how often an
interleaved pool repeats, where hardcoded files are spliced into the sequence, and what
overlays each pool renders. `songs` and `interstitials` are two names a user happened to
type into `powerhour.yaml`. A different project might define `rounds`, `stingers`,
`ad_breaks`, and `bumpers`, or a single pool with no interleave at all.

Every abstraction in the config is worth exactly as much as the code that consumes it.
Orchestration code that reads a literal collection name, a literal column name, or a
structural role and then behaves differently converts a configuration point back into a
hardcoded one, silently. The config still *accepts* the value; the system stops honoring
it.

Two live examples, both found while designing the playback-order swap/lock/shuffle work:

- `timelineView.entryLabel` (`internal/tui/dashboard/timeline_view.go:401`) builds a row's
  display name from a fixed ladder over the literal keys `"title"` and `"artist"`, falling
  back to the raw link basename. A collection that declares neither — an interstitials pool
  with `columns: [link, start_time, duration]` — has no way to be legible in the playback
  order, no matter what it puts in its plan file.
- `BuildTimelinePlacements` (`internal/project/timeline_plan.go:66,79`) decides whether a
  pool repeats by its *structural role*: the primary collection gets a monotonic cursor and
  runs out, the interleave collection gets a modulo cursor and cycles forever. "Songs don't
  repeat, interstitials do" is a real product rule, and it exists only as the difference
  between two cursor expressions.

## Decision

**Orchestration logic treats a collection as opaque.** Resolution, render, concat, and
display code may not branch on a collection's name, on a literal column name, or on the
structural position a collection occupies in the timeline. Anything that would be such a
branch is instead a declared field in `powerhour.yaml`, read from `CollectionConfig`,
`SequenceEntry`, or `InterleaveConfig`.

The boundary is **generator versus template**:

- `config.Default()` and `init.go`'s `defaultConfigYAML` are templates. They may — and
  should — name `songs` and `interstitials`, set `display: "{title} – {artist}"`, and write
  every other opinionated starting value. That is their entire job.
- Everything downstream is a generator. It reads whatever the config says and never
  assumes what it will find.

A consumer that needs a per-collection behavior adds the field rather than inferring it.
Where a field is absent, the fallback is a documented default in the config layer, not a
special case in the consumer.

## Rejected alternative

Keep the inferences and document them. Primary-is-once and interleave-is-repeat is correct
for every project anyone has built so far, and `title`/`artist` is correct for every music
collection. Adding config fields for both costs schema surface, validation, `init`
template lines, and migration of existing projects, to express rules nobody has yet needed
to vary.

Rejected because the cost lands as a wall, not a slope. Each inference is individually
defensible and cheap to keep; together they mean the config describes a system that no
longer exists. The failure mode is not an error — it is a value in `powerhour.yaml` that
looks honored and is not, which is strictly worse than a field that was never offered.

## Consequences

- New per-collection behavior arrives as a config field with a default, not as a branch.
  This is a review criterion: a literal `"title"`, `"songs"`, or a primary-vs-interleave
  test in resolution, render, or display code is a defect.
- `ValidateStrict` (`internal/config/validation.go`) grows as the schema does — a
  `display` template naming a column the collection never declares is a config error, and
  it is the config layer's job to say so.
- Existing projects need migration when a field is introduced, since the previously
  inferred behavior becomes something that must be written down. There is no
  backward-compatible inference path; per the project's no-backwards-compatibility rule,
  the fix is to migrate the project forward.
- The cache layer's `field_map` (`internal/config/config.go:176`) is the pattern this ADR
  generalizes. It already resolves canonical columns through declared, per-collection
  fallback lists rather than hardcoded yt-dlp field names.
