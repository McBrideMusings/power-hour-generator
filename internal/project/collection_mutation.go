package project

import (
	"strconv"
	"strings"
	"time"

	"powerhour/internal/cache"
	"powerhour/pkg/csvplan"
)

// CollectionOptionsForConfig converts collection config into csvplan options.
func CollectionOptionsForConfig(cfg Collection) csvplan.CollectionOptions {
	defaultDuration := 60
	if cfg.Config.Duration > 0 {
		defaultDuration = cfg.Config.Duration
	}
	return csvplan.CollectionOptions{
		LinkHeader:      cfg.Config.LinkHeader,
		StartHeader:     cfg.Config.StartHeader,
		DurationHeader:  cfg.Config.DurationHeader,
		DefaultDuration: defaultDuration,
	}
}

// AppendCollectionRows appends imported rows, reindexes the combined set, and
// expands CSV headers to preserve any new dynamic fields.
func AppendCollectionRows(coll Collection, imported []csvplan.CollectionRow) Collection {
	rows := make([]csvplan.CollectionRow, 0, len(coll.Rows)+len(imported))
	rows = append(rows, coll.Rows...)
	rows = append(rows, imported...)
	for i := range rows {
		rows[i].Index = i + 1
	}
	coll.Rows = rows
	coll.Headers = csvplan.MergeHeaders(coll.Headers, rows)
	return coll
}

// DedupeImportedRows filters imported rows whose link already matches an
// existing row in the collection, using the same YouTube video-ID extraction
// the cache index uses to canonicalize links (cache.ExtractYouTubeID) — so
// re-pasting the same sheet still matches even if tracking params or v=
// ordering differ. Links that aren't recognized YouTube URLs (other hosts,
// local paths) fall back to an exact trimmed-link comparison. Duplicates
// within the imported batch itself are also collapsed. Returns the rows that
// are actually new, plus how many were skipped as duplicates.
func DedupeImportedRows(coll Collection, imported []csvplan.CollectionRow) ([]csvplan.CollectionRow, int) {
	keyFor := func(link string) string {
		link = strings.TrimSpace(link)
		if link == "" {
			return ""
		}
		if id := cache.ExtractYouTubeID(link); id != "" {
			return "yt:" + id
		}
		return link
	}

	seen := make(map[string]struct{}, len(coll.Rows)+len(imported))
	for _, row := range coll.Rows {
		if key := keyFor(row.Link); key != "" {
			seen[key] = struct{}{}
		}
	}

	deduped := make([]csvplan.CollectionRow, 0, len(imported))
	skipped := 0
	for _, row := range imported {
		key := keyFor(row.Link)
		if key == "" {
			deduped = append(deduped, row)
			continue
		}
		if _, dup := seen[key]; dup {
			skipped++
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, row)
	}
	return deduped, skipped
}

// BuildCollectionRow initializes a single collection row from the collection's
// schema defaults, overriding the link field with the provided source.
func BuildCollectionRow(coll Collection, link string) csvplan.CollectionRow {
	opts := CollectionOptionsForConfig(coll)

	defaultDur := opts.DefaultDuration
	if defaultDur <= 0 {
		defaultDur = 60
	}

	linkHeader := opts.LinkHeader
	if linkHeader == "" {
		linkHeader = "link"
	}
	startHeader := opts.StartHeader
	if startHeader == "" {
		startHeader = "start_time"
	}
	durationHeader := opts.DurationHeader
	if durationHeader == "" {
		durationHeader = "duration"
	}

	fields := make(map[string]string, len(coll.Defaults)+3)
	for k, v := range coll.Defaults {
		fields[k] = v
	}

	startRaw := fields[startHeader]
	start := time.Duration(0)
	if startRaw == "" {
		startRaw = "0:00"
		fields[startHeader] = startRaw
	}
	if parsed, err := csvplan.ParseStartTime(startRaw); err == nil {
		start = parsed
	}

	durationRaw := fields[durationHeader]
	durationSeconds := defaultDur
	if durationRaw == "" {
		durationRaw = strconv.Itoa(durationSeconds)
		fields[durationHeader] = durationRaw
	} else if parsed, err := strconv.Atoi(durationRaw); err == nil && parsed > 0 {
		durationSeconds = parsed
	}

	fields[linkHeader] = link

	return csvplan.CollectionRow{
		Link:            link,
		StartRaw:        startRaw,
		Start:           start,
		DurationSeconds: durationSeconds,
		CustomFields:    fields,
	}
}

// DuplicateCollectionRow appends a deep copy of the selected row to the end of
// the collection and reindexes the full result.
func DuplicateCollectionRow(coll Collection, rowIdx int) Collection {
	if rowIdx < 0 || rowIdx >= len(coll.Rows) {
		return coll
	}

	row := coll.Rows[rowIdx]
	dup := csvplan.CollectionRow{
		Link:            row.Link,
		StartRaw:        row.StartRaw,
		Start:           row.Start,
		DurationSeconds: row.DurationSeconds,
		CustomFields:    make(map[string]string, len(row.CustomFields)),
	}
	for k, v := range row.CustomFields {
		dup.CustomFields[k] = v
	}
	// The id is the row's identity, not its content. Carrying it over would
	// make the copy the same row as its source everywhere the playback order,
	// the position index and render state are concerned — one slot, one
	// segment, and the copy unreachable. RowID is already left empty; the id
	// custom field has to go with it, or the write-back puts it back.
	delete(dup.CustomFields, "id")

	return AppendCollectionRows(coll, []csvplan.CollectionRow{dup})
}

// WriteCollectionPlan persists a collection back to its configured plan file.
func WriteCollectionPlan(coll Collection) error {
	if coll.PlanFormat == "yaml" {
		return csvplan.WriteYAML(coll.Plan, coll.Headers, coll.Defaults, coll.Rows)
	}
	delimiter := coll.Delimiter
	if delimiter == 0 {
		delimiter = ','
	}
	headers := coll.Headers
	if len(headers) == 0 {
		headers = csvplan.MergeHeaders(nil, coll.Rows)
	}
	return csvplan.WriteCSV(coll.Plan, headers, coll.Rows, delimiter)
}
