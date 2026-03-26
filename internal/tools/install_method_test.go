package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectInstallMethod_Managed(t *testing.T) {
	root, err := cacheRoot()
	if err != nil {
		t.Skip("cannot resolve cache root")
	}
	dir := filepath.Join(root, "test-tool", "1.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Join(root, "test-tool"))

	bin := filepath.Join(dir, "test-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := detectInstallMethod(bin); got != InstallMethodManaged {
		t.Errorf("expected managed, got %s", got)
	}
}

func TestDetectInstallMethod_HomebrewPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/opt/homebrew/Cellar/ffmpeg/7.1/bin/ffmpeg", InstallMethodHomebrew},
		{"/usr/local/Cellar/ffmpeg/7.1/bin/ffmpeg", InstallMethodHomebrew},
		{"/opt/homebrew/bin/ffmpeg", InstallMethodHomebrew},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isHomebrew(tt.path); !got {
				t.Errorf("expected isHomebrew=true for %s", tt.path)
			}
		})
	}
}

func TestDetectInstallMethod_Snap(t *testing.T) {
	if got := detectInstallMethod("/snap/bin/ffmpeg"); got != InstallMethodSnap {
		if got == InstallMethodSystem {
			t.Skip("snap detection only active on linux")
		}
		t.Errorf("expected snap, got %s", got)
	}
}

func TestDetectInstallMethod_Pip(t *testing.T) {
	tests := []string{
		"/home/user/.local/lib/python3.11/site-packages/yt_dlp/__main__.py",
		"/home/user/.local/pipx/venvs/yt-dlp/bin/yt-dlp",
	}
	for _, path := range tests {
		if !isPipManaged(path) {
			t.Errorf("expected isPipManaged=true for %s", path)
		}
	}
}

func TestDetectInstallMethod_SystemFallback(t *testing.T) {
	got := detectInstallMethod("/usr/bin/ffmpeg")
	if got != InstallMethodSystem && got != InstallMethodApt {
		t.Errorf("expected system or apt, got %s", got)
	}
}
