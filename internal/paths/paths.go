package paths

import (
	"fmt"
	"os"
	"path/filepath"

	"powerhour/internal/config"
)

// ProjectPaths captures canonical locations for a powerhour project.
type ProjectPaths struct {
	Root        string
	ConfigFile  string
	CSVFile     string
	CookiesFile string
	MetaDir     string
	CacheDir    string
	SegmentsDir string
	LogsDir     string
	IndexFile   string
}

// Resolve determines the project root using the optional --project flag or the
// current working directory when the flag is empty.
func Resolve(projectFlag string) (ProjectPaths, error) {
	var (
		root string
		err  error
	)

	if projectFlag != "" {
		root, err = filepath.Abs(projectFlag)
	} else {
		root, err = os.Getwd()
	}
	if err != nil {
		return ProjectPaths{}, fmt.Errorf("resolve project root: %w", err)
	}

	return newProjectPaths(root), nil
}

func newProjectPaths(root string) ProjectPaths {
	metaDir := filepath.Join(root, ".powerhour")
	return ProjectPaths{
		Root:        root,
		ConfigFile:  filepath.Join(root, "powerhour.yaml"),
		CSVFile:     filepath.Join(root, "powerhour.csv"),
		CookiesFile: filepath.Join(root, "cookies.txt"),
		MetaDir:     metaDir,
		CacheDir:    filepath.Join(root, "cache"),
		SegmentsDir: filepath.Join(root, "segments"),
		LogsDir:     filepath.Join(root, "logs"),
		IndexFile:   filepath.Join(metaDir, "index.json"),
	}
}

func ApplyConfig(pp ProjectPaths, cfg config.Config) ProjectPaths {
	if plan := cfg.PlanFile(); plan != "" {
		pp.CSVFile = resolveProjectPath(pp.Root, plan)
	}
	if cookies := cfg.CookiesFile(); cookies != "" {
		pp.CookiesFile = resolveProjectPath(pp.Root, cookies)
	}
	return pp
}

func resolveProjectPath(root, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, value)
}

// EnsureRoot makes sure the project root exists on disk.
func (p ProjectPaths) EnsureRoot() error {
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		return fmt.Errorf("create project root: %w", err)
	}
	return nil
}

// EnsureMetaDirs creates the standard cache/logs/segments hierarchy alongside
// the hidden .powerhour metadata directory.
func (p ProjectPaths) EnsureMetaDirs() error {
	dirs := []string{p.MetaDir, p.CacheDir, p.SegmentsDir, p.LogsDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

// FileExists reports whether a path exists and is a regular file.
func FileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

// DirExists reports whether a path exists and is a directory.
func DirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}
