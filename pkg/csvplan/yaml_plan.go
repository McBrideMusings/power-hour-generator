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
	Columns  []string
	Defaults map[string]string
	Rows     []CollectionRow
}

// LoadCollectionYAML reads a YAML plan file and returns a YAMLResult with
// columns and rows. The file can be either the structured format (mapping
// with "columns" and "rows" keys) or a bare YAML list for backward
// compatibility. Any row lacking a stable id is assigned one and written
// back to path (so a row hand-added in an external editor gets an id on the
// next load).
func LoadCollectionYAML(path string, opts CollectionOptions) (YAMLResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return YAMLResult{}, fmt.Errorf("read file: %w", err)
	}

	if len(data) == 0 {
		return YAMLResult{}, errors.New("plan file is empty")
	}

	result, assigned, loadErr := loadCollectionYAMLStructured(data, opts)
	if assigned > 0 && len(result.Rows) > 0 {
		// Write back using the declared columns as-is (WriteYAML merges in
		// "id" and any other row-only fields for the on-disk file); the
		// returned result.Columns stays exactly what the file declared —
		// discovering row-only fields for display is discoverColumns' job,
		// not this read path's.
		_ = WriteYAML(path, result.Columns, result.Defaults, result.Rows)
	}
	return result, loadErr
}

// LoadCollectionYAMLData reads a bare YAML list from raw bytes. This is used
// for importing pasted YAML snippets (not plan files). Missing ids are
// assigned in memory but never written back — the caller has no path to
// write to.
func LoadCollectionYAMLData(data []byte, opts CollectionOptions) ([]CollectionRow, error) {
	if len(data) == 0 {
		return nil, errors.New("plan file is empty")
	}
	rows, _, err := loadCollectionYAMLBareList(data, opts)
	return rows, err
}

// yamlPlan is the structured YAML plan format with explicit column schema.
type yamlPlan struct {
	Columns  []string         `yaml:"columns"`
	Defaults map[string]any   `yaml:"defaults"`
	Rows     []map[string]any `yaml:"rows"`
}

// loadCollectionYAMLStructured handles the structured format (columns + rows
// mapping) used by plan files. Falls back to bare list for backward compat.
// The returned int is the number of rows that were assigned a fresh stable
// id (0 if every row already had one).
func loadCollectionYAMLStructured(data []byte, opts CollectionOptions) (YAMLResult, int, error) {
	opts = normalizeYAMLOpts(opts)

	var plan yamlPlan
	err := yaml.Unmarshal(data, &plan)

	// Check if unmarshal succeeded
	if err == nil {
		// Structured format: columns key present
		if plan.Columns != nil {
			for i, c := range plan.Columns {
				plan.Columns[i] = normalizeHeader(c)
			}
			defaults := normalizeYAMLDefaults(plan.Defaults)
			rows, errs := parseYAMLRows(plan.Rows, defaults, opts)
			assigned := assignRowIDs(rows)
			result := YAMLResult{Columns: plan.Columns, Defaults: defaults, Rows: rows}
			if len(errs) > 0 {
				return result, assigned, errs
			}
			return result, assigned, nil
		}

		// Mapping with rows but no columns key - this is an error
		if plan.Rows != nil {
			return YAMLResult{}, 0, errors.New("YAML plan has rows but is missing required columns key")
		}
	}

	// Fall through to bare list parser for backward compat. Ids are still
	// assigned in memory (assignRowIDs runs inside loadCollectionYAMLBareList),
	// but the assigned count is not propagated here: a bare-list plan file
	// stays a bare list on disk rather than being silently upgraded to the
	// structured columns+rows format as a side effect of a read.
	rows, _, bareErr := loadCollectionYAMLBareList(data, opts)
	return YAMLResult{Rows: rows}, 0, bareErr
}

// loadCollectionYAMLBareList handles the legacy bare-list format (a YAML list
// of maps) used for importing pasted snippets and old plan files. The
// returned int is the number of rows assigned a fresh stable id.
func loadCollectionYAMLBareList(data []byte, opts CollectionOptions) ([]CollectionRow, int, error) {
	opts = normalizeYAMLOpts(opts)

	var rawRows []map[string]any
	if err := yaml.Unmarshal(data, &rawRows); err != nil {
		return nil, 0, fmt.Errorf("parse YAML: %w", err)
	}

	if len(rawRows) == 0 {
		return nil, 0, errors.New("no data rows found")
	}

	rows, errs := parseYAMLRows(rawRows, nil, opts)
	if len(rows) == 0 {
		return nil, 0, errors.New("no data rows found")
	}
	assigned := assignRowIDs(rows)
	if len(errs) > 0 {
		return rows, assigned, errs
	}
	return rows, assigned, nil
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

func normalizeYAMLDefaults(raw map[string]any) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	defaults := make(map[string]string, len(raw))
	for k, v := range raw {
		key := normalizeHeader(k)
		if key == "" {
			continue
		}
		defaults[key] = yamlScalarToString(v)
	}
	if len(defaults) == 0 {
		return nil
	}
	return defaults
}

func parseYAMLRows(rawRows []map[string]any, defaults map[string]string, opts CollectionOptions) ([]CollectionRow, ValidationErrors) {
	var (
		rows []CollectionRow
		errs ValidationErrors
	)
	for i, raw := range rawRows {
		rowIndex := i + 1
		row, rowErrs := parseYAMLRow(raw, defaults, rowIndex, opts)
		errs = append(errs, rowErrs...)
		rows = append(rows, row)
	}
	return rows, errs
}

func parseYAMLRow(raw map[string]any, defaults map[string]string, index int, opts CollectionOptions) (CollectionRow, []ValidationError) {
	var errs []ValidationError

	// Start from schema defaults, then apply row-specific values over them.
	fields := make(map[string]string, len(defaults)+len(raw))
	for k, v := range defaults {
		fields[k] = v
	}
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
		RowID:           strings.TrimSpace(fields["id"]),
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
func yamlScalarToString(v any) string {
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
