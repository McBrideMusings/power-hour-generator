package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// ProjectPaths captures canonical locations for a powerhour project.
type ProjectPaths struct {
	Root        string
	ConfigFile  string
	CSVFile     string
	MetaDir     string
	SrcDir      string
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
		MetaDir:     metaDir,
		SrcDir:      filepath.Join(metaDir, "src"),
		SegmentsDir: filepath.Join(metaDir, "segments"),
		LogsDir:     filepath.Join(metaDir, "logs"),
		IndexFile:   filepath.Join(metaDir, "index.json"),
	}
}

// EnsureRoot makes sure the project root exists on disk.
func (p ProjectPaths) EnsureRoot() error {
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		return fmt.Errorf("create project root: %w", err)
	}
	return nil
}

// EnsureMetaDirs creates the standard .powerhour cache hierarchy.
func (p ProjectPaths) EnsureMetaDirs() error {
	dirs := []string{p.MetaDir, p.SrcDir, p.SegmentsDir, p.LogsDir}
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
