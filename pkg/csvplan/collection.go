package csvplan

import (
	"bytes"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// CollectionOptions controls how a collection CSV is loaded with configurable headers.
type CollectionOptions struct {
	LinkHeader      string // CSV column name for video link
	StartHeader     string // CSV column name for start time
	DurationHeader  string // CSV column name for duration (optional)
	DefaultDuration int    // Fallback duration if not specified
}

// CollectionRow represents a single clip from a collection plan with dynamic fields.
type CollectionRow struct {
	Index           int               // 1-based row index
	RowID           string            // Stable per-row id (6 hex chars), survives reordering/edits
	Link            string            // Video link (required)
	StartRaw        string            // Raw start time string
	Start           time.Duration     // Parsed start time
	DurationSeconds int               // Clip duration in seconds
	CustomFields    map[string]string // All CSV columns as key-value pairs
}

// LoadCollection reads a CSV with configurable headers for a collection. Any
// row lacking a stable id is assigned one and written back to path (so a row
// hand-added in an external editor gets an id on next load).
func LoadCollection(path string, opts CollectionOptions) ([]CollectionRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	rows, assigned, err := loadCollectionData(data, opts)
	if assigned > 0 && len(rows) > 0 {
		if headers, delimiter, herr := ReadHeaders(path); herr == nil {
			_ = WriteCSV(path, headers, rows, delimiter)
		}
	}
	return rows, err
}

// LoadCollectionData reads a collection plan from raw CSV/TSV bytes. Missing
// ids are assigned in memory but never written back — the caller (paste/import
// flows) has no path to write to.
func LoadCollectionData(data []byte, opts CollectionOptions) ([]CollectionRow, error) {
	rows, _, err := loadCollectionData(data, opts)
	return rows, err
}

func loadCollectionData(data []byte, opts CollectionOptions) ([]CollectionRow, int, error) {
	// Normalize header names
	opts.LinkHeader = normalizeHeader(opts.LinkHeader)
	opts.StartHeader = normalizeHeader(opts.StartHeader)
	if opts.DurationHeader != "" {
		opts.DurationHeader = normalizeHeader(opts.DurationHeader)
	}

	// Validate protected headers
	protectedHeaders := map[string]bool{"index": true, "id": true}
	if protectedHeaders[opts.LinkHeader] {
		return nil, 0, fmt.Errorf("link_header cannot be %q (protected name)", opts.LinkHeader)
	}
	if protectedHeaders[opts.StartHeader] {
		return nil, 0, fmt.Errorf("start_header cannot be %q (protected name)", opts.StartHeader)
	}
	if opts.DurationHeader != "" && protectedHeaders[opts.DurationHeader] {
		return nil, 0, fmt.Errorf("duration_header cannot be %q (protected name)", opts.DurationHeader)
	}

	// Apply defaults
	if opts.LinkHeader == "" {
		opts.LinkHeader = "link"
	}
	if opts.StartHeader == "" {
		opts.StartHeader = "start_time"
	}
	if opts.DefaultDuration <= 0 {
		opts.DefaultDuration = 60
	}

	if len(data) == 0 {
		return nil, 0, errors.New("plan file is empty")
	}

	comma, err := detectDelimiter(data)
	if err != nil {
		return nil, 0, err
	}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = comma
	reader.FieldsPerRecord = -1

	var (
		rows      []CollectionRow
		errs      ValidationErrors
		headerMap map[string]int
		line      = 0
	)

	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, 0, fmt.Errorf("parse file: %w", err)
		}
		line++

		record = trimTrailingFields(record)

		if line == 1 {
			headerMap, err = buildCollectionHeaderMap(record, opts)
			if err != nil {
				return nil, 0, err
			}
			continue
		}

		record = trimTrailingFields(record)

		if isEmptyRecord(record) {
			continue
		}

		rowIndex := len(rows) + 1
		csvLine := line
		row, rowErrs := parseCollectionRecord(record, headerMap, rowIndex, csvLine, opts)
		if len(rowErrs) > 0 {
			errs = append(errs, rowErrs...)
		}
		rows = append(rows, row)
	}

	if headerMap == nil {
		return nil, 0, errors.New("missing header row")
	}

	assigned := assignRowIDs(rows)

	if len(errs) > 0 {
		return rows, assigned, errs
	}

	return rows, assigned, nil
}

// assignRowIDs enforces the invariant the playback order depends on: every
// row carries an id, and no two rows share one. A row with no id gets one; a
// row whose id is already taken by an earlier row gets a fresh one, the
// earlier row keeping the original so existing slots, locks and render state
// stay pointed at the same row. The id is mirrored into CustomFields["id"]
// so the existing writers persist it. Returns the number of rows changed.
//
// Repairing duplicates is not defensive tidying: (collection, id) IS a slot's
// identity, so two rows sharing an id are one row to the playback order, the
// position index and render state. They collapse into a single slot, the
// extras become unreachable — unselectable in the order, never rendered — and
// their segments overwrite each other. Any path can produce one: a duplicate
// gesture that copies the id field, or a plan hand-edited by copy-paste.
func assignRowIDs(rows []CollectionRow) int {
	if len(rows) == 0 {
		return 0
	}

	seen := make(map[string]bool, len(rows))
	changed := 0
	for i := range rows {
		if rows[i].RowID != "" && !seen[rows[i].RowID] {
			seen[rows[i].RowID] = true
			continue
		}
		id := newRowID(seen)
		seen[id] = true
		rows[i].RowID = id
		if rows[i].CustomFields == nil {
			rows[i].CustomFields = make(map[string]string, 1)
		}
		rows[i].CustomFields["id"] = id
		changed++
	}
	return changed
}

// newRowID generates a 6-hex-char id not already present in seen.
func newRowID(seen map[string]bool) string {
	for {
		buf := make([]byte, 3)
		if _, err := rand.Read(buf); err != nil {
			// crypto/rand.Read does not fail on supported platforms; if it
			// somehow does, fall back to a fixed collision-checked attempt
			// so we never return an empty id.
			buf = []byte{0, 0, 0}
		}
		id := hex.EncodeToString(buf)
		if !seen[id] {
			return id
		}
	}
}

func buildCollectionHeaderMap(header []string, opts CollectionOptions) (map[string]int, error) {
	if len(header) == 0 {
		return nil, errors.New("header row is empty")
	}

	headerMap := make(map[string]int, len(header))
	for idx, raw := range header {
		name := normalizeHeader(raw)
		if name == "" {
			continue
		}
		if _, exists := headerMap[name]; exists {
			return nil, fmt.Errorf("duplicate header: %s", name)
		}
		headerMap[name] = idx
	}

	// Validate required headers exist
	if _, ok := headerMap[opts.LinkHeader]; !ok {
		return nil, fmt.Errorf("missing required header: %s", opts.LinkHeader)
	}
	if _, ok := headerMap[opts.StartHeader]; !ok {
		return nil, fmt.Errorf("missing required header: %s", opts.StartHeader)
	}

	return headerMap, nil
}

func parseCollectionRecord(record []string, header map[string]int, index, line int, opts CollectionOptions) (CollectionRow, []ValidationError) {
	var errs []ValidationError

	get := func(field string) string {
		pos, ok := header[field]
		if !ok {
			return ""
		}
		if pos >= len(record) {
			return ""
		}
		value := strings.TrimSpace(record[pos])
		if strings.HasPrefix(value, "\ufeff") {
			value = strings.TrimPrefix(value, "\ufeff")
		}
		return value
	}

	// Get required fields
	link := get(opts.LinkHeader)
	if link == "" {
		errs = append(errs, ValidationError{Line: line, Field: opts.LinkHeader, Message: fmt.Sprintf("%s is required", opts.LinkHeader)})
	}

	startRaw := get(opts.StartHeader)
	var startDur time.Duration
	if startRaw == "" {
		errs = append(errs, ValidationError{Line: line, Field: opts.StartHeader, Message: fmt.Sprintf("%s is required", opts.StartHeader)})
	} else {
		d, err := parseStartTime(startRaw)
		if err != nil {
			errs = append(errs, ValidationError{Line: line, Field: opts.StartHeader, Message: err.Error()})
		} else {
			startDur = d
		}
	}

	// Get duration (optional with default)
	durationSeconds := opts.DefaultDuration
	if opts.DurationHeader != "" {
		if _, hasDuration := header[opts.DurationHeader]; hasDuration {
			durationRaw := get(opts.DurationHeader)
			if strings.TrimSpace(durationRaw) != "" {
				value, err := strconv.Atoi(durationRaw)
				if err != nil {
					errs = append(errs, ValidationError{Line: line, Field: opts.DurationHeader, Message: "duration must be an integer"})
				} else if value <= 0 {
					errs = append(errs, ValidationError{Line: line, Field: opts.DurationHeader, Message: "duration must be greater than 0"})
				} else {
					durationSeconds = value
				}
			}
		}
	}

	if durationSeconds <= 0 {
		errs = append(errs, ValidationError{Line: line, Field: "duration", Message: "duration must be greater than 0"})
	}

	// Collect all fields as custom fields
	customFields := make(map[string]string)
	for headerName, pos := range header {
		if pos < len(record) {
			value := strings.TrimSpace(record[pos])
			if value != "" {
				customFields[headerName] = value
			}
		}
	}

	row := CollectionRow{
		Index:           index,
		RowID:           get("id"),
		Link:            link,
		StartRaw:        startRaw,
		Start:           startDur,
		DurationSeconds: durationSeconds,
		CustomFields:    customFields,
	}

	return row, errs
}

// ReadHeaders reads just the first line of a CSV/TSV file and returns the raw
// header names (normalized) and the detected delimiter. This is used by the
// write-back path to preserve column order and delimiter.
func ReadHeaders(path string) (headers []string, delimiter rune, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read file: %w", err)
	}
	if len(data) == 0 {
		return nil, 0, errors.New("plan file is empty")
	}

	delimiter, err = detectDelimiter(data)
	if err != nil {
		return nil, 0, err
	}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1

	record, err := reader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("read header: %w", err)
	}

	headers = make([]string, 0, len(record))
	for _, raw := range record {
		name := normalizeHeader(raw)
		if name != "" {
			headers = append(headers, name)
		}
	}

	return headers, delimiter, nil
}

// ToRow converts a CollectionRow to a standard Row for compatibility with existing systems.
func (cr CollectionRow) ToRow() Row {
	return Row{
		Index:           cr.Index,
		RowID:           cr.RowID,
		Title:           cr.CustomFields["title"],
		Artist:          cr.CustomFields["artist"],
		StartRaw:        cr.StartRaw,
		Start:           cr.Start,
		DurationSeconds: cr.DurationSeconds,
		Name:            cr.CustomFields["name"],
		Link:            cr.Link,
		CustomFields:    cr.CustomFields,
	}
}
