package tools

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	InstallMethodHomebrew = "homebrew"
	InstallMethodApt      = "apt"
	InstallMethodSnap     = "snap"
	InstallMethodPip      = "pip"
	InstallMethodManaged  = "managed"
	InstallMethodSystem   = "system"
	InstallMethodUnknown  = "unknown"
)

// DetectFFmpegInstallMethod is a convenience wrapper for detecting how ffmpeg
// was installed. Exported for use by the render package.
func DetectFFmpegInstallMethod(ffmpegPath string) string {
	return detectInstallMethod(ffmpegPath)
}

// detectInstallMethod determines how a tool binary was installed based on its
// resolved path. It resolves symlinks first so Homebrew-managed binaries
// (symlinked from /usr/local/bin) are correctly identified.
func detectInstallMethod(binaryPath string) string {
	resolved, err := filepath.EvalSymlinks(binaryPath)
	if err != nil {
		resolved = binaryPath
	}

	if isUnderCacheRoot(resolved) {
		return InstallMethodManaged
	}

	if isHomebrew(resolved) {
		return InstallMethodHomebrew
	}

	if runtime.GOOS == "linux" {
		if strings.HasPrefix(resolved, "/snap/") {
			return InstallMethodSnap
		}
		if isDpkgManaged(resolved) {
			return InstallMethodApt
		}
	}

	if isPipManaged(resolved) {
		return InstallMethodPip
	}

	return InstallMethodSystem
}

func isUnderCacheRoot(resolved string) bool {
	root, err := cacheRoot()
	if err != nil {
		return false
	}
	return strings.HasPrefix(resolved, root+string(filepath.Separator))
}

func isHomebrew(resolved string) bool {
	lower := strings.ToLower(resolved)
	if strings.Contains(lower, "/homebrew/") || strings.Contains(lower, "/cellar/") {
		return true
	}
	brewPrefix := brewPrefixCached()
	if brewPrefix != "" && strings.HasPrefix(resolved, brewPrefix+string(filepath.Separator)) {
		return true
	}
	return false
}

var cachedBrewPrefix string
var brewPrefixChecked bool

func brewPrefixCached() string {
	if brewPrefixChecked {
		return cachedBrewPrefix
	}
	brewPrefixChecked = true
	out, err := exec.Command("brew", "--prefix").Output()
	if err != nil {
		return ""
	}
	cachedBrewPrefix = strings.TrimSpace(string(out))
	return cachedBrewPrefix
}

func isDpkgManaged(resolved string) bool {
	if _, err := exec.LookPath("dpkg"); err != nil {
		return false
	}
	err := exec.Command("dpkg", "-S", resolved).Run()
	return err == nil
}

func isPipManaged(resolved string) bool {
	lower := strings.ToLower(resolved)
	return strings.Contains(lower, "site-packages") || strings.Contains(lower, "/pipx/")
}
