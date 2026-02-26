package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateStrict_ProfileRefs(t *testing.T) {
	cfg := Config{
		Profiles: ProfilesConfig{
			"exists": {},
		},
		Collections: map[string]CollectionConfig{
			"good": {Plan: "x.csv", Profile: "exists"},
			"bad":  {Plan: "y.csv", Profile: "missing"},
		},
	}

	results := cfg.validateProfileRefs()
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateStrict_ProfileRefs_Valid(t *testing.T) {
	cfg := Config{
		Profiles: ProfilesConfig{
			"p1": {},
		},
		Collections: map[string]CollectionConfig{
			"c1": {Plan: "x.csv", Profile: "p1"},
		},
	}

	results := cfg.validateProfileRefs()
	if len(results) != 0 {
		t.Fatalf("expected no results, got %v", results)
	}
}

func TestValidateStrict_PlanPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "exists.csv"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Collections: map[string]CollectionConfig{
			"good": {Plan: "exists.csv"},
			"bad":  {Plan: "missing.csv"},
		},
	}

	results := cfg.validatePlanPaths(dir)
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateStrict_PlanPaths_AllExist(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.csv"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.csv"), []byte("b"), 0644)

	cfg := Config{
		Collections: map[string]CollectionConfig{
			"c1": {Plan: "a.csv"},
			"c2": {Plan: "b.csv"},
		},
	}

	results := cfg.validatePlanPaths(dir)
	if len(results) != 0 {
		t.Fatalf("expected no results, got %v", results)
	}
}

var testTokens = []string{"INDEX", "INDEX_PAD3", "SAFE_TITLE", "ARTIST"}

func TestValidateStrict_SegmentTemplate_ValidTokens(t *testing.T) {
	cfg := Config{
		Outputs: OutputConfig{
			SegmentTemplate: "$INDEX_PAD3_$SAFE_TITLE",
		},
	}

	results := cfg.validateSegmentTemplate(testTokens)
	if len(results) != 0 {
		t.Fatalf("expected no results for valid tokens, got %v", results)
	}
}

func TestValidateStrict_SegmentTemplate_UnknownToken(t *testing.T) {
	cfg := Config{
		Outputs: OutputConfig{
			SegmentTemplate: "$INDEX_PAD3_$BOGUS_TOKEN",
		},
	}

	results := cfg.validateSegmentTemplate(testTokens)
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for unknown token, got %d: %v", len(errs), errs)
	}
}

func TestValidateStrict_SegmentTemplate_MixedTokens(t *testing.T) {
	cfg := Config{
		Outputs: OutputConfig{
			SegmentTemplate: "$INDEX_$UNKNOWN1_$ARTIST_$UNKNOWN2",
		},
	}

	results := cfg.validateSegmentTemplate(testTokens)
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors for unknown tokens, got %d: %v", len(errs), errs)
	}
}

func TestValidateStrict_OrphanedProfiles(t *testing.T) {
	cfg := Config{
		Profiles: ProfilesConfig{
			"used":     {},
			"orphaned": {},
		},
		Collections: map[string]CollectionConfig{
			"c1": {Plan: "x.csv", Profile: "used"},
		},
	}

	results := cfg.validateOrphanedProfiles()
	var warnings []ValidationResult
	for _, r := range results {
		if r.Level == "warning" {
			warnings = append(warnings, r)
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
}

func TestValidateStrict_NoCollections(t *testing.T) {
	cfg := Config{
		Profiles: ProfilesConfig{},
	}

	results := cfg.ValidateStrict("/tmp", nil)
	if len(results) != 0 {
		t.Fatalf("expected no results for empty config, got %v", results)
	}
}

func TestValidateTimeline_Empty(t *testing.T) {
	cfg := Config{}
	results := cfg.validateTimeline()
	if len(results) != 0 {
		t.Fatalf("expected no results for empty timeline, got %v", results)
	}
}

func TestValidateTimeline_Valid(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"intro":         {Plan: "intro.csv"},
			"songs":         {Plan: "songs.csv"},
			"interstitials": {Plan: "interstitials.csv"},
			"outro":         {Plan: "outro.csv"},
		},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{Collection: "intro", Count: 1},
				{Collection: "songs", Interleave: &InterleaveConfig{Collection: "interstitials", Every: 1}},
				{Collection: "outro", Count: 1},
			},
		},
	}
	results := cfg.validateTimeline()
	if len(results) != 0 {
		t.Fatalf("expected no results for valid timeline, got %v", results)
	}
}

func TestValidateTimeline_MissingCollection(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{Collection: "nonexistent"},
			},
		},
	}
	results := cfg.validateTimeline()
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for missing collection, got %d: %v", len(errs), errs)
	}
}

func TestValidateTimeline_MissingInterleaveCollection(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"songs": {Plan: "songs.csv"},
		},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{Collection: "songs", Interleave: &InterleaveConfig{Collection: "nonexistent", Every: 1}},
			},
		},
	}
	results := cfg.validateTimeline()
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for missing interleave collection, got %d: %v", len(errs), errs)
	}
}

func TestValidateTimeline_EveryZero(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"songs":         {Plan: "songs.csv"},
			"interstitials": {Plan: "interstitials.csv"},
		},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{Collection: "songs", Interleave: &InterleaveConfig{Collection: "interstitials", Every: 0}},
			},
		},
	}
	results := cfg.validateTimeline()
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for every=0, got %d: %v", len(errs), errs)
	}
}

func TestValidateTimeline_EveryNegative(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"songs":         {Plan: "songs.csv"},
			"interstitials": {Plan: "interstitials.csv"},
		},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{Collection: "songs", Interleave: &InterleaveConfig{Collection: "interstitials", Every: -1}},
			},
		},
	}
	results := cfg.validateTimeline()
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for every=-1, got %d: %v", len(errs), errs)
	}
}

func TestValidateTimeline_NegativeCount(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"songs": {Plan: "songs.csv"},
		},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{Collection: "songs", Count: -1},
			},
		},
	}
	results := cfg.validateTimeline()
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for count=-1, got %d: %v", len(errs), errs)
	}
}

func TestValidateTimeline_EmptyCollectionName(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{},
		Timeline: TimelineConfig{
			Sequence: []SequenceEntry{
				{Collection: ""},
			},
		},
	}
	results := cfg.validateTimeline()
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for empty collection name, got %d: %v", len(errs), errs)
	}
}

func TestValidateExternalFiles_MissingProfileFile(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		ProfileFiles: []string{"missing.yaml"},
	}
	results := cfg.validateExternalFiles(dir)
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateExternalFiles_MissingCollectionFile(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		CollectionFiles: []string{"missing.yaml"},
	}
	results := cfg.validateExternalFiles(dir)
	var errs []ValidationResult
	for _, r := range results {
		if r.Level == "error" {
			errs = append(errs, r)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateExternalFiles_AllPresent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "profiles.yaml"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(dir, "colls.yaml"), []byte("{}"), 0644)

	cfg := Config{
		ProfileFiles:    []string{"profiles.yaml"},
		CollectionFiles: []string{"colls.yaml"},
	}
	results := cfg.validateExternalFiles(dir)
	if len(results) != 0 {
		t.Fatalf("expected no errors, got %v", results)
	}
}

func TestExtractTemplateTokens(t *testing.T) {
	tests := []struct {
		template string
		want     []string
	}{
		{"$INDEX_PAD3_$SAFE_TITLE", []string{"INDEX_PAD3", "SAFE_TITLE"}},
		{"no-tokens-here", nil},
		{"$$escaped", nil},
		{"$A_$B_$C", []string{"A", "B", "C"}},
		{"prefix_$TOKEN_suffix", []string{"TOKEN_suffix"}},
	}

	for _, tt := range tests {
		got := extractTemplateTokens(tt.template)
		if len(got) != len(tt.want) {
			t.Errorf("extractTemplateTokens(%q) = %v, want %v", tt.template, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("extractTemplateTokens(%q)[%d] = %q, want %q", tt.template, i, got[i], tt.want[i])
			}
		}
	}
}
