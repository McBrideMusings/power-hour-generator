package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"powerhour/internal/config"
)

func TestResolveInitDir(t *testing.T) {
	t.Run("project flag takes precedence", func(t *testing.T) {
		dir, err := resolveInitDir("/custom/path", []string{"ignored"})
		if err != nil {
			t.Fatal(err)
		}
		if dir != "/custom/path" {
			t.Fatalf("got %s, want /custom/path", dir)
		}
	})

	t.Run("dot uses cwd", func(t *testing.T) {
		cwd, _ := os.Getwd()
		dir, err := resolveInitDir("", []string{"."})
		if err != nil {
			t.Fatal(err)
		}
		if dir != cwd {
			t.Fatalf("got %s, want %s", dir, cwd)
		}
	})

	t.Run("named arg creates subdirectory", func(t *testing.T) {
		cwd, _ := os.Getwd()
		dir, err := resolveInitDir("", []string{"my-project"})
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(cwd, "my-project")
		if dir != want {
			t.Fatalf("got %s, want %s", dir, want)
		}
	})
}

func TestNextAvailableDir(t *testing.T) {
	base := t.TempDir()

	t.Run("returns powerhour-1 when empty", func(t *testing.T) {
		dir, err := nextAvailableDir(base)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(base, "powerhour-1")
		if dir != want {
			t.Fatalf("got %s, want %s", dir, want)
		}
	})

	t.Run("skips existing directories", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(base, "powerhour-1"), 0o755); err != nil {
			t.Fatal(err)
		}
		dir, err := nextAvailableDir(base)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(base, "powerhour-2")
		if dir != want {
			t.Fatalf("got %s, want %s", dir, want)
		}
	})

	t.Run("skips multiple existing", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(base, "powerhour-2"), 0o755); err != nil {
			t.Fatal(err)
		}
		dir, err := nextAvailableDir(base)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(base, "powerhour-3")
		if dir != want {
			t.Fatalf("got %s, want %s", dir, want)
		}
	})
}

func TestInitPlanTemplate(t *testing.T) {
	tests := []struct {
		collection string
		format     string
		wantFile   string
		wantBody   string
	}{
		{collection: "songs", format: "yaml", wantFile: "songs.yaml", wantBody: songsPlanYAML},
		{collection: "songs", format: "csv", wantFile: "songs.csv", wantBody: songsPlanCSV},
		{collection: "songs", format: "tsv", wantFile: "songs.tsv", wantBody: songsPlanTSV},
		{collection: "interstitials", format: "yaml", wantFile: "interstitials.yaml", wantBody: interstitialsPlanYAML},
		{collection: "interstitials", format: "csv", wantFile: "interstitials.csv", wantBody: interstitialsPlanCSV},
		{collection: "interstitials", format: "tsv", wantFile: "interstitials.tsv", wantBody: interstitialsPlanTSV},
	}

	for _, tt := range tests {
		t.Run(tt.collection+"-"+tt.format, func(t *testing.T) {
			gotFile, gotBody := initPlanTemplate(tt.collection, tt.format)
			if gotFile != tt.wantFile {
				t.Fatalf("file = %q, want %q", gotFile, tt.wantFile)
			}
			if gotBody != tt.wantBody {
				t.Fatalf("body = %q, want %q", gotBody, tt.wantBody)
			}
		})
	}
}

func TestDefaultPlanSchemaColumns(t *testing.T) {
	// Helper to extract columns from YAML: "columns: [col1, col2, ...]"
	extractYAMLColumns := func(yaml string) []string {
		lines := strings.Split(yaml, "\n")
		if len(lines) == 0 {
			return nil
		}
		line := lines[0]
		if !strings.Contains(line, "columns: [") {
			return nil
		}
		// Extract "[col1, col2, ...]" part
		start := strings.Index(line, "[")
		end := strings.Index(line, "]")
		if start < 0 || end < 0 || start >= end {
			return nil
		}
		content := line[start+1 : end]
		// Split on ", " and trim spaces
		parts := strings.Split(content, ", ")
		result := make([]string, len(parts))
		for i, p := range parts {
			result[i] = strings.TrimSpace(p)
		}
		return result
	}

	// Helper to extract columns from CSV/TSV
	extractDelimitedColumns := func(csv string, delimiter string) []string {
		firstLine := strings.Split(strings.TrimSpace(csv), "\n")[0]
		var delim rune
		if delimiter == "csv" {
			delim = ','
		} else {
			delim = '\t'
		}
		parts := strings.Split(firstLine, string(delim))
		result := make([]string, len(parts))
		for i, p := range parts {
			result[i] = strings.TrimSpace(p)
		}
		return result
	}

	tests := []struct {
		name        string
		template    string
		kind        string // "yaml", "csv", or "tsv"
		wantColumns []string
	}{
		{
			name:        "songs-yaml",
			template:    songsPlanYAML,
			kind:        "yaml",
			wantColumns: []string{"title", "artist", "name", "start_time", "duration", "link"},
		},
		{
			name:        "songs-csv",
			template:    songsPlanCSV,
			kind:        "csv",
			wantColumns: []string{"title", "artist", "name", "start_time", "duration", "link"},
		},
		{
			name:        "songs-tsv",
			template:    songsPlanTSV,
			kind:        "tsv",
			wantColumns: []string{"title", "artist", "name", "start_time", "duration", "link"},
		},
		{
			name:        "interstitials-yaml",
			template:    interstitialsPlanYAML,
			kind:        "yaml",
			wantColumns: []string{"label", "link", "start_time", "duration"},
		},
		{
			name:        "interstitials-csv",
			template:    interstitialsPlanCSV,
			kind:        "csv",
			wantColumns: []string{"label", "link", "start_time", "duration"},
		},
		{
			name:        "interstitials-tsv",
			template:    interstitialsPlanTSV,
			kind:        "tsv",
			wantColumns: []string{"label", "link", "start_time", "duration"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			if tt.kind == "yaml" {
				got = extractYAMLColumns(tt.template)
			} else {
				got = extractDelimitedColumns(tt.template, tt.kind)
			}

			if !slices.Equal(got, tt.wantColumns) {
				t.Errorf("columns mismatch\ngot:  %v\nwant: %v", got, tt.wantColumns)
			}
		})
	}
}

func TestRenderDefaultConfigYAMLUsesRequestedPlanFormat(t *testing.T) {
	cases := []struct {
		format string
		want   []string
	}{
		{format: "yaml", want: []string{"plan: songs.yaml", "plan: interstitials.yaml"}},
		{format: "csv", want: []string{"plan: songs.csv", "plan: interstitials.csv"}},
		{format: "tsv", want: []string{"plan: songs.tsv", "plan: interstitials.tsv"}},
	}

	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			rendered := renderDefaultConfigYAML(tc.format)
			for _, want := range tc.want {
				if !strings.Contains(rendered, want) {
					t.Fatalf("rendered config missing %q", want)
				}
			}
			for _, want := range []string{
				"columns: [title, artist]",
				"ytdlp:",
				"search_fields: [title, artist]",
			} {
				if !strings.Contains(rendered, want) {
					t.Fatalf("rendered config missing %q", want)
				}
			}
		})
	}
}

func TestRenderDefaultConfigYAMLLibraryKeysCommented(t *testing.T) {
	rendered := renderDefaultConfigYAML("yaml")

	// Verify library keys are commented, not a bare empty mapping
	if !strings.Contains(rendered, "# mode: shared") {
		t.Fatalf("rendered config missing library mode comment")
	}
	if !strings.Contains(rendered, "# path:") {
		t.Fatalf("rendered config missing library path comment")
	}
	if strings.Contains(rendered, "library: {}") {
		t.Fatalf("rendered config should not contain bare library: {}")
	}

	// Write to temp file and load via config.Load to verify parsing works
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "powerhour.yaml")
	if err := os.WriteFile(configPath, []byte(rendered), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}

	// Verify defaults: commented keys result in zero values, which resolve to defaults
	if !cfg.LibraryShared() {
		t.Fatalf("LibraryShared() should be true (default), got false")
	}
	if cfg.LibraryPath() != "" {
		t.Fatalf("LibraryPath() should be empty (default), got %q", cfg.LibraryPath())
	}
}
