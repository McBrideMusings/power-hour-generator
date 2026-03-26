package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVersionNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"2026.03.01", "2024.07.16", true},
		{"2024.07.16", "2024.07.16", false},
		{"2024.07.15", "2024.07.16", false},
		{"7.1", "6.0", true},
		{"6.0", "7.1", false},
		{"6.0", "6.0", false},
		{"2026.02.04", "2026.01.01", true},
	}
	for _, tt := range tests {
		t.Run(tt.latest+"_vs_"+tt.current, func(t *testing.T) {
			if got := versionNewer(tt.latest, tt.current); got != tt.want {
				t.Errorf("versionNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestExtractNumericParts(t *testing.T) {
	tests := []struct {
		input string
		want  []int
	}{
		{"2026.03.01", []int{2026, 3, 1}},
		{"7.1", []int{7, 1}},
		{"6.0.1", []int{6, 0, 1}},
		{"v2024.07.16", []int{2024, 7, 16}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractNumericParts(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("len mismatch: got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("part %d: got %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestUpdateCheckCacheTTL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update_check.json")

	cache := UpdateCheckCache{
		Entries: map[string]UpdateCheckEntry{
			"yt-dlp": {
				Tool:           "yt-dlp",
				LatestVersion:  "2026.03.01",
				CurrentVersion: "2024.07.16",
				CheckedAt:      time.Now().Add(-25 * time.Hour),
			},
		},
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var loaded UpdateCheckCache
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &loaded)

	entry := loaded.Entries["yt-dlp"]
	stale := time.Since(entry.CheckedAt) > updateCheckTTL
	if !stale {
		t.Error("expected entry to be stale (> 24h)")
	}
}

func TestUpdateCheckCacheFresh(t *testing.T) {
	cache := UpdateCheckCache{
		Entries: map[string]UpdateCheckEntry{
			"yt-dlp": {
				Tool:           "yt-dlp",
				LatestVersion:  "2026.03.01",
				CurrentVersion: "2024.07.16",
				CheckedAt:      time.Now(),
			},
		},
	}

	entry := cache.Entries["yt-dlp"]
	stale := time.Since(entry.CheckedAt) > updateCheckTTL
	if stale {
		t.Error("expected entry to be fresh")
	}
}

func TestParseBrewInfoVersion(t *testing.T) {
	data := []byte(`{
		"formulae": [{
			"versions": {
				"stable": "7.1"
			}
		}]
	}`)
	ver, ok := parseBrewInfoVersion(data)
	if !ok {
		t.Fatal("expected ok")
	}
	if ver != "7.1" {
		t.Errorf("got %q, want 7.1", ver)
	}
}

func TestParseBrewInfoVersion_Empty(t *testing.T) {
	_, ok := parseBrewInfoVersion([]byte(`{"formulae": []}`))
	if ok {
		t.Error("expected not ok for empty formulae")
	}
}

func TestUpdateNoticeCommand(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{InstallMethodHomebrew, "brew upgrade yt-dlp"},
		{InstallMethodManaged, "powerhour tools install yt-dlp"},
		{InstallMethodApt, "sudo apt upgrade yt-dlp"},
		{InstallMethodPip, "pip install --upgrade yt-dlp"},
		{"", "powerhour tools install yt-dlp"},
	}
	for _, tt := range tests {
		n := UpdateNotice{Tool: "yt-dlp", InstallMethod: tt.method}
		got := n.UpdateCommand()
		if got != tt.want {
			t.Errorf("method %q: got %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestNotifiedVersionSuppression(t *testing.T) {
	entry := UpdateCheckEntry{
		Tool:            "yt-dlp",
		LatestVersion:   "2026.03.01",
		CurrentVersion:  "2024.07.16",
		NotifiedVersion: "2026.03.01",
		CheckedAt:       time.Now(),
	}

	shouldNotify := entry.LatestVersion != entry.NotifiedVersion &&
		versionNewer(entry.LatestVersion, entry.CurrentVersion)
	if shouldNotify {
		t.Error("should not notify when NotifiedVersion matches LatestVersion")
	}
}
