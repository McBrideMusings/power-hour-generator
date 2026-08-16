package csvplan

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// writeTempPlan writes content to a temp file named name and returns its path.
func writeTempPlan(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp plan: %v", err)
	}
	return path
}

func TestImportFromCSVHeuristics(t *testing.T) {
	type want struct {
		index           int
		link            string
		startRaw        string
		start           time.Duration
		durationSeconds int
		customFields    map[string]string
	}

	tests := []struct {
		name    string
		content string
		opts    ImportOptions
		want    []want
	}{
		{
			name: "comma-separated with header",
			content: "title,link,start_time,duration\n" +
				"Song One,https://example.com/1,0:10,45\n" +
				"Song Two,https://example.com/2,1:20,30\n",
			opts: ImportOptions{},
			want: []want{
				{
					index: 1, link: "https://example.com/1", startRaw: "0:10",
					start: 10 * time.Second, durationSeconds: 45,
					customFields: map[string]string{
						"title": "Song One", "link": "https://example.com/1",
						"start_time": "0:10", "duration": "45",
					},
				},
				{
					index: 2, link: "https://example.com/2", startRaw: "1:20",
					start: 80 * time.Second, durationSeconds: 30,
					customFields: map[string]string{
						"title": "Song Two", "link": "https://example.com/2",
						"start_time": "1:20", "duration": "30",
					},
				},
			},
		},
		{
			name: "tab-separated with header",
			content: "title\tlink\tstart_time\tduration\n" +
				"Song One\thttps://example.com/1\t0:10\t45\n",
			opts: ImportOptions{},
			want: []want{
				{
					index: 1, link: "https://example.com/1", startRaw: "0:10",
					start: 10 * time.Second, durationSeconds: 45,
					customFields: map[string]string{
						"title": "Song One", "link": "https://example.com/1",
						"start_time": "0:10", "duration": "45",
					},
				},
			},
		},
		{
			name: "comma-separated with no header",
			content: "https://example.com/1,0:10,Cool Song,45\n" +
				"https://example.com/2,1:00,Another Song,30\n",
			opts: ImportOptions{},
			want: []want{
				{
					index: 1, link: "https://example.com/1", startRaw: "0:10",
					start: 10 * time.Second, durationSeconds: 45,
					customFields: map[string]string{
						"link": "https://example.com/1", "start_time": "0:10",
						"col1": "Cool Song", "duration": "45",
					},
				},
				{
					index: 2, link: "https://example.com/2", startRaw: "1:00",
					start: 60 * time.Second, durationSeconds: 30,
					customFields: map[string]string{
						"link": "https://example.com/2", "start_time": "1:00",
						"col1": "Another Song", "duration": "30",
					},
				},
			},
		},
		{
			name: "tab-separated with no header",
			content: "https://example.com/1\t0:10\tCool Song\t45\n" +
				"https://example.com/2\t1:00\tAnother Song\t30\n",
			opts: ImportOptions{},
			want: []want{
				{
					index: 1, link: "https://example.com/1", startRaw: "0:10",
					start: 10 * time.Second, durationSeconds: 45,
					customFields: map[string]string{
						"link": "https://example.com/1", "start_time": "0:10",
						"col1": "Cool Song", "duration": "45",
					},
				},
				{
					index: 2, link: "https://example.com/2", startRaw: "1:00",
					start: 60 * time.Second, durationSeconds: 30,
					customFields: map[string]string{
						"link": "https://example.com/2", "start_time": "1:00",
						"col1": "Another Song", "duration": "30",
					},
				},
			},
		},
		{
			name: "quoted field containing delimiter (CSV)",
			content: "title,link,start_time,duration\n" +
				"\"Song, with comma\",https://example.com/1,0:10,45\n",
			opts: ImportOptions{},
			want: []want{
				{
					index: 1, link: "https://example.com/1", startRaw: "0:10",
					start: 10 * time.Second, durationSeconds: 45,
					customFields: map[string]string{
						"title": "Song, with comma", "link": "https://example.com/1",
						"start_time": "0:10", "duration": "45",
					},
				},
			},
		},
		{
			// majorityDelim ties go to tab (tabs >= commas), so a TSV line
			// containing a quoted comma still parses as TSV; the CSV reader
			// keeps the comma inside its quoted field rather than splitting on it.
			name: "quoted field containing delimiter (TSV)",
			content: "title\tlink\tstart_time\tduration\n" +
				"\"Song, with comma\"\thttps://example.com/1\t0:10\t45\n",
			opts: ImportOptions{},
			want: []want{
				{
					index: 1, link: "https://example.com/1", startRaw: "0:10",
					start: 10 * time.Second, durationSeconds: 45,
					customFields: map[string]string{
						"title": "Song, with comma", "link": "https://example.com/1",
						"start_time": "0:10", "duration": "45",
					},
				},
			},
		},
		{
			name: "missing duration column defaults to sixty seconds",
			content: "title,link,start_time\n" +
				"Song One,https://example.com/1,0:10\n",
			opts: ImportOptions{}, // DefaultDuration 0 coerces to 60
			want: []want{
				{
					index: 1, link: "https://example.com/1", startRaw: "0:10",
					start: 10 * time.Second, durationSeconds: 60,
					customFields: map[string]string{
						"title": "Song One", "link": "https://example.com/1",
						"start_time": "0:10",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempPlan(t, "plan.csv", tt.content)

			rows, err := ImportFromCSV(path, tt.opts)
			if err != nil {
				t.Fatalf("ImportFromCSV returned error: %v", err)
			}
			if len(rows) != len(tt.want) {
				t.Fatalf("expected %d rows, got %d: %+v", len(tt.want), len(rows), rows)
			}
			for i, w := range tt.want {
				row := rows[i]
				if row.Index != w.index {
					t.Errorf("row %d: Index = %d, want %d", i, row.Index, w.index)
				}
				if row.Link != w.link {
					t.Errorf("row %d: Link = %q, want %q", i, row.Link, w.link)
				}
				if row.StartRaw != w.startRaw {
					t.Errorf("row %d: StartRaw = %q, want %q", i, row.StartRaw, w.startRaw)
				}
				if row.Start != w.start {
					t.Errorf("row %d: Start = %v, want %v", i, row.Start, w.start)
				}
				if row.DurationSeconds != w.durationSeconds {
					t.Errorf("row %d: DurationSeconds = %d, want %d", i, row.DurationSeconds, w.durationSeconds)
				}
				if !reflect.DeepEqual(row.CustomFields, w.customFields) {
					t.Errorf("row %d: CustomFields = %#v, want %#v", i, row.CustomFields, w.customFields)
				}
			}
		})
	}
}

// TestImportFromCSVLinkColumnOverride pins the heuristic override in
// resolveColumnRoles: when the header-mapped link column doesn't actually
// contain URLs but another column does, that column is used for link
// instead. Only the canonical CollectionRow fields are asserted here (not
// CustomFields) because the override causes two columns to share the
// "link" output name, and map iteration order over colNames makes which
// value survives in CustomFields nondeterministic.
func TestImportFromCSVLinkColumnOverride(t *testing.T) {
	content := "link,other,start_time,duration\n" +
		"not-a-url,https://example.com/1,0:10,45\n" +
		"also-not,https://example.com/2,1:00,30\n"
	path := writeTempPlan(t, "plan.csv", content)

	rows, err := ImportFromCSV(path, ImportOptions{})
	if err != nil {
		t.Fatalf("ImportFromCSV returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	wantLink := []string{"https://example.com/1", "https://example.com/2"}
	wantStartRaw := []string{"0:10", "1:00"}
	wantStart := []time.Duration{10 * time.Second, 60 * time.Second}
	wantDuration := []int{45, 30}

	for i, row := range rows {
		if row.Link != wantLink[i] {
			t.Errorf("row %d: Link = %q, want %q", i, row.Link, wantLink[i])
		}
		if row.StartRaw != wantStartRaw[i] {
			t.Errorf("row %d: StartRaw = %q, want %q", i, row.StartRaw, wantStartRaw[i])
		}
		if row.Start != wantStart[i] {
			t.Errorf("row %d: Start = %v, want %v", i, row.Start, wantStart[i])
		}
		if row.DurationSeconds != wantDuration[i] {
			t.Errorf("row %d: DurationSeconds = %d, want %d", i, row.DurationSeconds, wantDuration[i])
		}
	}
}

func TestImportFromCSVValidationErrors(t *testing.T) {
	content := "title,link,start_time\n" +
		"Song A,,0:10\n" +
		"Song B,https://example.com/2,\n"
	path := writeTempPlan(t, "plan.csv", content)

	rows, err := ImportFromCSV(path, ImportOptions{})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows despite validation issues, got %d", len(rows))
	}

	var vErrs ValidationErrors
	if !errors.As(err, &vErrs) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(vErrs) != 2 {
		t.Fatalf("expected 2 validation errors, got %d: %+v", len(vErrs), vErrs)
	}

	if vErrs[0].Line != 1 || vErrs[0].Field != "link" {
		t.Errorf("errs[0] = %+v, want Line=1 Field=link", vErrs[0])
	}
	if vErrs[1].Line != 2 || vErrs[1].Field != "start_time" {
		t.Errorf("errs[1] = %+v, want Line=2 Field=start_time", vErrs[1])
	}

	// Rows are still populated for the fields that did parse successfully.
	if rows[0].Link != "" {
		t.Errorf("rows[0].Link = %q, want empty", rows[0].Link)
	}
	if rows[0].StartRaw != "0:10" {
		t.Errorf("rows[0].StartRaw = %q, want 0:10", rows[0].StartRaw)
	}
	if rows[1].Link != "https://example.com/2" {
		t.Errorf("rows[1].Link = %q, want https://example.com/2", rows[1].Link)
	}
	if rows[1].StartRaw != "" {
		t.Errorf("rows[1].StartRaw = %q, want empty", rows[1].StartRaw)
	}
}
