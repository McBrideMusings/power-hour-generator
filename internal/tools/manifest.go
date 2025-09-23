package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const manifestFileName = "manifest.json"

// cacheRoot determines the per-user cache directory for tool downloads.
func cacheRoot() (string, error) {
	if override, ok := os.LookupEnv("POWERHOUR_TOOLS_DIR"); ok && override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("resolve POWERHOUR_TOOLS_DIR: %w", err)
		}
		return abs, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("detect user home: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "PowerHour", "bin"), nil
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "PowerHour", "bin"), nil
		}
		return filepath.Join(home, "AppData", "Local", "PowerHour", "bin"), nil
	default:
		return filepath.Join(home, ".local", "share", "powerhour", "bin"), nil
	}
}

func downloadsDir() (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "downloads"), nil
}

func manifestPath() (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, manifestFileName), nil
}

func loadManifest() (Manifest, error) {
	path, err := manifestPath()
	if err != nil {
		return Manifest{}, err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{Entries: map[string]ManifestEntry{}}, nil
		}
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("unmarshal manifest: %w", err)
	}
	if manifest.Entries == nil {
		manifest.Entries = map[string]ManifestEntry{}
	}
	return manifest, nil
}

func saveManifest(m Manifest) error {
	path, err := manifestPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare manifest directory: %w", err)
	}

	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "manifest-*.json")
	if err != nil {
		return fmt.Errorf("create temp manifest: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return fmt.Errorf("write manifest temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close manifest temp: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
}
