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

// YAMLResult holds the parsed result of a YAML plan file, including declared
// column names and parsed rows.
type YAMLResult struct {
	Columns []string
	Rows    []CollectionRow
}

// LoadCollectionYAML reads a YAML plan file and returns a YAMLResult with
// columns and rows. The file can be either the structured format (mapping with
// "columns" and "rows" keys) or a bare YAML list for backward compatibility.
func LoadCollectionYAML(path string, opts CollectionOptions) (YAMLResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return YAMLResult{}, fmt.Errorf("read file: %w", err)
	}

	if len(data) == 0 {
		return YAMLResult{}, errors.New("plan file is empty")
	}

	return loadCollectionYAMLStructured(data, opts)
}

// LoadCollectionYAMLData reads a bare YAML list from raw bytes. This is used
// for importing pasted YAML snippets (not plan files).
func LoadCollectionYAMLData(data []byte, opts CollectionOptions) ([]CollectionRow, error) {
	if len(data) == 0 {
		return nil, errors.New("plan file is empty")
	}
	return loadCollectionYAMLBareList(data, opts)
}

// yamlPlan is the structured YAML plan format with explicit column schema.
type yamlPlan struct {
	Columns []string                 `yaml:"columns"`
	Rows    []map[string]interface{} `yaml:"rows"`
}

// loadCollectionYAMLStructured handles the structured format (columns + rows
// mapping) used by plan files. Falls back to bare list for backward compat.
func loadCollectionYAMLStructured(data []byte, opts CollectionOptions) (YAMLResult, error) {
	opts = normalizeYAMLOpts(opts)

	var plan yamlPlan
	if err := yaml.Unmarshal(data, &plan); err == nil && plan.Columns != nil {
		for i, c := range plan.Columns {
			plan.Columns[i] = normalizeHeader(c)
		}
		rows, errs := parseYAMLRows(plan.Rows, opts)
		result := YAMLResult{Columns: plan.Columns, Rows: rows}
		if len(errs) > 0 {
			return result, errs
		}
		return result, nil
	}

	rows, err := loadCollectionYAMLBareList(data, opts)
	return YAMLResult{Rows: rows}, err
}

// loadCollectionYAMLBareList handles the legacy bare-list format (a YAML list
// of maps) used for importing pasted snippets and old plan files.
func loadCollectionYAMLBareList(data []byte, opts CollectionOptions) ([]CollectionRow, error) {
	opts = normalizeYAMLOpts(opts)

	var rawRows []map[string]interface{}
	if err := yaml.Unmarshal(data, &rawRows); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	if len(rawRows) == 0 {
		return nil, errors.New("no data rows found")
	}

	rows, errs := parseYAMLRows(rawRows, opts)
	if len(rows) == 0 {
		return nil, errors.New("no data rows found")
	}
	if len(errs) > 0 {
		return rows, errs
	}
	return rows, nil
}

func normalizeYAMLOpts(opts CollectionOptions) CollectionOptions {
	// Normalize header names
	if n := normalizeHeader(opts.LinkHeader); n != "" {
		opts.LinkHeader = n
	} else {
		opts.LinkHeader = "link"
	}

	if n := normalizeHeader(opts.StartHeader); n != "" {
		opts.StartHeader = n
	} else {
		opts.StartHeader = "start_time"
	}

	if n := normalizeHeader(opts.DurationHeader); n != "" {
		opts.DurationHeader = n
	} else {
		opts.DurationHeader = "duration"
	}

	// Set default duration
	if opts.DefaultDuration <= 0 {
		opts.DefaultDuration = 60
	}

	return opts
}

func parseYAMLRows(rawRows []map[string]interface{}, opts CollectionOptions) ([]CollectionRow, ValidationErrors) {
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
	return rows, errs
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
