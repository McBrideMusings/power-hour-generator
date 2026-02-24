package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// resolveExternalPath returns path as-is if absolute, otherwise joins it with projectRoot.
func resolveExternalPath(projectRoot, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(projectRoot, path)
}

// loadProfileFiles reads each file in ProfileFiles, unmarshals as map[string]OverlayProfile,
// and merges into c.Profiles with duplicate detection.
func (c *Config) loadProfileFiles(projectRoot string) error {
	if len(c.ProfileFiles) == 0 {
		return nil
	}

	if c.Profiles == nil {
		c.Profiles = ProfilesConfig{}
	}

	// Track where each profile name was defined for duplicate detection.
	sources := make(map[string]string, len(c.Profiles))
	for name := range c.Profiles {
		sources[name] = "inline config"
	}

	for _, relPath := range c.ProfileFiles {
		absPath := resolveExternalPath(projectRoot, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("load profile file %q: %w", relPath, err)
		}

		var profiles map[string]OverlayProfile
		if err := yaml.Unmarshal(data, &profiles); err != nil {
			return fmt.Errorf("parse profile file %q: %w", relPath, err)
		}

		for name, profile := range profiles {
			if existing, ok := sources[name]; ok {
				return fmt.Errorf("profile %q defined in both %s and %q", name, existing, relPath)
			}
			sources[name] = relPath
			c.Profiles[name] = profile
		}
	}

	return nil
}

// loadCollectionFiles reads each file in CollectionFiles, unmarshals as map[string]CollectionConfig,
// and merges into c.Collections with duplicate detection.
func (c *Config) loadCollectionFiles(projectRoot string) error {
	if len(c.CollectionFiles) == 0 {
		return nil
	}

	if c.Collections == nil {
		c.Collections = map[string]CollectionConfig{}
	}

	// Track where each collection name was defined for duplicate detection.
	sources := make(map[string]string, len(c.Collections))
	for name := range c.Collections {
		sources[name] = "inline config"
	}

	for _, relPath := range c.CollectionFiles {
		absPath := resolveExternalPath(projectRoot, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("load collection file %q: %w", relPath, err)
		}

		var collections map[string]CollectionConfig
		if err := yaml.Unmarshal(data, &collections); err != nil {
			return fmt.Errorf("parse collection file %q: %w", relPath, err)
		}

		for name, collection := range collections {
			if existing, ok := sources[name]; ok {
				return fmt.Errorf("collection %q defined in both %s and %q", name, existing, relPath)
			}
			sources[name] = relPath
			c.Collections[name] = collection
		}
	}

	return nil
}
