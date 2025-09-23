package tools

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// InstallOptions configures install behaviour.
type InstallOptions struct {
	Force   bool
	Version string
}

// Install downloads and installs the requested tool version into the cache.
func Install(ctx context.Context, toolName string, version string, opts InstallOptions) (Status, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
	}

	def, ok := Definition(toolName)
	if !ok {
		return Status{}, fmt.Errorf("unknown tool: %s", toolName)
	}

	current, err := currentStatus(ctx, toolName)
	if err != nil {
		return Status{Tool: toolName, Error: err.Error()}, err
	}

	requestedVersion := resolveRequestedVersion(def, current, version, opts)
	if requestedVersion == "" {
		requestedVersion = def.DefaultVersion
	}

	if current.Source == SourceCache && current.Satisfied && !opts.Force {
		if requestedVersion == "" || requestedVersion == current.Version {
			return current, nil
		}
	}

	unlock, err := acquireInstallLock(ctx, def.Name)
	if err != nil {
		return Status{Tool: toolName, Error: err.Error()}, err
	}
	defer unlock()

	current, err = currentStatus(ctx, toolName)
	if err != nil {
		return Status{Tool: toolName, Error: err.Error()}, err
	}
	if current.Source == SourceCache && current.Satisfied && !opts.Force {
		if requestedVersion == "" || requestedVersion == current.Version {
			return current, nil
		}
	}

	var fallbackNotes []string

	spec, ok, lookupErr := resolveRelease(ctx, def.Name, requestedVersion)
	if ok {
		relStatus, installErr := installFromRelease(ctx, def, spec, opts)
		if installErr == nil {
			return relStatus, nil
		}
		fallbackNotes = append(fallbackNotes, fmt.Sprintf("release install failed: %v", installErr))
	} else if lookupErr != nil {
		fallbackNotes = append(fallbackNotes, fmt.Sprintf("release lookup failed: %v", lookupErr))
	}

	status, systemErr := installFromSystem(ctx, def, requestedVersion, opts, current, fallbackNotes)
	if systemErr == nil {
		return status, nil
	}
	return status, systemErr
}

func currentStatus(ctx context.Context, toolName string) (Status, error) {
	statuses, err := Detect(ctx)
	if err != nil {
		return Status{}, err
	}
	for _, st := range statuses {
		if st.Tool == toolName {
			return st, nil
		}
	}
	return Status{Tool: toolName}, nil
}

func resolveRequestedVersion(def ToolDefinition, current Status, version string, opts InstallOptions) string {
	if version != "" {
		return version
	}
	if opts.Version != "" {
		return opts.Version
	}
	if current.Version != "" {
		return current.Version
	}
	if def.Name == "yt-dlp" {
		return ""
	}
	return def.DefaultVersion
}

func installFromRelease(ctx context.Context, def ToolDefinition, spec releaseSpec, opts InstallOptions) (Status, error) {
	notes := []string{fmt.Sprintf("downloaded release %s", spec.Version)}

	if spec.URL == "" {
		return Status{Tool: def.Name, Notes: notes}, fmt.Errorf("release metadata missing download url")
	}

	downloads, err := downloadsDir()
	if err != nil {
		return Status{Tool: def.Name, Notes: notes}, err
	}
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		return Status{Tool: def.Name, Notes: notes}, fmt.Errorf("prepare downloads dir: %w", err)
	}

	archivePath, err := resolveArchivePath(downloads, spec.URL)
	if err != nil {
		return Status{Tool: def.Name, Notes: notes}, err
	}

	if err := ensureDownload(ctx, archivePath, spec.URL, spec.Checksum, opts.Force); err != nil {
		return Status{Tool: def.Name, Notes: notes}, err
	}

	sourcePaths := map[string]string{}
	cleanup := func() {}

	switch spec.Archive {
	case archiveFormatNone:
		if len(def.Binaries) != 1 {
			return Status{Tool: def.Name, Notes: notes}, fmt.Errorf("release format none requires single binary")
		}
		sourcePaths[def.Binaries[0].ID] = archivePath
	case archiveFormatZip, archiveFormatTarGz, archiveFormatTarXz:
		extractDir, err := os.MkdirTemp(downloads, def.Name+"-extract-")
		if err != nil {
			return Status{Tool: def.Name, Notes: notes}, fmt.Errorf("create extract dir: %w", err)
		}
		cleanup = func() {
			_ = os.RemoveAll(extractDir)
		}
		if err := extractArchive(ctx, spec.Archive, archivePath, extractDir); err != nil {
			cleanup()
			return Status{Tool: def.Name, Notes: notes}, err
		}
		for _, bin := range def.Binaries {
			rel := ""
			if spec.Files != nil {
				rel = spec.Files[bin.ID]
			}
			var candidate string
			if rel != "" {
				candidate = filepath.Join(extractDir, filepath.FromSlash(rel))
			} else {
				candidate, err = findExecutable(extractDir, bin.Executable)
				if err != nil {
					cleanup()
					return Status{Tool: def.Name, Notes: notes}, err
				}
			}
			if candidate == "" {
				cleanup()
				return Status{Tool: def.Name, Notes: notes}, fmt.Errorf("binary %s not found in archive", bin.ID)
			}
			sourcePaths[bin.ID] = candidate
		}
	default:
		notes = append(notes, "unsupported archive format; falling back to system copy")
		return Status{Tool: def.Name, Notes: notes}, fmt.Errorf("unsupported archive format %q", spec.Archive)
	}
	defer cleanup()

	version := spec.Version
	if version == "" {
		version = def.DefaultVersion
	}

	destPaths, checksum, err := cacheBinaries(def, version, sourcePaths, opts.Force)
	if err != nil {
		return Status{Tool: def.Name, Notes: notes}, err
	}

	notes = append(notes, "cached release binaries")
	status, err := saveCacheInstall(def, version, destPaths, checksum, notes)
	return status, err
}

func installFromSystem(ctx context.Context, def ToolDefinition, requestedVersion string, opts InstallOptions, current Status, extraNotes []string) (Status, error) {
	paths := current.Paths
	if len(paths) == 0 || current.Source != SourceSystem {
		var err error
		paths, err = locateSystem(def)
		if err != nil {
			st := Status{Tool: def.Name, Error: err.Error(), Notes: append([]string{}, extraNotes...)}
			return st, err
		}
	}

	version, err := readVersion(ctx, def, paths)
	if err != nil {
		st := Status{Tool: def.Name, Error: err.Error(), Notes: append([]string{}, extraNotes...)}
		return st, err
	}
	if requestedVersion != "" && requestedVersion != version {
		err := fmt.Errorf("requested version %s unavailable; system reports %s", requestedVersion, version)
		st := Status{Tool: def.Name, Error: err.Error(), Notes: append([]string{}, extraNotes...)}
		return st, err
	}

	notes := append([]string{}, extraNotes...)
	notes = append(notes, "copied binaries from system PATH")

	destPaths, checksum, err := cacheBinaries(def, version, paths, opts.Force)
	if err != nil {
		st := Status{Tool: def.Name, Error: err.Error(), Notes: notes}
		return st, err
	}

	status, err := saveCacheInstall(def, version, destPaths, checksum, notes)
	if err != nil {
		return Status{Tool: def.Name, Error: err.Error(), Notes: notes}, err
	}
	return status, nil
}

func cacheBinaries(def ToolDefinition, version string, sources map[string]string, force bool) (map[string]string, string, error) {
	root, err := cacheRoot()
	if err != nil {
		return nil, "", err
	}

	if err := os.MkdirAll(filepath.Join(root, def.Name), 0o755); err != nil {
		return nil, "", fmt.Errorf("prepare cache dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp(root, def.Name+"-tmp-")
	if err != nil {
		return nil, "", fmt.Errorf("create temp dir: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	for _, bin := range def.Binaries {
		src, ok := sources[bin.ID]
		if !ok {
			return nil, "", fmt.Errorf("missing source binary %s", bin.ID)
		}
		dest := filepath.Join(tmpDir, bin.Executable)
		if err := copyFile(src, dest); err != nil {
			return nil, "", fmt.Errorf("copy %s: %w", bin.ID, err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dest, 0o755); err != nil {
				return nil, "", fmt.Errorf("chmod %s: %w", bin.ID, err)
			}
		}
	}

	destDir := filepath.Join(root, def.Name, version)
	if err := os.RemoveAll(destDir); err != nil {
		return nil, "", fmt.Errorf("replace cache dir: %w", err)
	}
	if err := os.Rename(tmpDir, destDir); err != nil {
		return nil, "", fmt.Errorf("commit cache dir: %w", err)
	}
	committed = true

	destPaths := make(map[string]string, len(def.Binaries))
	for _, bin := range def.Binaries {
		destPaths[bin.ID] = filepath.Join(destDir, bin.Executable)
	}

	checksum, err := computeChecksum(destPaths[def.Binaries[0].ID])
	if err != nil {
		return nil, "", fmt.Errorf("checksum: %w", err)
	}

	return destPaths, checksum, nil
}

func saveCacheInstall(def ToolDefinition, version string, destPaths map[string]string, checksum string, notes []string) (Status, error) {
	manifest, err := loadManifest()
	if err != nil {
		return Status{Tool: def.Name, Error: err.Error(), Notes: notes}, err
	}
	if manifest.Entries == nil {
		manifest.Entries = map[string]ManifestEntry{}
	}

	installedAt := time.Now().UTC().Format(time.RFC3339)
	entry := ManifestEntry{
		Tool:        def.Name,
		Version:     version,
		Source:      SourceCache,
		Paths:       destPaths,
		Checksum:    checksum,
		InstalledAt: installedAt,
	}
	manifest.Entries[def.Name] = entry
	if err := saveManifest(manifest); err != nil {
		return Status{Tool: def.Name, Error: err.Error(), Notes: notes}, err
	}

	satisfied := meetsMinimum(version, def.MinimumVersion)
	status := Status{
		Tool:        def.Name,
		Version:     version,
		Minimum:     def.MinimumVersion,
		Source:      SourceCache,
		Path:        destPaths[def.Binaries[0].ID],
		Paths:       destPaths,
		InstalledAt: installedAt,
		Checksum:    checksum,
		Satisfied:   satisfied,
		Notes:       append([]string{}, notes...),
	}
	if !satisfied {
		status.Error = fmt.Sprintf("version %s below minimum %s", version, def.MinimumVersion)
	}
	return status, nil
}

func acquireInstallLock(ctx context.Context, tool string) (func(), error) {
	root, err := cacheRoot()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("prepare cache root: %w", err)
	}

	lockPath := filepath.Join(root, fmt.Sprintf("%s.lock", tool))
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire lock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func ensureDownload(ctx context.Context, dest, downloadURL, checksum string, force bool) error {
	if !force {
		if _, err := os.Stat(dest); err == nil {
			if checksum == "" {
				return nil
			}
			if match, err := verifyChecksum(dest, checksum); err == nil && match {
				return nil
			}
		}
	}

	return downloadArtifact(ctx, dest, downloadURL, checksum)
}

func downloadArtifact(ctx context.Context, dest, downloadURL, checksum string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("prepare download destination: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "powerhour/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: unexpected status %s", downloadURL, resp.Status)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(dest), "download-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if checksum != "" {
		match, err := verifyChecksum(tmpPath, checksum)
		if err != nil {
			return err
		}
		if !match {
			return fmt.Errorf("checksum mismatch for %s", downloadURL)
		}
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("finalize download: %w", err)
	}
	return nil
}

func verifyChecksum(path, expected string) (bool, error) {
	sum, err := computeChecksum(path)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(sum, expected), nil
}

func resolveArchivePath(downloadsDir, downloadURL string) (string, error) {
	parsed, err := url.Parse(downloadURL)
	if err != nil {
		return "", fmt.Errorf("parse download url: %w", err)
	}
	base := path.Base(parsed.Path)
	if base == "." || base == "" || base == "/" {
		return "", fmt.Errorf("infer archive name from url: %s", downloadURL)
	}
	return filepath.Join(downloadsDir, base), nil
}

func extractArchive(ctx context.Context, format archiveFormat, archivePath, dest string) error {
	switch format {
	case archiveFormatZip:
		return extractZip(archivePath, dest)
	case archiveFormatTarGz:
		return extractTarGz(archivePath, dest)
	case archiveFormatTarXz:
		return extractTarXz(ctx, archivePath, dest)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

func extractZip(archivePath, dest string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		target := filepath.Join(dest, filepath.FromSlash(file.Name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, file.Mode()); err != nil {
				return fmt.Errorf("create dir %s: %w", target, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("prepare file %s: %w", target, err)
		}
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", file.Name, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("create file %s: %w", target, err)
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return fmt.Errorf("copy file %s: %w", target, err)
		}
		rc.Close()
		if err := out.Close(); err != nil {
			return fmt.Errorf("close file %s: %w", target, err)
		}
	}
	return nil
}

func extractTarGz(archivePath, dest string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	return untarStream(gz, dest)
}

func extractTarXz(ctx context.Context, archivePath, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("prepare extract dir: %w", err)
	}
	cmd := exec.CommandContext(ctx, "tar", "-xJf", archivePath, "-C", dest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar extract: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func untarStream(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}
		target := filepath.Join(dest, filepath.FromSlash(header.Name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("create dir %s: %w", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("prepare file %s: %w", target, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("write file %s: %w", target, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", target, err)
			}
		default:
			// Ignore other entry types.
		}
	}
	return nil
}

func findExecutable(root, name string) (string, error) {
	var match string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == name {
			match = path
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return match, nil
}

func computeChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open for checksum: %w", err)
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dest.Close()

	if _, err := io.Copy(dest, source); err != nil {
		return err
	}
	return nil
}

// Ensure makes sure the requested tool is available, attempting installation if required.
func Ensure(ctx context.Context, toolName string) (Status, error) {
	statuses, err := Detect(ctx)
	if err != nil {
		return Status{}, err
	}
	for _, status := range statuses {
		if status.Tool == toolName {
			if status.Satisfied {
				return status, nil
			}
			return Install(ctx, toolName, "", InstallOptions{})
		}
	}
	return Install(ctx, toolName, "", InstallOptions{})
}

// Lookup returns the main binary path for the requested tool if recorded in the manifest.
func Lookup(toolName string) (string, error) {
	def, ok := Definition(toolName)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
	manifest, err := loadManifest()
	if err != nil {
		return "", err
	}
	entry, ok := manifest.Entries[toolName]
	if !ok {
		return "", fmt.Errorf("tool %s not recorded in manifest", toolName)
	}
	path, ok := entry.Paths[def.Binaries[0].ID]
	if !ok {
		return "", fmt.Errorf("manifest entry for %s missing binary %s", toolName, def.Binaries[0].ID)
	}
	return path, nil
}

// Probe is kept for backward compatibility with older callers. It proxies to Detect.
func Probe(ctx context.Context) map[string]Status {
	statuses, err := Detect(ctx)
	if err != nil {
		return map[string]Status{
			"error": {Tool: "error", Error: err.Error()},
		}
	}
	result := make(map[string]Status, len(statuses))
	for _, status := range statuses {
		result[status.Tool] = status
	}
	return result
}
