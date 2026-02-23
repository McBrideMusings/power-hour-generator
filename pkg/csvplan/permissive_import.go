package csvplan

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ImportOptions controls how ImportFromCSV parses a CSV/TSV file.
type ImportOptions struct {
	LinkHeader      string // Override column name for the URL field (empty = auto-detect)
	StartHeader     string // Override column name for the start time field (empty = auto-detect)
	DurationHeader  string // Override column name for the duration field (empty = auto-detect)
	DefaultDuration int    // Fallback duration in seconds (default: 60)
}

var (
	reURL     = regexp.MustCompile(`(?i)^https?://`)
	reTimePat = regexp.MustCompile(`^\d+:\d{2}`)
)

// ImportFromCSV reads a CSV/TSV file permissively and returns CollectionRows.
// Permissive behaviors:
//   - Mixed delimiters: header and data rows may use different delimiters.
//   - Case-insensitive headers: all header names are normalized.
//   - Heuristic column override: if the mapped link column doesn't contain URLs
//     but another column does, that column is used for link instead.
//   - NoHeader mode: column roles are detected entirely from data patterns.
func ImportFromCSV(path string, opts ImportOptions) ([]CollectionRow, error) {
	if opts.DefaultDuration <= 0 {
		opts.DefaultDuration = 60
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("plan file is empty")
	}

	content := strings.TrimPrefix(string(raw), "\ufeff") // strip UTF-8 BOM

	allLines := nonEmptyLines(content)
	if len(allLines) == 0 {
		return nil, errors.New("plan file is empty")
	}

	var headerLine string
	var dataLines []string

	// Auto-detect whether the first line is a header. If it contains a URL or
	// a time pattern (digits:digits) it's almost certainly a data row.
	if looksLikeHeader(allLines[0]) {
		headerLine = allLines[0]
		dataLines = allLines[1:]
	} else {
		dataLines = allLines
	}

	if len(dataLines) == 0 {
		return nil, errors.New("no data rows found")
	}

	// Use majority vote among data lines to choose the data delimiter.
	dataDelim := majorityDelim(dataLines)

	// Parse each data line into a raw string slice.
	rawRecords := make([][]string, 0, len(dataLines))
	for _, line := range dataLines {
		rec := splitLine(line, dataDelim)
		for i, v := range rec {
			rec[i] = stripSingleQuotes(v)
		}
		if !isEmptyRecord(rec) {
			rawRecords = append(rawRecords, rec)
		}
	}
	if len(rawRecords) == 0 {
		return nil, errors.New("no data rows found")
	}

	// Determine column roles (link, start, duration) and output key names.
	// Empty headerLine means the file had no header row; use pure heuristics.
	linkCol, startCol, durationCol, colNames := resolveColumnRoles(headerLine, rawRecords, opts)

	// Build CollectionRows.
	var (
		rows []CollectionRow
		errs ValidationErrors
	)
	for ri, rec := range rawRecords {
		row, rowErrs := buildImportRow(rec, ri+1, linkCol, startCol, durationCol, colNames, opts.DefaultDuration)
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

// resolveColumnRoles returns the column indices for link, start, and duration,
// plus a map of col index → output field name for all other columns.
func resolveColumnRoles(headerLine string, records [][]string, opts ImportOptions) (linkCol, startCol, durationCol int, colNames map[int]string) {
	linkCol, startCol, durationCol = -1, -1, -1

	if headerLine == "" {
		linkCol, startCol, durationCol = heuristicRoles(records)
		colNames = heuristicColNames(maxCols(records), linkCol, startCol, durationCol)
		return
	}

	// Parse header with its own delimiter.
	headerDelim := lineDelim(headerLine)
	rawHeaders := splitLine(headerLine, headerDelim)
	normHeaders := make([]string, len(rawHeaders))
	for i, h := range rawHeaders {
		normHeaders[i] = normalizeHeader(h)
	}

	wantLink := normalizeHeader(opts.LinkHeader)
	if wantLink == "" {
		wantLink = "link"
	}
	wantStart := normalizeHeader(opts.StartHeader)
	if wantStart == "" {
		wantStart = "start_time"
	}
	wantDuration := normalizeHeader(opts.DurationHeader)
	if wantDuration == "" {
		wantDuration = "duration"
	}

	// Map from normalized header name → column index.
	for i, h := range normHeaders {
		switch h {
		case wantLink:
			linkCol = i
		case wantStart:
			startCol = i
		case wantDuration:
			durationCol = i
		}
	}

	// Heuristic override: if the mapped link col doesn't actually have URLs,
	// scan all columns for URLs. We intentionally do not exclude durationCol
	// here — the header column named "duration" may actually contain URLs when
	// the spreadsheet column order differs from the header order.
	if linkCol < 0 || !colMatchesMajority(records, linkCol, reURL) {
		for i := range normHeaders {
			if colMatchesMajority(records, i, reURL) {
				linkCol = i
				break
			}
		}
	}
	// If the heuristic reassigned linkCol to a column that was previously
	// identified as durationCol (e.g. URL in "duration" column), clear
	// durationCol so it isn't double-assigned.
	if linkCol == durationCol {
		durationCol = -1
	}
	if linkCol == startCol {
		startCol = -1
	}
	// Heuristic override for start_time. Only exclude the confirmed link column.
	if startCol < 0 || !colMatchesMajority(records, startCol, reTimePat) {
		for i := range normHeaders {
			if i != linkCol && colMatchesMajority(records, i, reTimePat) {
				startCol = i
				break
			}
		}
	}

	// Build output names: standard names for role columns, header names for rest.
	colNames = make(map[int]string, len(normHeaders))
	for i, h := range normHeaders {
		colNames[i] = h
	}
	if linkCol >= 0 {
		colNames[linkCol] = wantLink
	}
	if startCol >= 0 {
		colNames[startCol] = wantStart
	}
	if durationCol >= 0 {
		colNames[durationCol] = wantDuration
	}
	return
}

// buildImportRow constructs a CollectionRow from a raw record given the column
// role indices and output key names.
func buildImportRow(rec []string, index, linkCol, startCol, durationCol int, colNames map[int]string, defaultDuration int) (CollectionRow, []ValidationError) {
	var errs []ValidationError

	get := func(col int) string {
		if col < 0 || col >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[col])
	}

	link := get(linkCol)
	if link == "" {
		errs = append(errs, ValidationError{Line: index, Field: "link", Message: "link is required"})
	}

	startRaw := get(startCol)
	var startDur time.Duration
	if startRaw == "" {
		errs = append(errs, ValidationError{Line: index, Field: "start_time", Message: "start_time is required"})
	} else {
		d, err := parseStartTime(startRaw)
		if err != nil {
			errs = append(errs, ValidationError{Line: index, Field: "start_time", Message: err.Error()})
		} else {
			startDur = d
		}
	}

	durationSeconds := defaultDuration
	if durationCol >= 0 {
		if durRaw := get(durationCol); durRaw != "" {
			if v, err := strconv.Atoi(durRaw); err == nil && v > 0 {
				durationSeconds = v
			}
		}
	}

	// Build custom fields from all columns using their output names.
	customFields := make(map[string]string, len(colNames)+2)
	for col, name := range colNames {
		v := get(col)
		if v != "" {
			customFields[name] = v
		}
	}
	// Always include canonical link/start in custom fields for template access.
	if link != "" {
		customFields["link"] = link
	}
	if startRaw != "" {
		customFields["start_time"] = startRaw
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

// heuristicRoles scans records to identify which column index is URL (link),
// time pattern (start_time), and small integer (duration).
func heuristicRoles(records [][]string) (linkCol, startCol, durationCol int) {
	n := maxCols(records)
	urlCounts := make([]int, n)
	timeCounts := make([]int, n)
	intCounts := make([]int, n)

	for _, rec := range records {
		for i, v := range rec {
			if i >= n {
				break
			}
			v = strings.TrimSpace(v)
			if reURL.MatchString(v) {
				urlCounts[i]++
			}
			if reTimePat.MatchString(v) {
				timeCounts[i]++
			}
			if isSmallInt(v) {
				intCounts[i]++
			}
		}
	}

	linkCol = bestCol(urlCounts)
	startCol = bestColExcluding(timeCounts, linkCol)
	durationCol = bestColExcluding(intCounts, linkCol, startCol)
	return
}

// heuristicColNames assigns output names to all columns in no-header mode.
// Role columns get standard names; others get "col1", "col2", etc.
func heuristicColNames(numCols, linkCol, startCol, durationCol int) map[int]string {
	names := make(map[int]string, numCols)
	roles := map[int]string{
		linkCol:     "link",
		startCol:    "start_time",
		durationCol: "duration",
	}
	colN := 1
	for i := 0; i < numCols; i++ {
		if name, ok := roles[i]; ok && i >= 0 {
			names[i] = name
		} else {
			names[i] = fmt.Sprintf("col%d", colN)
			colN++
		}
	}
	return names
}

// colMatchesMajority returns true when more than half of non-empty values in
// the column match the pattern.
func colMatchesMajority(records [][]string, col int, re *regexp.Regexp) bool {
	total, matched := 0, 0
	for _, rec := range records {
		if col >= len(rec) {
			continue
		}
		v := strings.TrimSpace(rec[col])
		if v == "" {
			continue
		}
		total++
		if re.MatchString(v) {
			matched++
		}
	}
	return total > 0 && matched*2 >= total
}

// isSmallInt returns true when s is a decimal integer in the range 1–600.
func isSmallInt(s string) bool {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil && v >= 1 && v <= 600
}

// bestCol returns the column index with the highest count (ties: lower index wins).
func bestCol(counts []int) int {
	best, bestIdx := 0, -1
	for i, c := range counts {
		if c > best {
			best = c
			bestIdx = i
		}
	}
	return bestIdx
}

// bestColExcluding is like bestCol but ignores specified indices.
func bestColExcluding(counts []int, exclude ...int) int {
	skip := make(map[int]bool, len(exclude))
	for _, e := range exclude {
		if e >= 0 {
			skip[e] = true
		}
	}
	best, bestIdx := 0, -1
	for i, c := range counts {
		if skip[i] {
			continue
		}
		if c > best {
			best = c
			bestIdx = i
		}
	}
	return bestIdx
}

// maxCols returns the largest number of columns across all records.
func maxCols(records [][]string) int {
	max := 0
	for _, rec := range records {
		if len(rec) > max {
			max = len(rec)
		}
	}
	return max
}

// nonEmptyLines splits content into lines, filtering blank lines and lines
// starting with '#'.
func nonEmptyLines(content string) []string {
	all := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(all))
	for _, l := range all {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, l) // preserve original for CSV parsing
	}
	return out
}

// lineDelim returns '\t' if the line contains more tabs than commas, else ','.
func lineDelim(line string) rune {
	if strings.Count(line, "\t") > strings.Count(line, ",") {
		return '\t'
	}
	return ','
}

// majorityDelim picks the delimiter used by the majority of data lines.
func majorityDelim(lines []string) rune {
	tabs, commas := 0, 0
	for _, l := range lines {
		if strings.Count(l, "\t") > 0 {
			tabs++
		}
		if strings.Count(l, ",") > 0 {
			commas++
		}
	}
	if tabs >= commas {
		return '\t'
	}
	return ','
}

// splitLine parses a single line using the csv package with the given delimiter.
// LazyQuotes=true allows malformed quoting to pass through.
func splitLine(line string, delim rune) []string {
	r := csv.NewReader(strings.NewReader(line))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rec, err := r.Read()
	if err != nil {
		// Fallback to simple split on error.
		return strings.Split(line, string(delim))
	}
	return rec
}

// looksLikeHeader returns true when a line appears to be a header row rather
// than data. A line is considered data if any field contains a URL or a
// mm:ss time pattern — those patterns don't appear in column names.
func looksLikeHeader(line string) bool {
	delim := lineDelim(line)
	for _, f := range splitLine(line, delim) {
		f = strings.TrimSpace(f)
		if reURL.MatchString(f) || reTimePat.MatchString(f) {
			return false
		}
	}
	return true
}

// stripSingleQuotes removes leading and trailing single quotes from a value
// when both are present (e.g. from non-standard CSV quoting).
func stripSingleQuotes(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	return v
}
