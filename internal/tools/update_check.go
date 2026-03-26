package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	updateCheckFile = "update_check.json"
	updateCheckTTL  = 24 * time.Hour
)

// UpdateCheckCache stores the results of periodic update checks.
type UpdateCheckCache struct {
	Entries map[string]UpdateCheckEntry `json:"entries"`
}

// UpdateCheckEntry records the result of an update check for a single tool.
type UpdateCheckEntry struct {
	Tool            string    `json:"tool"`
	LatestVersion   string    `json:"latest_version"`
	CurrentVersion  string    `json:"current_version"`
	CheckedAt       time.Time `json:"checked_at"`
	NotifiedVersion string    `json:"notified_version,omitempty"`
	CheckFailed     bool      `json:"check_failed,omitempty"`
	InstallMethod   string    `json:"install_method,omitempty"`
}

// UpdateNotice represents an available update for a tool.
type UpdateNotice struct {
	Tool           string
	CurrentVersion string
	LatestVersion  string
	InstallMethod  string
}

// UpdateCommand returns the command the user should run to update this tool.
func (n UpdateNotice) UpdateCommand() string {
	switch n.InstallMethod {
	case InstallMethodHomebrew:
		return "brew upgrade " + n.Tool
	case InstallMethodApt:
		return "sudo apt upgrade " + n.Tool
	case InstallMethodSnap:
		return "sudo snap refresh " + n.Tool
	case InstallMethodPip:
		return "pip install --upgrade " + n.Tool
	default:
		return "powerhour tools install " + n.Tool
	}
}

func updateCheckPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".powerhour", updateCheckFile), nil
}

func loadUpdateCheckCache() UpdateCheckCache {
	path, err := updateCheckPath()
	if err != nil {
		return UpdateCheckCache{Entries: map[string]UpdateCheckEntry{}}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return UpdateCheckCache{Entries: map[string]UpdateCheckEntry{}}
	}
	var cache UpdateCheckCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return UpdateCheckCache{Entries: map[string]UpdateCheckEntry{}}
	}
	if cache.Entries == nil {
		cache.Entries = map[string]UpdateCheckEntry{}
	}
	return cache
}

func saveUpdateCheckCache(cache UpdateCheckCache) {
	path, err := updateCheckPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// CheckForUpdates checks whether newer versions of detected tools are
// available. It uses a 24-hour cache to avoid hitting the network on every
// run. When the cache is stale or a previous check failed, it performs a
// fresh check. All errors are swallowed — this never blocks the CLI.
func CheckForUpdates(ctx context.Context, statuses []Status) []UpdateNotice {
	cache := loadUpdateCheckCache()
	var notices []UpdateNotice
	changed := false

	for _, st := range statuses {
		if !st.Satisfied || st.Version == "" {
			continue
		}

		entry, ok := cache.Entries[st.Tool]
		fresh := ok && !entry.CheckFailed && time.Since(entry.CheckedAt) < updateCheckTTL

		if fresh {
			if entry.LatestVersion != "" &&
				entry.LatestVersion != entry.NotifiedVersion &&
				versionNewer(entry.LatestVersion, st.Version) {
				notices = append(notices, UpdateNotice{
					Tool:           st.Tool,
					CurrentVersion: st.Version,
					LatestVersion:  entry.LatestVersion,
					InstallMethod:  st.InstallMethod,
				})
			}
			continue
		}

		latest, checkOK := checkLatestVersion(ctx, st.Tool, st.InstallMethod)
		newEntry := UpdateCheckEntry{
			Tool:           st.Tool,
			LatestVersion:  latest,
			CurrentVersion: st.Version,
			CheckedAt:      time.Now(),
			CheckFailed:    !checkOK,
			InstallMethod:  st.InstallMethod,
		}
		if ok {
			newEntry.NotifiedVersion = entry.NotifiedVersion
		}
		cache.Entries[st.Tool] = newEntry
		changed = true

		if checkOK && latest != "" &&
			latest != newEntry.NotifiedVersion &&
			versionNewer(latest, st.Version) {
			notices = append(notices, UpdateNotice{
				Tool:           st.Tool,
				CurrentVersion: st.Version,
				LatestVersion:  latest,
				InstallMethod:  st.InstallMethod,
			})
		}
	}

	if changed {
		saveUpdateCheckCache(cache)
	}
	return notices
}

// MarkNotified records that the user has been shown a notice for this version
// so it won't be shown again.
func MarkNotified(tool, version string) {
	cache := loadUpdateCheckCache()
	entry, ok := cache.Entries[tool]
	if !ok {
		return
	}
	entry.NotifiedVersion = version
	cache.Entries[tool] = entry
	saveUpdateCheckCache(cache)
}

// checkLatestVersion fetches the latest available version for a tool.
// For yt-dlp it checks the release cache then GitHub API.
// For ffmpeg it checks via Homebrew if that's how it was installed.
func checkLatestVersion(ctx context.Context, tool, installMethod string) (string, bool) {
	switch tool {
	case "yt-dlp":
		return checkLatestYtDlp(ctx)
	case "ffmpeg":
		return checkLatestFfmpeg(ctx, installMethod)
	default:
		return "", false
	}
}

func checkLatestYtDlp(ctx context.Context) (string, bool) {
	if ver, ok := LatestCachedRelease("yt-dlp"); ok {
		return ver, true
	}
	spec, err := fetchDynamicRelease(ctx, "yt-dlp", "")
	if err != nil {
		return "", false
	}
	cacheLatestRelease("yt-dlp", spec)
	return spec.Version, true
}

func checkLatestFfmpeg(ctx context.Context, installMethod string) (string, bool) {
	if installMethod != InstallMethodHomebrew {
		return "", false
	}
	return checkHomebrewLatest(ctx, "ffmpeg")
}

// checkHomebrewLatest queries Homebrew for the latest version of a formula.
func checkHomebrewLatest(ctx context.Context, formula string) (string, bool) {
	if _, err := exec.LookPath("brew"); err != nil {
		return "", false
	}
	cmd := exec.CommandContext(ctx, "brew", "info", "--json=v2", formula)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return parseBrewInfoVersion(out)
}

func parseBrewInfoVersion(data []byte) (string, bool) {
	var info struct {
		Formulae []struct {
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
		} `json:"formulae"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return "", false
	}
	if len(info.Formulae) == 0 || info.Formulae[0].Versions.Stable == "" {
		return "", false
	}
	return info.Formulae[0].Versions.Stable, true
}

// versionNewer returns true if latest is strictly newer than current.
func versionNewer(latest, current string) bool {
	if latest == current {
		return false
	}
	latestParts := extractNumericParts(latest)
	currentParts := extractNumericParts(current)
	return compareParts(latestParts, currentParts) > 0
}

func extractNumericParts(version string) []int {
	var parts []int
	var num int
	inNum := false
	for _, c := range version {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
			inNum = true
		} else {
			if inNum {
				parts = append(parts, num)
				num = 0
				inNum = false
			}
		}
	}
	if inNum {
		parts = append(parts, num)
	}
	return parts
}

func compareParts(a, b []int) int {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		va, vb := 0, 0
		if i < len(a) {
			va = a[i]
		}
		if i < len(b) {
			vb = b[i]
		}
		if va > vb {
			return 1
		}
		if va < vb {
			return -1
		}
	}
	return 0
}

// FormatUpdateTarget returns the latest known version for a tool from the
// update check cache. This is used when installing updates to target the
// specific version we know about.
func FormatUpdateTarget(tool string) string {
	cache := loadUpdateCheckCache()
	entry, ok := cache.Entries[tool]
	if !ok {
		return ""
	}
	return entry.LatestVersion
}

// ClearUpdateNotice removes the update check entry for a tool so it will be
// re-checked on the next run. Call this after a successful install/upgrade.
func ClearUpdateNotice(tool string) {
	cache := loadUpdateCheckCache()
	delete(cache.Entries, tool)
	saveUpdateCheckCache(cache)
}

// InstallMethodLabel returns a human-readable label for an install method.
func InstallMethodLabel(method string) string {
	switch method {
	case InstallMethodHomebrew:
		return "homebrew"
	case InstallMethodApt:
		return "apt"
	case InstallMethodSnap:
		return "snap"
	case InstallMethodPip:
		return "pip"
	case InstallMethodManaged:
		return "managed"
	case InstallMethodSystem:
		return "system PATH"
	default:
		return method
	}
}

// FormatUpdateHint returns a formatted string if an update is available for
// the given tool, suitable for appending to tools list output. Returns empty
// string if no update is available.
func FormatUpdateHint(tool, currentVersion string) string {
	cache := loadUpdateCheckCache()
	entry, ok := cache.Entries[tool]
	if !ok || entry.LatestVersion == "" || entry.CheckFailed {
		return ""
	}
	if !versionNewer(entry.LatestVersion, currentVersion) {
		return ""
	}
	notice := UpdateNotice{Tool: tool, InstallMethod: entry.InstallMethod}
	return fmt.Sprintf("update available: %s (%s)", entry.LatestVersion, notice.UpdateCommand())
}

