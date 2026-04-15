package dashboard

import "testing"

func TestTruncateCollectionValuePrefersFileName(t *testing.T) {
	value := "/Users/pierce/Projects/power-hour-generator/powerhour-1/videos/ghibli-visuals.webm"
	got := truncateCollectionValue(value, 32)

	if got != ".../videos/ghibli-visuals.webm" {
		t.Fatalf("truncateCollectionValue() = %q", got)
	}
}

func TestTruncateCollectionValueFallsBackForURLs(t *testing.T) {
	value := "https://example.com/really/long/path/to/a/video/file"
	got := truncateCollectionValue(value, 20)

	if got != "https://example.c..." {
		t.Fatalf("truncateCollectionValue() = %q", got)
	}
}
