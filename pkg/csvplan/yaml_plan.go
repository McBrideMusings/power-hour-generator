package csvplan

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadCollectionYAML reads a YAML plan file and returns CollectionRows.
// Each YAML entry is a map of field names to values. Keys are normalized to
// lowercase. The link and start fields are required; duration defaults to
// opts.DefaultDuration when absent.
func LoadCollectionYAML(path string, opts CollectionOptions) ([]CollectionRow, error) {
	opts.LinkHeader = normalizeHeader(opts.LinkHeader)
	opts.StartHeader = normalizeHeader(opts.StartHeader)
	if opts.DurationHeader != "" {
		opts.DurationHeader = normalizeHeader(opts.DurationHeader)
	}

	if opts.LinkHeader == "" {
		opts.LinkHeader = "link"
	}
	if opts.StartHeader == "" {
		opts.StartHeader = "start_time"
	}
	if opts.DurationHeader == "" {
		opts.DurationHeader = "duration"
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

	var rawRows []map[string]interface{}
	if err := yaml.Unmarshal(data, &rawRows); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	// yaml.Unmarshal returns nil slice for empty/comment-only files
	if len(rawRows) == 0 {
		return nil, errors.New("no data rows found")
	}

	var (
		rows []CollectionRow
		errs ValidationErrors
	)

	for i, raw := range rawRows {
		rowIndex := i + 1
		row, rowErrs := parseYAMLRow(raw, rowIndex, opts)
		errs = append(errs, rowErrs...)
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return nil, errors.New("no data rows found")
	}

	if len(errs) > 0 {
		return rows, errs
	}
	return rows, nil
}

func parseYAMLRow(raw map[string]interface{}, index int, opts CollectionOptions) (CollectionRow, []ValidationError) {
	var errs []ValidationError

	// Normalize all keys and convert values to strings.
	fields := make(map[string]string, len(raw))
	for k, v := range raw {
		key := normalizeHeader(k)
		if key == "" {
			continue
		}
		fields[key] = yamlScalarToString(v)
	}

	link := strings.TrimSpace(fields[opts.LinkHeader])
	if link == "" {
		errs = append(errs, ValidationError{
			Line:    index,
			Field:   opts.LinkHeader,
			Message: fmt.Sprintf("%s is required", opts.LinkHeader),
		})
	}

	startRaw := strings.TrimSpace(fields[opts.StartHeader])
	var startDur time.Duration
	if startRaw == "" {
		errs = append(errs, ValidationError{
			Line:    index,
			Field:   opts.StartHeader,
			Message: fmt.Sprintf("%s is required", opts.StartHeader),
		})
	} else {
		d, err := parseStartTime(startRaw)
		if err != nil {
			errs = append(errs, ValidationError{Line: index, Field: opts.StartHeader, Message: err.Error()})
		} else {
			startDur = d
		}
	}

	durationSeconds := opts.DefaultDuration
	if durRaw := strings.TrimSpace(fields[opts.DurationHeader]); durRaw != "" {
		v, err := strconv.Atoi(durRaw)
		if err != nil {
			errs = append(errs, ValidationError{
				Line:    index,
				Field:   opts.DurationHeader,
				Message: "duration must be an integer",
			})
		} else if v <= 0 {
			errs = append(errs, ValidationError{
				Line:    index,
				Field:   opts.DurationHeader,
				Message: "duration must be greater than 0",
			})
		} else {
			durationSeconds = v
		}
	}

	if durationSeconds <= 0 {
		errs = append(errs, ValidationError{
			Line:    index,
			Field:   "duration",
			Message: "duration must be greater than 0",
		})
	}

	// All non-empty fields become custom fields (including link/start/duration).
	customFields := make(map[string]string, len(fields))
	for k, v := range fields {
		if v != "" {
			customFields[k] = v
		}
	}

	return CollectionRow{
		Index:           index,
		Link:            link,
		StartRaw:        startRaw,
		Start:           startDur,
		DurationSeconds: durationSeconds,
		CustomFields:    customFields,
	}, errs
}

// yamlScalarToString converts a YAML scalar value to its string representation.
// yaml.v3 follows YAML 1.2, so "1:40" is already a string; ints/floats are
// returned as their decimal string forms.
func yamlScalarToString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}
