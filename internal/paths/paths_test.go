package paths

import (
	"os"
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

func TestApplyLibraryShared(t *testing.T) {
	tmp := t.TempDir()
	pp := ProjectPaths{
		CacheDir:          "/project/cache",
		IndexFile:         "/project/.powerhour/index.json",
		LibrarySourcesDir: filepath.Join(tmp, "library", "sources"),
		LibraryIndexFile:  filepath.Join(tmp, "library", "index.json"),
	}

	result := ApplyLibrary(pp, true, "")
	if result.CacheDir != result.LibrarySourcesDir {
		t.Fatalf("expected CacheDir=%s, got %s", result.LibrarySourcesDir, result.CacheDir)
	}
	if result.IndexFile != result.LibraryIndexFile {
		t.Fatalf("expected IndexFile=%s, got %s", result.LibraryIndexFile, result.IndexFile)
	}
}

func TestApplyLibraryLocal(t *testing.T) {
	pp := ProjectPaths{
		CacheDir:          "/project/cache",
		IndexFile:         "/project/.powerhour/index.json",
		LibrarySourcesDir: "/home/user/.powerhour/library/sources",
		LibraryIndexFile:  "/home/user/.powerhour/library/index.json",
	}

	result := ApplyLibrary(pp, false, "")
	if result.CacheDir != "/project/cache" {
		t.Fatalf("expected CacheDir unchanged, got %s", result.CacheDir)
	}
	if result.IndexFile != "/project/.powerhour/index.json" {
		t.Fatalf("expected IndexFile unchanged, got %s", result.IndexFile)
	}
}

func TestApplyLibraryConfigPath(t *testing.T) {
	tmp := t.TempDir()
	customLib := filepath.Join(tmp, "custom-lib")

	pp := ProjectPaths{
		CacheDir:  "/project/cache",
		IndexFile: "/project/.powerhour/index.json",
	}

	result := ApplyLibrary(pp, true, customLib)
	expectedSources := filepath.Join(customLib, "sources")
	expectedIndex := filepath.Join(customLib, "index.json")
	if result.CacheDir != expectedSources {
		t.Fatalf("expected CacheDir=%s, got %s", expectedSources, result.CacheDir)
	}
	if result.IndexFile != expectedIndex {
		t.Fatalf("expected IndexFile=%s, got %s", expectedIndex, result.IndexFile)
	}
}

func TestApplyLibraryEnvVar(t *testing.T) {
	tmp := t.TempDir()
	envLib := filepath.Join(tmp, "env-lib")

	t.Setenv("POWERHOUR_LIBRARY", envLib)

	pp := ProjectPaths{
		CacheDir:  "/project/cache",
		IndexFile: "/project/.powerhour/index.json",
	}

	result := ApplyLibrary(pp, true, "/config/path/ignored")
	expectedSources := filepath.Join(envLib, "sources")
	expectedIndex := filepath.Join(envLib, "index.json")
	if result.CacheDir != expectedSources {
		t.Fatalf("expected CacheDir=%s, got %s", expectedSources, result.CacheDir)
	}
	if result.IndexFile != expectedIndex {
		t.Fatalf("expected IndexFile=%s, got %s", expectedIndex, result.IndexFile)
	}
}

func TestDefaultLibrarySourcesDir(t *testing.T) {
	dir, err := DefaultLibrarySourcesDir()
	if err != nil {
		t.Fatalf("DefaultLibrarySourcesDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("expected absolute path, got %s", dir)
	}
	if filepath.Base(dir) != "sources" {
		t.Fatalf("expected dir named 'sources', got %s", filepath.Base(dir))
	}
}

func TestDefaultLibraryIndexFile(t *testing.T) {
	path, err := DefaultLibraryIndexFile()
	if err != nil {
		t.Fatalf("DefaultLibraryIndexFile: %v", err)
	}
	if filepath.Base(path) != "index.json" {
		t.Fatalf("expected file named 'index.json', got %s", filepath.Base(path))
	}
}

func TestLibraryDirEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	envDir := filepath.Join(tmp, "my-library")

	t.Setenv("POWERHOUR_LIBRARY", envDir)

	dir, err := LibraryDir("")
	if err != nil {
		t.Fatalf("LibraryDir: %v", err)
	}
	if dir != envDir {
		t.Fatalf("expected %s, got %s", envDir, dir)
	}
	// Verify it was created
	if _, err := os.Stat(envDir); err != nil {
		t.Fatalf("expected directory to be created: %v", err)
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
