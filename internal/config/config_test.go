package config

import (
	"strings"
	"testing"
)

func TestLibrarySharedDefault(t *testing.T) {
	cfg := Config{}
	if !cfg.LibraryShared() {
		t.Fatal("expected LibraryShared() = true when mode is empty")
	}
}

func TestLibrarySharedExplicit(t *testing.T) {
	cfg := Config{Library: LibraryConfig{Mode: "shared"}}
	if !cfg.LibraryShared() {
		t.Fatal("expected LibraryShared() = true when mode is 'shared'")
	}
}

func TestLibraryLocal(t *testing.T) {
	cfg := Config{Library: LibraryConfig{Mode: "local"}}
	if cfg.LibraryShared() {
		t.Fatal("expected LibraryShared() = false when mode is 'local'")
	}
}

func TestLibraryPath(t *testing.T) {
	cfg := Config{Library: LibraryConfig{Path: "/custom/library"}}
	if cfg.LibraryPath() != "/custom/library" {
		t.Fatalf("expected LibraryPath() = /custom/library, got %q", cfg.LibraryPath())
	}
}

func TestLibraryPathEmpty(t *testing.T) {
	cfg := Config{}
	if cfg.LibraryPath() != "" {
		t.Fatalf("expected LibraryPath() = empty, got %q", cfg.LibraryPath())
	}
}

func TestValidateCollections_FileAndPlanMutuallyExclusive(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"opening": {File: "opening.mp4", Plan: "opening.csv"},
		},
	}
	err := cfg.ValidateCollections()
	if err == nil {
		t.Fatal("expected error when both file and plan are set")
	}
	if !strings.Contains(err.Error(), "cannot specify both file and plan") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCollections_NeitherFileNorPlan(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"opening": {},
		},
	}
	err := cfg.ValidateCollections()
	if err == nil {
		t.Fatal("expected error when neither file nor plan is set")
	}
	if !strings.Contains(err.Error(), "either file or plan is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCollections_FileOnly(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"opening": {File: "opening.mp4"},
		},
	}
	err := cfg.ValidateCollections()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCollections_FileSkipsHeaderValidation(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{
			"opening": {File: "opening.mp4", LinkHeader: "index"},
		},
	}
	err := cfg.ValidateCollections()
	if err != nil {
		t.Fatalf("file-based collection should skip header validation: %v", err)
	}
}
