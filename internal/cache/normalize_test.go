package cache

import "testing"

func assertReasons(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Reasons = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Reasons = %#v, want %#v", got, want)
		}
	}
}

func TestNormalizeMetadataSplitsArtistTitle(t *testing.T) {
	cfg := NormalizationConfig{}
	in := NormalizationInput{
		Title: "Tame Impala - The Less I Know The Better (Official Video)",
	}

	res := NormalizeMetadata(cfg, in)

	if res.Artist != "Tame Impala" {
		t.Errorf("Artist = %q, want %q", res.Artist, "Tame Impala")
	}
	if res.Title != "The Less I Know The Better" {
		t.Errorf("Title = %q, want %q", res.Title, "The Less I Know The Better")
	}
	if res.Confidence != "high" {
		t.Errorf("Confidence = %q, want %q", res.Confidence, "high")
	}
	assertReasons(t, res.Reasons, []string{"split artist/title from title field"})
}

func TestNormalizeMetadataStripsVideoSuffixAndFallsBackToUploader(t *testing.T) {
	cfg := NormalizationConfig{}
	in := NormalizationInput{
		Title:    "Bohemian Rhapsody (Official Video)",
		Uploader: "Queen",
	}

	res := NormalizeMetadata(cfg, in)

	if res.Title != "Bohemian Rhapsody" {
		t.Errorf("Title = %q, want %q", res.Title, "Bohemian Rhapsody")
	}
	if res.Artist != "Queen" {
		t.Errorf("Artist = %q, want %q", res.Artist, "Queen")
	}
	if res.Confidence != "medium" {
		t.Errorf("Confidence = %q, want %q", res.Confidence, "medium")
	}
	assertReasons(t, res.Reasons, []string{"removed video suffix noise", "fell back to uploader"})
}

func TestNormalizeMetadataFallsBackToChannel(t *testing.T) {
	cfg := NormalizationConfig{}
	in := NormalizationInput{
		Title:   "Creep [HD]",
		Channel: "Radiohead",
	}

	res := NormalizeMetadata(cfg, in)

	if res.Title != "Creep" {
		t.Errorf("Title = %q, want %q", res.Title, "Creep")
	}
	if res.Artist != "Radiohead" {
		t.Errorf("Artist = %q, want %q", res.Artist, "Radiohead")
	}
	if res.Confidence != "medium" {
		t.Errorf("Confidence = %q, want %q", res.Confidence, "medium")
	}
	assertReasons(t, res.Reasons, []string{"removed video suffix noise", "fell back to channel"})
}

func TestNormalizeMetadataHandleLikeUploaderYieldsLowConfidenceCandidate(t *testing.T) {
	cfg := NormalizationConfig{}
	in := NormalizationInput{
		Title:    "Karma Police",
		Uploader: "RadioheadVEVO",
	}

	res := NormalizeMetadata(cfg, in)

	if res.Artist != "" {
		t.Errorf("Artist = %q, want empty", res.Artist)
	}
	if res.AliasCandidate != "RadioheadVEVO" {
		t.Errorf("AliasCandidate = %q, want %q", res.AliasCandidate, "RadioheadVEVO")
	}
	if res.Confidence != "low" {
		t.Errorf("Confidence = %q, want %q", res.Confidence, "low")
	}
	assertReasons(t, res.Reasons, nil)
}
