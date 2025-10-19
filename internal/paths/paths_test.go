package paths

import (
	"path/filepath"
	"testing"

	"powerhour/internal/config"
)

func TestApplyConfigRelative(t *testing.T) {
	root := t.TempDir()
	pp := ProjectPaths{
		Root:        root,
		ConfigFile:  filepath.Join(root, "powerhour.yaml"),
		CSVFile:     filepath.Join(root, "powerhour.csv"),
		CookiesFile: filepath.Join(root, "cookies.txt"),
	}

	cfg := config.Config{}
	cfg.Files.Plan = "custom-plan.tsv"
	cfg.Files.Cookies = "cookies/auth.txt"

	applied := ApplyConfig(pp, cfg)

	expectedPlan := filepath.Join(root, "custom-plan.tsv")
	if applied.CSVFile != expectedPlan {
		t.Fatalf("expected plan path %s, got %s", expectedPlan, applied.CSVFile)
	}

	expectedCookies := filepath.Join(root, "cookies/auth.txt")
	if applied.CookiesFile != expectedCookies {
		t.Fatalf("expected cookies path %s, got %s", expectedCookies, applied.CookiesFile)
	}
}

func TestApplyConfigAbsolute(t *testing.T) {
	root := t.TempDir()
	pp := ProjectPaths{
		Root:        root,
		ConfigFile:  filepath.Join(root, "powerhour.yaml"),
		CSVFile:     filepath.Join(root, "powerhour.csv"),
		CookiesFile: filepath.Join(root, "cookies.txt"),
	}

	planAbs := filepath.Join(t.TempDir(), "plan.csv")
	cookiesAbs := filepath.Join(t.TempDir(), "cookies.txt")

	cfg := config.Config{}
	cfg.Files.Plan = planAbs
	cfg.Files.Cookies = cookiesAbs

	applied := ApplyConfig(pp, cfg)

	if applied.CSVFile != planAbs {
		t.Fatalf("expected plan path %s, got %s", planAbs, applied.CSVFile)
	}
	if applied.CookiesFile != cookiesAbs {
		t.Fatalf("expected cookies path %s, got %s", cookiesAbs, applied.CookiesFile)
	}
}

func TestApplyConfigNoOverrides(t *testing.T) {
	root := t.TempDir()
	pp := ProjectPaths{
		Root:        root,
		ConfigFile:  filepath.Join(root, "powerhour.yaml"),
		CSVFile:     filepath.Join(root, "powerhour.csv"),
		CookiesFile: filepath.Join(root, "cookies.txt"),
	}

	applied := ApplyConfig(pp, config.Config{})

	if applied.CSVFile != pp.CSVFile {
		t.Fatalf("expected plan path unchanged")
	}
	if applied.CookiesFile != pp.CookiesFile {
		t.Fatalf("expected cookies path unchanged")
	}
}
