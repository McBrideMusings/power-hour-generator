package tools

import "testing"

func TestFilterRemediation(t *testing.T) {
	tests := []struct {
		name    string
		missing []string
		method  string
		wantLen int
		wantSub string
	}{
		{"homebrew", []string{"drawtext"}, InstallMethodHomebrew, 1, "brew reinstall"},
		{"apt", []string{"drawtext"}, InstallMethodApt, 1, "libavfilter-extra"},
		{"snap", []string{"drawtext"}, InstallMethodSnap, 1, "snap refresh"},
		{"managed", []string{"drawtext"}, InstallMethodManaged, 2, "ffmpeg.org"},
		{"empty missing", nil, InstallMethodHomebrew, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterRemediation(tt.missing, tt.method)
			if len(got) != tt.wantLen {
				t.Errorf("got %d suggestions, want %d: %v", len(got), tt.wantLen, got)
				return
			}
			if tt.wantSub != "" {
				found := false
				for _, s := range got {
					if contains(s, tt.wantSub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected suggestion containing %q, got %v", tt.wantSub, got)
				}
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
