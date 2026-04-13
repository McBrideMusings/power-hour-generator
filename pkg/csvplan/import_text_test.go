package csvplan

import "testing"

func TestImportCollectionTextDetectsCSVTSVAndYAML(t *testing.T) {
	opts := CollectionOptions{
		LinkHeader:      "link",
		StartHeader:     "start_time",
		DurationHeader:  "duration",
		DefaultDuration: 60,
	}

	tests := []struct {
		name   string
		input  string
		format ImportFormat
	}{
		{
			name:   "csv",
			input:  "title,link,start_time,duration\nSong,https://example.com,0:10,62\n",
			format: ImportFormatCSV,
		},
		{
			name:   "tsv",
			input:  "title\tlink\tstart_time\tduration\nSong\thttps://example.com\t0:10\t62\n",
			format: ImportFormatTSV,
		},
		{
			name: "yaml",
			input: "- title: Song\n" +
				"  link: https://example.com\n" +
				"  start_time: \"0:10\"\n" +
				"  duration: 62\n",
			format: ImportFormatYAML,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, format, err := ImportCollectionText(tt.input, opts)
			if err != nil {
				t.Fatalf("ImportCollectionText: %v", err)
			}
			if format != tt.format {
				t.Fatalf("format = %q, want %q", format, tt.format)
			}
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			if rows[0].Link != "https://example.com" {
				t.Fatalf("link = %q", rows[0].Link)
			}
		})
	}
}

func TestImportCollectionTextNormalizesHeaders(t *testing.T) {
	input := "TITLE,LINK,START TIME,DURATION\nSong,https://example.com,0:10,62\n"
	rows, _, err := ImportCollectionText(input, CollectionOptions{
		LinkHeader:      "link",
		StartHeader:     "start_time",
		DurationHeader:  "duration",
		DefaultDuration: 60,
	})
	if err != nil {
		t.Fatalf("ImportCollectionText: %v", err)
	}

	if got := rows[0].CustomFields["title"]; got != "Song" {
		t.Fatalf("title = %q, want Song", got)
	}
}

func TestImportCollectionTextRejectsInvalidBatch(t *testing.T) {
	input := "- title: Song\n  link: https://example.com\n"
	rows, format, err := ImportCollectionText(input, CollectionOptions{
		LinkHeader:      "link",
		StartHeader:     "start_time",
		DurationHeader:  "duration",
		DefaultDuration: 60,
	})
	if format != ImportFormatYAML {
		t.Fatalf("format = %q, want yaml", format)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 partial row", len(rows))
	}
	if err == nil {
		t.Fatal("expected validation error")
	}
	if _, ok := err.(ValidationErrors); !ok {
		t.Fatalf("err type = %T, want ValidationErrors", err)
	}
}

func TestMergeHeadersAppendsNewFields(t *testing.T) {
	merged := MergeHeaders([]string{"title", "link"}, []CollectionRow{
		{CustomFields: map[string]string{"artist": "Artist", "title": "Song", "mood": "loud"}},
	})

	want := []string{"title", "link", "artist", "mood"}
	if len(merged) != len(want) {
		t.Fatalf("headers len = %d, want %d (%v)", len(merged), len(want), merged)
	}
	for i := range want {
		if merged[i] != want[i] {
			t.Fatalf("headers[%d] = %q, want %q", i, merged[i], want[i])
		}
	}
}
