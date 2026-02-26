package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// moveFile moves a file from src to dest, falling back to copy+remove for
// cross-device moves.
func moveFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	// Fallback: copy + remove (cross-device)
	return copyAndRemove(src, dest)
}

func copyAndRemove(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		os.Remove(dest) // clean up partial
		return fmt.Errorf("copy: %w", err)
	}

	if err := destFile.Close(); err != nil {
		return fmt.Errorf("close dest: %w", err)
	}
	srcFile.Close()

	return os.Remove(src)
}

// deduplicateFilename adds a numeric suffix to avoid filename collisions.
func deduplicateFilename(dir, base string) string {
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
