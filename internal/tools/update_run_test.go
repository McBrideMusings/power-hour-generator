package tools

import (
	"strings"
	"testing"
)

func TestUpdateArgvExternalUsesPackageManager(t *testing.T) {
	cases := []struct {
		method string
		want   string
	}{
		{InstallMethodHomebrew, "brew upgrade ffmpeg"},
		{InstallMethodApt, "sudo apt upgrade ffmpeg"},
		{InstallMethodSnap, "sudo snap refresh ffmpeg"},
		{InstallMethodPip, "pip install --upgrade ffmpeg"},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			got := strings.Join(UpdateArgv("ffmpeg", tc.method, "/tmp/project"), " ")
			if got != tc.want {
				t.Errorf("UpdateArgv = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateArgvManagedReentersInstall(t *testing.T) {
	argv := UpdateArgv("yt-dlp", InstallMethodManaged, "/tmp/project")

	if len(argv) < 5 {
		t.Fatalf("argv too short: %v", argv)
	}
	joined := strings.Join(argv[1:], " ")
	if !strings.HasPrefix(joined, "tools install yt-dlp --force") {
		t.Errorf("argv = %v, want a `tools install yt-dlp --force` invocation", argv)
	}
	if !strings.Contains(joined, "--project /tmp/project") {
		t.Errorf("argv = %v, want the project root passed through", argv)
	}
}

func TestUpdateArgvManagedOmitsEmptyProject(t *testing.T) {
	argv := UpdateArgv("yt-dlp", InstallMethodManaged, "")
	for _, a := range argv {
		if a == "--project" {
			t.Fatalf("argv should omit --project when no root is given: %v", argv)
		}
	}
}

func TestUpdateSupported(t *testing.T) {
	if !UpdateSupported("ffmpeg", InstallMethodHomebrew) {
		t.Error("homebrew-installed tools are always updatable")
	}
	if !UpdateSupported("yt-dlp", InstallMethodManaged) {
		t.Error("yt-dlp is powerhour-installable and should be updatable")
	}
	if UpdateSupported("vlc", InstallMethodSystem) {
		t.Error("a system-installed, non-installable tool has no update path")
	}
}
