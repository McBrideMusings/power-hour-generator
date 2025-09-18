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

var requiredHeaders = []string{"title", "artist", "start_time", "duration", "name", "link"}

// Row represents a validated entry from the powerhour plan file.
type Row struct {
	Index           int
	Title           string
	Artist          string
	StartRaw        string
	Start           time.Duration
	DurationSeconds int
	Name            string
	Link            string
}

// Load reads a CSV/TSV file, validates its contents, and returns normalized rows.
// When validation issues are found, the returned error will be of type ValidationErrors
// and still include any successfully parsed rows to allow callers to continue working
// with the data.
func Load(path string) ([]Row, error) {
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
		rows      []Row
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

		if line == 1 {
			headerMap, err = buildHeaderMap(record)
			if err != nil {
				return nil, err
			}
			continue
		}

		if isEmptyRecord(record) {
			continue
		}

		rowIndex := len(rows) + 1
		csvLine := line
		row, rowErrs := parseRecord(record, headerMap, rowIndex, csvLine)
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

func detectDelimiter(data []byte) (rune, error) {
	// Skip UTF-8 BOM if present.
	dataStr := string(data)
	if strings.HasPrefix(dataStr, "\ufeff") {
		dataStr = strings.TrimPrefix(dataStr, "\ufeff")
	}

	newline := strings.IndexAny(dataStr, "\r\n")
	var headerLine string
	if newline == -1 {
		headerLine = dataStr
	} else {
		headerLine = dataStr[:newline]
	}

	if strings.Count(headerLine, "\t") > 0 {
		return '\t', nil
	}

	if strings.Count(headerLine, ",") > 0 {
		return ',', nil
	}

	return 0, errors.New("unable to detect delimiter (expected comma or tab)")
}

func buildHeaderMap(header []string) (map[string]int, error) {
	if len(header) == 0 {
		return nil, errors.New("header row is empty")
	}

	headerMap := make(map[string]int, len(header))
	for idx, raw := range header {
		name := normalizeHeader(raw)
		if _, exists := headerMap[name]; exists {
			return nil, fmt.Errorf("duplicate header: %s", name)
		}
		headerMap[name] = idx
	}

	for _, required := range requiredHeaders {
		if _, ok := headerMap[required]; !ok {
			return nil, fmt.Errorf("missing required header: %s", required)
		}
	}

	return headerMap, nil
}

func normalizeHeader(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "\ufeff") {
		value = strings.TrimPrefix(value, "\ufeff")
	}
	return strings.ToLower(value)
}

func isEmptyRecord(record []string) bool {
	if len(record) == 0 {
		return true
	}
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

func parseRecord(record []string, header map[string]int, index, line int) (Row, []ValidationError) {
	var errs []ValidationError

	get := func(field string) string {
		pos, ok := header[field]
		if !ok {
			return ""
		}
		if pos >= len(record) {
			errs = append(errs, ValidationError{Line: line, Field: field, Message: "missing value"})
			return ""
		}
		value := strings.TrimSpace(record[pos])
		if strings.HasPrefix(value, "\ufeff") {
			value = strings.TrimPrefix(value, "\ufeff")
		}
		return value
	}

	title := get("title")
	if title == "" {
		errs = append(errs, ValidationError{Line: line, Field: "title", Message: "title is required"})
	}

	artist := get("artist")
	if artist == "" {
		errs = append(errs, ValidationError{Line: line, Field: "artist", Message: "artist is required"})
	}

	startRaw := get("start_time")
	var startDur time.Duration
	if startRaw == "" {
		errs = append(errs, ValidationError{Line: line, Field: "start_time", Message: "start_time is required"})
	} else {
		d, err := parseStartTime(startRaw)
		if err != nil {
			errs = append(errs, ValidationError{Line: line, Field: "start_time", Message: err.Error()})
		} else {
			startDur = d
		}
	}

	durationRaw := get("duration")
	durationSeconds := 0
	if durationRaw == "" {
		errs = append(errs, ValidationError{Line: line, Field: "duration", Message: "duration is required"})
	} else {
		value, err := strconv.Atoi(durationRaw)
		if err != nil {
			errs = append(errs, ValidationError{Line: line, Field: "duration", Message: "duration must be an integer"})
		} else if value <= 0 {
			errs = append(errs, ValidationError{Line: line, Field: "duration", Message: "duration must be greater than 0"})
		} else {
			durationSeconds = value
		}
	}

	name := get("name")
	link := get("link")
	if link == "" {
		errs = append(errs, ValidationError{Line: line, Field: "link", Message: "link is required"})
	}

	row := Row{
		Index:           index,
		Title:           title,
		Artist:          artist,
		StartRaw:        startRaw,
		Start:           startDur,
		DurationSeconds: durationSeconds,
		Name:            name,
		Link:            link,
	}

	return row, errs
}

func parseStartTime(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("start_time is required")
	}

	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, fmt.Errorf("invalid start_time %q", value)
	}

	var hours, minutes int
	var err error

	if len(parts) == 2 {
		minutes, err = parseComponent("minutes", parts[0], 59)
		if err != nil {
			return 0, err
		}
	} else {
		hours, err = parseComponent("hours", parts[0], -1)
		if err != nil {
			return 0, err
		}
		minutes, err = parseComponent("minutes", parts[1], 59)
		if err != nil {
			return 0, err
		}
	}

	seconds, nanos, err := parseSeconds(parts[len(parts)-1])
	if err != nil {
		return 0, err
	}

	duration := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(nanos)
	return duration, nil
}

func parseComponent(name, raw string, max int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", name)
	}
	if max >= 0 && value > max {
		return 0, fmt.Errorf("%s must be <= %d", name, max)
	}
	return value, nil
}

func parseSeconds(raw string) (int, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, errors.New("seconds are required")
	}

	parts := strings.SplitN(raw, ".", 2)
	secInt, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, errors.New("seconds must be an integer")
	}
	if secInt < 0 || secInt > 59 {
		return 0, 0, errors.New("seconds must be between 0 and 59")
	}

	nanos := 0
	if len(parts) == 2 {
		frac := parts[1]
		if frac == "" {
			return 0, 0, errors.New("fractional seconds requires digits")
		}
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for len(frac) < 9 {
			frac += "0"
		}
		nanos64, err := strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, 0, errors.New("invalid fractional seconds")
		}
		nanos = int(nanos64)
	}

	return secInt, nanos, nil
}
