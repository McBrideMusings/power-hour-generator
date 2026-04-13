package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		})
	}
}
