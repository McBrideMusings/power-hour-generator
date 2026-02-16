package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	// Apply segments base directory from config
	if segmentsBase := strings.TrimSpace(cfg.SegmentsBaseDir); segmentsBase != "" {
		pp.SegmentsDir = resolveProjectPath(pp.Root, segmentsBase)
	}
	return pp
}

// CollectionOutputDir returns the output directory for a specific collection.
func (p ProjectPaths) CollectionOutputDir(cfg config.Config, collectionName string) string {
	collection, ok := cfg.Collections[collectionName]
	if !ok {
		// Fallback to collection name under segments dir
		return filepath.Join(p.SegmentsDir, collectionName)
	}

	outputDir := strings.TrimSpace(collection.OutputDir)
	if outputDir == "" {
		outputDir = collectionName
	}

	// If outputDir is relative, it's relative to SegmentsDir
	if filepath.IsAbs(outputDir) {
		return filepath.Clean(outputDir)
	}
	return filepath.Join(p.SegmentsDir, outputDir)
}

// EnsureCollectionDirs creates output directories for all configured collections.
func (p ProjectPaths) EnsureCollectionDirs(cfg config.Config) error {
	if cfg.Collections == nil {
		return nil
	}

	for name := range cfg.Collections {
		dir := p.CollectionOutputDir(cfg, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create collection %q output dir: %w", name, err)
		}
	}
	return nil
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

// GlobalDir returns the user-level powerhour directory (~/.powerhour).
// It creates the directory if it does not exist.
func GlobalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("detect user home: %w", err)
	}
	dir := filepath.Join(home, ".powerhour")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create global dir: %w", err)
	}
	return dir, nil
}

// GlobalLogsDir returns the global logs directory (~/.powerhour/logs).
// It creates the directory if it does not exist.
func GlobalLogsDir() (string, error) {
	global, err := GlobalDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(global, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create global logs dir: %w", err)
	}
	return dir, nil
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
