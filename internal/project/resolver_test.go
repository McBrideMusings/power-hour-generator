package project

import (
	"testing"

	"powerhour/internal/config"
)

func TestResolveProjectPath(t *testing.T) {
	tests := []struct {
		name  string
		root  string
		value string
		want  string
	}{
		{"empty value", "/root", "", ""},
		{"whitespace value", "/root", "   ", ""},
		{"absolute path", "/root", "/abs/path/file.csv", "/abs/path/file.csv"},
		{"relative path", "/root", "plans/songs.csv", "/root/plans/songs.csv"},
		{"relative with dots", "/root", "../other/file.csv", "/other/file.csv"},
		{"absolute with redundant slashes", "/root", "/abs//path/./file.csv", "/abs/path/file.csv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveProjectPath(tt.root, tt.value)
			if got != tt.want {
				t.Errorf("resolveProjectPath(%q, %q) = %q, want %q", tt.root, tt.value, got, tt.want)
			}
		})
	}
}

func TestProfileExists(t *testing.T) {
	profiles := map[string]ResolvedProfile{
		"songs":  {Name: "songs"},
		"intros": {Name: "intros"},
	}

	tests := []struct {
		name        string
		profileName string
		want        bool
	}{
		{"existing profile", "songs", true},
		{"another existing", "intros", true},
		{"missing profile", "outros", false},
		{"empty name", "", false},
		{"whitespace trimmed", " songs ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := profileExists(profiles, tt.profileName)
			if got != tt.want {
				t.Errorf("profileExists(%q) = %v, want %v", tt.profileName, got, tt.want)
			}
		})
	}
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

func TestCloneProfile(t *testing.T) {
	orig := config.OverlayProfile{
		DefaultStyle: config.TextStyle{
			FontFile:  "font.ttf",
			FontSize:  intPtr(24),
			FontColor: "white",
		},
		Segments: []config.OverlaySegment{
			{Name: "title", Template: "{title}"},
		},
		DefaultDurationSec: intPtr(60),
		FadeInSec:          floatPtr(0.5),
		FadeOutSec:         floatPtr(1.0),
	}

	clone := cloneProfile("test", orig)

	if clone.Name != "test" {
		t.Errorf("clone.Name = %q, want %q", clone.Name, "test")
	}
	if *clone.DefaultStyle.FontSize != 24 {
		t.Errorf("clone font size = %d, want 24", *clone.DefaultStyle.FontSize)
	}

	// Mutating original should not affect clone
	*orig.DefaultStyle.FontSize = 48
	*orig.DefaultDurationSec = 30
	*orig.FadeInSec = 9.9
	orig.Segments[0].Name = "mutated"

	if *clone.DefaultStyle.FontSize != 24 {
		t.Error("clone font size mutated when original changed")
	}
	if *clone.DefaultDurationSec != 60 {
		t.Error("clone duration mutated when original changed")
	}
	if *clone.FadeInSec != 0.5 {
		t.Error("clone fade-in mutated when original changed")
	}
	if clone.Segments[0].Name != "title" {
		t.Error("clone segment name mutated when original changed")
	}
}

func TestCloneTextStyle(t *testing.T) {
	orig := config.TextStyle{
		FontSize:      intPtr(20),
		OutlineWidth:  intPtr(2),
		LineSpacing:   intPtr(5),
		LetterSpacing: intPtr(1),
	}

	clone := cloneTextStyle(orig)

	// Mutate all pointer fields in original
	*orig.FontSize = 99
	*orig.OutlineWidth = 99
	*orig.LineSpacing = 99
	*orig.LetterSpacing = 99

	if *clone.FontSize != 20 {
		t.Error("FontSize was not deep-cloned")
	}
	if *clone.OutlineWidth != 2 {
		t.Error("OutlineWidth was not deep-cloned")
	}
	if *clone.LineSpacing != 5 {
		t.Error("LineSpacing was not deep-cloned")
	}
	if *clone.LetterSpacing != 1 {
		t.Error("LetterSpacing was not deep-cloned")
	}
}

func TestCloneTextStyleNilPointers(t *testing.T) {
	orig := config.TextStyle{FontColor: "red"}
	clone := cloneTextStyle(orig)

	if clone.FontColor != "red" {
		t.Errorf("FontColor = %q, want %q", clone.FontColor, "red")
	}
	if clone.FontSize != nil {
		t.Error("expected nil FontSize")
	}
}

func TestCloneSegments(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		got := cloneSegments(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := cloneSegments([]config.OverlaySegment{})
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("deep clone", func(t *testing.T) {
		segs := []config.OverlaySegment{
			{Name: "a", Style: config.TextStyle{FontSize: intPtr(10)}},
			{Name: "b", Style: config.TextStyle{FontSize: intPtr(20)}},
		}

		cloned := cloneSegments(segs)

		if len(cloned) != 2 {
			t.Fatalf("len = %d, want 2", len(cloned))
		}

		// Mutate original
		*segs[0].Style.FontSize = 99
		segs[1].Name = "mutated"

		if *cloned[0].Style.FontSize != 10 {
			t.Error("segment style was not deep-cloned")
		}
		if cloned[1].Name != "b" {
			t.Error("segment name was shared (not cloned)")
		}
	})
}

func TestCopyIntPtr(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if copyIntPtr(nil) != nil {
			t.Error("expected nil")
		}
	})
	t.Run("value", func(t *testing.T) {
		orig := 42
		cp := copyIntPtr(&orig)
		orig = 99
		if *cp != 42 {
			t.Errorf("got %d, want 42", *cp)
		}
	})
}

func TestCopyFloatPtr(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if copyFloatPtr(nil) != nil {
			t.Error("expected nil")
		}
	})
	t.Run("value", func(t *testing.T) {
		orig := 3.14
		cp := copyFloatPtr(&orig)
		orig = 99.9
		if *cp != 3.14 {
			t.Errorf("got %f, want 3.14", *cp)
		}
	})
}
