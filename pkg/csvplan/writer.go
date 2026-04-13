package csvplan

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// WriteCSV writes collection rows back to a CSV/TSV file using atomic write
// (temp file + rename). Headers and delimiter are preserved from the original.
func WriteCSV(path string, headers []string, rows []CollectionRow, delimiter rune) error {
	if delimiter == 0 {
		delimiter = ','
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".csvplan-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	w := csv.NewWriter(tmp)
	w.Comma = delimiter

	if err := w.Write(headers); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write headers: %w", err)
	}

	for _, row := range rows {
		record := make([]string, len(headers))
		for i, h := range headers {
			if val, ok := row.CustomFields[h]; ok {
				record[i] = val
			}
		}
		if err := w.Write(record); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("write row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("flush csv: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// WriteYAML writes collection rows back to a YAML plan file using atomic write.
// The output uses the structured format with explicit columns and rows.
// Columns are merged with any new fields discovered in the row data.
func WriteYAML(path string, columns []string, rows []CollectionRow) error {
	columns = MergeHeaders(columns, rows)

	entries := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		entry := make(map[string]interface{}, len(row.CustomFields))
		for k, v := range row.CustomFields {
			if v == "" {
				continue
			}
			entry[k] = v
		}
		entries = append(entries, entry)
	}

	plan := struct {
		Columns []string                 `yaml:"columns"`
		Rows    []map[string]interface{} `yaml:"rows"`
	}{
		Columns: columns,
		Rows:    entries,
	}

	data, err := yaml.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".yamlplan-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write yaml: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// MergeHeaders preserves the existing CSV header order while appending any new
// normalized fields present in the provided rows.
func MergeHeaders(headers []string, rows []CollectionRow) []string {
	seen := make(map[string]bool)

	// Add existing headers in order
	merged := make([]string, 0, len(headers))
	for _, header := range headers {
		normalized := normalizeHeader(header)
		if normalized != "" && !seen[normalized] {
			seen[normalized] = true
			merged = append(merged, normalized)
		}
	}

	// Collect and sort new fields from rows
	var extras []string
	for _, row := range rows {
		for field := range row.CustomFields {
			normalized := normalizeHeader(field)
			if normalized != "" && !seen[normalized] {
				seen[normalized] = true
				extras = append(extras, normalized)
			}
		}
	}
	sort.Strings(extras)

	return append(merged, extras...)
}
