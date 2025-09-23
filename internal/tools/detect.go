package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Detect returns the status of each known tool, updating the manifest when new
// information is discovered.
func Detect(ctx context.Context) ([]Status, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	manifest, err := loadManifest()
	if err != nil {
		return nil, err
	}

	var statuses []Status
	changed := false

	for _, name := range KnownTools() {
		def, _ := Definition(name)
		status, entry, dirty := detectOne(ctx, def, manifest.Entries[name])
		statuses = append(statuses, status)
		if dirty {
			if entry.Tool == "" {
				delete(manifest.Entries, name)
			} else {
				if manifest.Entries == nil {
					manifest.Entries = map[string]ManifestEntry{}
				}
				manifest.Entries[name] = entry
			}
			changed = true
		}
	}

	if changed {
		if err := saveManifest(manifest); err != nil {
			return nil, err
		}
	}

	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Tool < statuses[j].Tool })
	return statuses, nil
}

func detectOne(ctx context.Context, def ToolDefinition, entry ManifestEntry) (Status, ManifestEntry, bool) {
	status := Status{Tool: def.Name, Minimum: def.MinimumVersion, Paths: map[string]string{}}
	dirty := false

	// Validate manifest entry if present.
	if entry.Tool != "" {
		if ok := validateManifestEntry(entry, def); ok {
			version, err := readVersion(ctx, def, entry.Paths)
			if err == nil {
				status.Version = version
				status.Path = entry.Paths[def.Binaries[0].ID]
				status.Paths = entry.Paths
				status.Source = entry.Source
				status.Checksum = entry.Checksum
				status.InstalledAt = entry.InstalledAt
				status.Satisfied = meetsMinimum(version, def.MinimumVersion)
				if !status.Satisfied {
					status.Error = fmt.Sprintf("version %s below minimum %s", version, def.MinimumVersion)
				}
				return status, entry, false
			}
			status.Notes = append(status.Notes, fmt.Sprintf("manifest entry invalid: %v", err))
		}
	}

	// Attempt to locate cached binaries.
	cachePaths, err := locateCache(def)
	if err == nil && len(cachePaths) > 0 {
		version, verr := readVersion(ctx, def, cachePaths)
		if verr == nil {
			status.Version = version
			status.Path = cachePaths[def.Binaries[0].ID]
			status.Paths = cachePaths
			status.Source = SourceCache
			status.Satisfied = meetsMinimum(version, def.MinimumVersion)
			if !status.Satisfied {
				status.Error = fmt.Sprintf("version %s below minimum %s", version, def.MinimumVersion)
			}

			checksum, csErr := computeChecksum(cachePaths[def.Binaries[0].ID])
			if csErr != nil {
				status.Notes = append(status.Notes, fmt.Sprintf("checksum error: %v", csErr))
			} else {
				status.Checksum = checksum
			}

			entry = ManifestEntry{
				Tool:        def.Name,
				Version:     version,
				Source:      SourceCache,
				Paths:       cachePaths,
				Checksum:    status.Checksum,
				InstalledAt: time.Now().UTC().Format(time.RFC3339),
			}
			dirty = true
			return status, entry, dirty
		}
	}

	// Fallback to system PATH.
	systemPaths, err := locateSystem(def)
	if err != nil {
		status.Error = err.Error()
		if entry.Tool != "" {
			return status, ManifestEntry{}, true
		}
		return status, ManifestEntry{}, false
	}

	version, err := readVersion(ctx, def, systemPaths)
	if err != nil {
		status.Error = err.Error()
		status.Paths = systemPaths
		status.Path = systemPaths[def.Binaries[0].ID]
		if entry.Tool != "" {
			return status, ManifestEntry{}, true
		}
		return status, ManifestEntry{}, false
	}

	status.Version = version
	status.Path = systemPaths[def.Binaries[0].ID]
	status.Paths = systemPaths
	status.Source = SourceSystem
	status.Satisfied = meetsMinimum(version, def.MinimumVersion)
	if !status.Satisfied {
		status.Error = fmt.Sprintf("version %s below minimum %s", version, def.MinimumVersion)
	}

	newEntry := ManifestEntry{
		Tool:        def.Name,
		Version:     version,
		Source:      SourceSystem,
		Paths:       systemPaths,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}

	if entry.Tool == "" || entry.Source != SourceSystem || !equalPathMaps(entry.Paths, systemPaths) || entry.Version != version {
		dirty = true
		entry = newEntry
	}

	return status, entry, dirty
}

func validateManifestEntry(entry ManifestEntry, def ToolDefinition) bool {
	if entry.Tool != def.Name {
		return false
	}
	if len(entry.Paths) == 0 {
		return false
	}
	for _, bin := range def.Binaries {
		path, ok := entry.Paths[bin.ID]
		if !ok {
			return false
		}
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func locateCache(def ToolDefinition) (map[string]string, error) {
	root, err := cacheRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, def.Name)
	infos, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// choose latest entry by name sort descending.
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].Name() > infos[j].Name() })
	for _, info := range infos {
		if !info.IsDir() {
			continue
		}
		candidate := map[string]string{}
		ok := true
		for _, bin := range def.Binaries {
			path := filepath.Join(dir, info.Name(), bin.Executable)
			if _, err := os.Stat(path); err != nil {
				ok = false
				break
			}
			candidate[bin.ID] = path
		}
		if ok {
			return candidate, nil
		}
	}
	return nil, errors.New("no cached binaries found")
}

func locateSystem(def ToolDefinition) (map[string]string, error) {
	paths := map[string]string{}
	for _, bin := range def.Binaries {
		path, err := exec.LookPath(bin.Executable)
		if err != nil {
			return nil, fmt.Errorf("%s not found in PATH", bin.Executable)
		}
		paths[bin.ID] = path
	}
	return paths, nil
}

func equalPathMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
