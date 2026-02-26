package config

import "testing"

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
