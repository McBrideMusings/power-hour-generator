package cli

import (
	"os"
	"path/filepath"
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
