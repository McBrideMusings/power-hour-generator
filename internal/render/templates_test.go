package render

import (
	"testing"
	"time"

	"powerhour/internal/cache"
	"powerhour/pkg/csvplan"
)

func TestSegmentBaseNameWithTemplate(t *testing.T) {
	seg := Segment{
		Row: csvplan.Row{
			Index:           28,
			Title:           "Chic, C'est La Vie",
			Artist:          "Countess Luann",
			Name:            "Madison",
			DurationSeconds: 60,
			Start:           39 * time.Second,
		},
		CachedPath: "/tmp/cache/4rgzBdOpDt8.webm",
		Entry: cache.Entry{
			Key:    "0J3vgcE5i2o",
			Source: "https://www.youtube.com/watch?v=4rgzBdOpDt8",
		},
	}

	base := SegmentBaseName("$ID_$INDEX_$TITLE_$NAME", seg)
	want := "0J3vgcE5i2o_028_Chic_C_est_La_Vie_Madison"
	if base != want {
		t.Fatalf("segmentBaseName mismatch: got %q want %q", base, want)
	}
}

func TestSegmentBaseNameFallback(t *testing.T) {
	seg := Segment{
		Row: csvplan.Row{
			Index:           5,
			Title:           "Fellow Feeling",
			DurationSeconds: 60,
		},
	}

	base := SegmentBaseName("", seg)
	if base == "" {
		t.Fatalf("expected fallback base name, got empty string")
	}
	if base != "005_fellow-feeling" {
		t.Fatalf("unexpected fallback base: %q", base)
	}
}
