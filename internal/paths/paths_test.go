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

func TestApplyGlobalCacheEnabled(t *testing.T) {
	pp := ProjectPaths{
		CacheDir:        "/project/cache",
		IndexFile:       "/project/.powerhour/index.json",
		GlobalCacheDir:  "/home/user/.powerhour/cache",
		GlobalIndexFile: "/home/user/.powerhour/index.json",
	}

	result := ApplyGlobalCache(pp, true)
	if result.CacheDir != pp.GlobalCacheDir {
		t.Fatalf("expected CacheDir=%s, got %s", pp.GlobalCacheDir, result.CacheDir)
	}
	if result.IndexFile != pp.GlobalIndexFile {
		t.Fatalf("expected IndexFile=%s, got %s", pp.GlobalIndexFile, result.IndexFile)
	}
}

func TestApplyGlobalCacheDisabled(t *testing.T) {
	pp := ProjectPaths{
		CacheDir:        "/project/cache",
		IndexFile:       "/project/.powerhour/index.json",
		GlobalCacheDir:  "/home/user/.powerhour/cache",
		GlobalIndexFile: "/home/user/.powerhour/index.json",
	}

	result := ApplyGlobalCache(pp, false)
	if result.CacheDir != "/project/cache" {
		t.Fatalf("expected CacheDir unchanged, got %s", result.CacheDir)
	}
	if result.IndexFile != "/project/.powerhour/index.json" {
		t.Fatalf("expected IndexFile unchanged, got %s", result.IndexFile)
	}
}

func TestApplyGlobalCacheEmptyGlobalPaths(t *testing.T) {
	pp := ProjectPaths{
		CacheDir:  "/project/cache",
		IndexFile: "/project/.powerhour/index.json",
		// GlobalCacheDir and GlobalIndexFile are empty
	}

	result := ApplyGlobalCache(pp, true)
	if result.CacheDir != "/project/cache" {
		t.Fatalf("expected CacheDir unchanged when global paths empty, got %s", result.CacheDir)
	}
}

func TestGlobalCacheDirCreatesDir(t *testing.T) {
	dir, err := GlobalCacheDir()
	if err != nil {
		t.Fatalf("GlobalCacheDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("expected absolute path, got %s", dir)
	}
	if filepath.Base(dir) != "cache" {
		t.Fatalf("expected dir named 'cache', got %s", filepath.Base(dir))
	}
}

func TestGlobalIndexFile(t *testing.T) {
	path, err := GlobalIndexFile()
	if err != nil {
		t.Fatalf("GlobalIndexFile: %v", err)
	}
	if filepath.Base(path) != "index.json" {
		t.Fatalf("expected file named 'index.json', got %s", filepath.Base(path))
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
