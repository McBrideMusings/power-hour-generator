package csvplan

import (
	"bytes"
	"encoding/csv"
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
	LinkHeader     string // CSV column name for video link
	StartHeader    string // CSV column name for start time
	DurationHeader string // CSV column name for duration (optional)
	DefaultDuration int    // Fallback duration if not specified
}

// CollectionRow represents a single clip from a collection plan with dynamic fields.
type CollectionRow struct {
	Index           int               // 1-based row index
	Link            string            // Video link (required)
	StartRaw        string            // Raw start time string
	Start           time.Duration     // Parsed start time
	DurationSeconds int               // Clip duration in seconds
	CustomFields    map[string]string // All CSV columns as key-value pairs
}

// LoadCollection reads a CSV with configurable headers for a collection.
func LoadCollection(path string, opts CollectionOptions) ([]CollectionRow, error) {
	// Normalize header names
	opts.LinkHeader = normalizeHeader(opts.LinkHeader)
	opts.StartHeader = normalizeHeader(opts.StartHeader)
	if opts.DurationHeader != "" {
		opts.DurationHeader = normalizeHeader(opts.DurationHeader)
	}

	// Validate protected headers
	protectedHeaders := map[string]bool{"index": true, "id": true}
	if protectedHeaders[opts.LinkHeader] {
		return nil, fmt.Errorf("link_header cannot be %q (protected name)", opts.LinkHeader)
	}
	if protectedHeaders[opts.StartHeader] {
		return nil, fmt.Errorf("start_header cannot be %q (protected name)", opts.StartHeader)
	}
	if opts.DurationHeader != "" && protectedHeaders[opts.DurationHeader] {
		return nil, fmt.Errorf("duration_header cannot be %q (protected name)", opts.DurationHeader)
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

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	if len(data) == 0 {
		return nil, errors.New("plan file is empty")
	}

	comma, err := detectDelimiter(data)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("parse file: %w", err)
		}
		line++

		record = trimTrailingFields(record)

		if line == 1 {
			headerMap, err = buildCollectionHeaderMap(record, opts)
			if err != nil {
				return nil, err
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
		return nil, errors.New("missing header row")
	}

	if len(rows) == 0 {
		return nil, errors.New("no data rows found")
	}

	if len(errs) > 0 {
		return rows, errs
	}

	return rows, nil
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
		Link:            link,
		StartRaw:        startRaw,
		Start:           startDur,
		DurationSeconds: durationSeconds,
		CustomFields:    customFields,
	}

	return row, errs
}

// ToRow converts a CollectionRow to a standard Row for compatibility with existing systems.
func (cr CollectionRow) ToRow() Row {
	return Row{
		Index:           cr.Index,
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
