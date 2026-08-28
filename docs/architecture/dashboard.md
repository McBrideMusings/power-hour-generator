# TUI Dashboard

The `internal/tui/dashboard/` package is a full-screen, alt-screen [bubbletea](https://github.com/charmbracelet/bubbletea) app launched by `powerhour tui` (`internal/cli/tui_cmd.go`). It shows a startup spinner while detecting tools and loading project data, then hands off to an interactive, editable view of the whole project: timeline, collections, cache, and tools.

This is a different layer from the progress display covered on the [TUI System](/architecture/tui) page. That page's `StatusWriter`/`ProgressModel`/`RunWithWork` are a one-shot table shown while a single command (`fetch`, `render`) runs to completion. The dashboard is a persistent, navigable app the user drives with the keyboard across an entire session — it does not use `ProgressModel`.

## Top-level model

`Model` in `model.go` holds the loaded project state: config, project paths, resolved collections, timeline, cache index, and render state. It owns:

- **View switching** — which of the top-level views (timeline, collections, cache, tools) is active.
- **Interaction mode** — what kind of input, if any, is currently being captured.
- **Job state** — an active async fetch/render job and its event channel, if one is running.

`Model.Update` is the bubbletea entry point. Key events fan out through `handleKey`, which first checks for an active overlay (help, cache doctor), then dispatches by interaction mode, then — in the default (`modeNormal`) case — by the active view. Rendering (`View()`) delegates to each sub-view's own render method plus shared chrome (header, footer, help row).

## View set

`Model.viewKind(idx)` maps a view index to a view type:

| Index | View |
|---|---|
| `0` | timeline |
| `1..len(collectionNames)` | one per collection, sorted by name |
| `len(collectionNames)+1` | cache |
| `len(collectionNames)+2` | tools |

`←`/`→` step between views; number keys jump directly. Each view type has its own file:

- **`timeline_view.go`** — two panels over one project: a read-only, fixed-height TIMELINE SEQUENCE panel (`timeline.sequence` from `powerhour.yaml` — genuinely editable only there, `e` opens it) and a mutable PLAYBACK ORDER panel showing what `finalize` would actually produce, resolved from the playback order via the same `playback.ResolveOrder`/`playback.Placements` call `finalize` itself uses (ADR 0003). The order panel's `s`/`l`/`S`/`Esc` gestures (swap/pick, lock, shuffle, clear mark) call `internal/playback` directly and hold no logic of their own (ADR 0003) — a `once` collection's slot is marked then swapped against another slot of the same collection, a `repeat` collection's slot opens the `overlayPicker` overlay (`order_picker.go`) over its pool. Also renders the finalized output path/status.
- **`collection_view.go`** — one table per collection with dynamic columns discovered from the plan data, row-state color coding (rendered / not-rendered / not-cached), and a persistent "add clip" slot at the bottom.
- **`cache_view.go`** — the yt-dlp cache index, with a filtered/all toggle, configurable columns driven by `cache.view.columns` in config, and a persistent "add" slot for registering a URL, YouTube ID, or local file directly into the cache.
- **`tools_view.go`** — detected tool versions, install methods, and update status.

Shared chrome lives in `header.go`, `footer.go`, `help_row.go`, `overlay.go`, and `styles.go`. `row_render.go` provides `renderCell`/`renderEditCell`/`renderEditField` — the low-level helpers every view uses to truncate, pad, and (during inline edit) cursor-highlight a table cell without letting ANSI styling bytes break column alignment.

## Interaction modes

`interactionMode` (`model.go`) is the state machine governing what keypresses mean. Exactly one mode is active at a time (plus an independent overlay layer for help/doctor, checked first in `handleKey`):

| Mode | Entered by | Exited by | Handler |
|---|---|---|---|
| `modeNormal` | default state; return here after any other mode completes | — | `handleKey`'s default view-dispatch path |
| `modeInput` | starting a free-text prompt (e.g. renaming, confirming a value) | submit or cancel | `handleInputKey` (`input.go`) |
| `modeConfirmDelete` | pressing delete on a row | confirm (`y`/enter) or cancel | `handleConfirmDeleteKey` |
| `modeInlineEdit` | editing a collection row's field in place | save (advances field or exits) or cancel | `handleInlineEditKey` |
| `modeCacheInlineEdit` | editing a cache entry's field in place | save or cancel | `handleCacheInlineEditKey` |
| `modeAddClip` | focusing the collection's add-clip slot | add + stay (ready for another paste), enter inline-edit on an empty required field, or cancel | `handleAddClipKey` |
| `modeAddCache` | focusing the cache view's add-slot (URL, YouTube ID, or local file path) | dispatch a background registration job or cancel | `handleAddCacheKey` |

The timeline's PLAYBACK ORDER panel has no dedicated mode of its own — `s`/`l`/`S`/`Esc` are handled directly in `modeNormal` by `handleTimelineKeyWithMutations`, and a `repeat` collection's `s` opens the `overlayPicker` overlay instead of entering a new mode.

Every mode transition is local, synchronous state on `Model` — there is no separate router package. The mode value alone determines which handler `handleKey` calls; view code never re-checks it. This is state describing what the user is doing, not a keybinding table — see `.claude/CLAUDE.md`'s "TUI Dashboard" section for the actual key surface, which is out of scope for this page.

## Write-back paths

The dashboard writes to disk immediately on every mutation and reloads from what it just wrote — the plan files and config stay the single source of truth, never an in-memory-only cache of one.

- **Collection rows** (add/edit/delete/reorder): `writeCollection` (or, after a full row addition, `reindexAndWriteCollection`) serializes the collection's rows via `project.WriteCollectionPlan`, which writes CSV or YAML depending on `Collection.PlanFormat` — for CSV, `csvplan.MergeHeaders` first folds any newly-discovered fields into the header list. `reloadCollection` then re-reads the file back into `Model` so the in-memory rows match what's on disk (and row states get recomputed).
- **Playback order edits** (swap/set/lock/shuffle): `persistOrder` calls `playback.Save(m.pp.Root, m.order)` to persist `playback-order.yaml`, then `syncPlaybackOrder` re-resolves through `playback.ResolveOrder`/`playback.Placements` — the same function `finalize` uses — so the panel and `finalize` never disagree. `reResolve` and `NewModel` both call `syncPlaybackOrder` too, so every path that (re)builds the model resolves the order the same way.
- **Cache edits** (inline edit, cache doctor apply): `saveCacheEdit` and `applyCurrentDoctorEntry` write through `cache.Save(m.pp, idx)`, persisting `.powerhour/index.json`.
- **Render/fetch jobs**: on completion, `rs.Save(pp.RenderStateFile)` persists the updated render state (`.powerhour/render-state.json`) so hash-based skip logic in a later `render` run sees what the dashboard already did.

## Async side channels

Several dashboard features run off the bubbletea main loop and report back through messages or a dedicated channel:

- **`probe.go`** — after adding a URL, an async `yt-dlp --dump-json` call fills in title/artist (gated per collection by `collectionHasField`) and reports back as a `metadataProbeMsg`.
- **`song_lookup.go`** — `setCacheEntryField` and cache-backed suggestions for the add-clip slot.
- **`cache_doctor.go`** — the `overlayDoctor` overlay: inline metadata review/correction with fuzzy artist autocomplete and an async yt-dlp requery (`Ctrl+R`). `view()` allocates its `termHeight-5` line budget by priority rather than tail-chopping: edit fields, confidence reasons, and requery status always render; the optional context fields (uploader/channel/track/album) are dropped first, the artist suggestion list is capped second, and any reduction leaves an inline `hidden`/`… N more` marker.
- **`order_picker.go`** — the `overlayPicker` overlay: a type-to-filter list over a `repeat`-selection collection's pool, opened by `s` on a PLAYBACK ORDER slot. Renders in the content area, following `overlayDoctor` rather than the full-screen `overlayHelp`. Enter applies the highlighted row via `playback.Set`; the overlay itself holds no domain logic — filtering is presentational only.
- **`vlc.go`** — VLC playback integration (single item, playlist, or untrimmed source for scrubbing), driven via `open -a VLC` / `osascript` on macOS.
- **`tea.ExecProcess`** shell-outs — the timeline view's `r` and the collection views' `r`/`R` hand off to the real CLI subprocess (`finalize` on the timeline, `render` on a collection), then reload state on return.
- **`dashboardJobEvent`** — the channel type used by background fetch/render jobs (`runDashboardFetchJob`, `runDashboardRenderJob`); `Model.drainJobEvents` pulls events off it each tick to update per-row job status without blocking the UI.

## See also

- [TUI System](/architecture/tui) — the progress-display layer used by single-shot commands, distinct from this dashboard.
- [Cache System](/architecture/cache) — the index and service the cache view and cache doctor read from and write to.
- [Config System](/architecture/config) — the YAML config the timeline view edits and `config.Save` persists.
