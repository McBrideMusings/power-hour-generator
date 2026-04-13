package csvplan

import (
	"fmt"
	"strings"
)

// ImportFormat identifies the source format of imported collection text.
type ImportFormat string

const (
	ImportFormatCSV  ImportFormat = "csv"
	ImportFormatTSV  ImportFormat = "tsv"
	ImportFormatYAML ImportFormat = "yaml"
)

// ImportCollectionText auto-detects YAML, CSV, or TSV collection text and
// parses it into normalized collection rows.
func ImportCollectionText(raw string, opts CollectionOptions) ([]CollectionRow, ImportFormat, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if trimmed == "" {
		return nil, "", fmt.Errorf("plan text is empty")
	}

	if looksLikeYAMLList(trimmed) {
		rows, err := LoadCollectionYAMLData([]byte(trimmed), opts)
		return rows, ImportFormatYAML, err
	}

	delimiter, err := detectDelimiter([]byte(trimmed))
	if err != nil {
		return nil, "", err
	}

	format := ImportFormatCSV
	if delimiter == '\t' {
		format = ImportFormatTSV
	}

	rows, err := LoadCollectionData([]byte(trimmed), opts)
	return rows, format, err
}

func looksLikeYAMLList(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	firstLine := raw
	if idx := strings.IndexAny(raw, "\r\n"); idx >= 0 {
		firstLine = raw[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)
	return strings.HasPrefix(firstLine, "- ")
}
