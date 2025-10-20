package cli

import "testing"

func TestMatchTemplateBase(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		actual  string
		match   bool
	}{
		{"exact match", "abc", "abc", true},
		{"exact mismatch", "abc", "abcd", false},
		{"single placeholder", "%(id)s", "XYZ123", true},
		{"prefix placeholder", "clip_%(id)s", "clip_XYZ123", true},
		{"suffix placeholder", "%(id)s_clip", "XYZ123_clip", true},
		{"middle placeholder", "clip_%(id)s_end", "clip_123_end", true},
		{"missing middle content", "clip_%(id)s_end", "clip__end", false},
		{"multiple placeholders", "%(id)s_%(id)s", "A_B", true},
		{"insufficient content", "%(id)s%(id)s", "a", false},
		{"repeated text", "clip_%(id)s_mid_%(id)s", "clip_A_mid_B", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchTemplateBase(tc.pattern, tc.actual); got != tc.match {
				t.Fatalf("matchTemplateBase(%q, %q) = %v, want %v", tc.pattern, tc.actual, got, tc.match)
			}
		})
	}
}
