package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveExternalPath_Relative(t *testing.T) {
	got := resolveExternalPath("/project", "profiles/songs.yaml")
	want := filepath.Join("/project", "profiles/songs.yaml")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveExternalPath_Absolute(t *testing.T) {
	got := resolveExternalPath("/project", "/abs/path.yaml")
	if got != "/abs/path.yaml" {
		t.Fatalf("got %q, want /abs/path.yaml", got)
	}
}

// --- Collection file tests ---

func TestLoadCollectionFiles_Single(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "colls.yaml"), `
extras:
  plan: extras.yaml
  output_dir: extras
`)

	cfg := Config{
		CollectionFiles: []string{"colls.yaml"},
		Collections:     map[string]CollectionConfig{},
	}
	if err := cfg.loadCollectionFiles(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Collections["extras"]; !ok {
		t.Fatal("expected extras collection to be loaded")
	}
}

func TestLoadCollectionFiles_Multiple(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), `
coll-a:
  plan: a.csv
`)
	writeFile(t, filepath.Join(dir, "b.yaml"), `
coll-b:
  plan: b.csv
`)

	cfg := Config{
		CollectionFiles: []string{"a.yaml", "b.yaml"},
		Collections:     map[string]CollectionConfig{},
	}
	if err := cfg.loadCollectionFiles(dir); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Collections) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(cfg.Collections))
	}
}

func TestLoadCollectionFiles_DuplicateInlineVsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "colls.yaml"), `
songs:
  plan: songs.csv
`)

	cfg := Config{
		CollectionFiles: []string{"colls.yaml"},
		Collections: map[string]CollectionConfig{
			"songs": {Plan: "songs.yaml"},
		},
	}
	err := cfg.loadCollectionFiles(dir)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(err.Error(), "songs") || !strings.Contains(err.Error(), "inline config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadCollectionFiles_DuplicateAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), `
shared:
  plan: a.csv
`)
	writeFile(t, filepath.Join(dir, "b.yaml"), `
shared:
  plan: b.csv
`)

	cfg := Config{
		CollectionFiles: []string{"a.yaml", "b.yaml"},
		Collections:     map[string]CollectionConfig{},
	}
	err := cfg.loadCollectionFiles(dir)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestLoadCollectionFiles_MissingFile(t *testing.T) {
	cfg := Config{
		CollectionFiles: []string{"nonexistent.yaml"},
		Collections:     map[string]CollectionConfig{},
	}
	err := cfg.loadCollectionFiles(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadCollectionFiles_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "empty.yaml"), "")

	cfg := Config{
		CollectionFiles: []string{"empty.yaml"},
		Collections: map[string]CollectionConfig{
			"existing": {Plan: "x.csv"},
		},
	}
	if err := cfg.loadCollectionFiles(dir); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Collections) != 1 {
		t.Fatalf("expected 1 collection unchanged, got %d", len(cfg.Collections))
	}
}

func TestLoadCollectionFiles_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.yaml"), "{{{{not yaml")

	cfg := Config{
		CollectionFiles: []string{"bad.yaml"},
		Collections:     map[string]CollectionConfig{},
	}
	err := cfg.loadCollectionFiles(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadCollectionFiles_NoFiles(t *testing.T) {
	cfg := Config{
		Collections: map[string]CollectionConfig{"existing": {Plan: "x.csv"}},
	}
	if err := cfg.loadCollectionFiles(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Collections) != 1 {
		t.Fatal("collections should be unchanged with no collection_files")
	}
}

// --- Integration test: Load() with external files ---

func TestLoad_WithExternalFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "collections", "extras.yaml"), `
extras:
  plan: extras.csv
  output_dir: extras
`)

	writeFile(t, filepath.Join(dir, "powerhour.yaml"), `
version: 1
collection_files:
  - collections/extras.yaml
collections:
  songs:
    plan: songs.csv
    output_dir: songs
`)

	cfg, err := Load(filepath.Join(dir, "powerhour.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := cfg.Collections["extras"]; !ok {
		t.Error("expected extras collection from external file")
	}
	if _, ok := cfg.Collections["songs"]; !ok {
		t.Error("expected inline songs collection")
	}
}

func TestLoad_ApplyDefaultsAfterMerge(t *testing.T) {
	dir := t.TempDir()

	// External collection without header defaults — ApplyDefaults should fill them in.
	writeFile(t, filepath.Join(dir, "colls.yaml"), `
ext-coll:
  plan: plan.csv
`)
	writeFile(t, filepath.Join(dir, "powerhour.yaml"), `
version: 1
collection_files:
  - colls.yaml
collections: {}
`)

	cfg, err := Load(filepath.Join(dir, "powerhour.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	coll, ok := cfg.Collections["ext-coll"]
	if !ok {
		t.Fatal("expected ext-coll")
	}
	if coll.LinkHeader != "link" {
		t.Errorf("expected default link_header 'link', got %q", coll.LinkHeader)
	}
	if coll.StartHeader != "start_time" {
		t.Errorf("expected default start_header 'start_time', got %q", coll.StartHeader)
	}
	if coll.DurationHeader != "duration" {
		t.Errorf("expected default duration_header 'duration', got %q", coll.DurationHeader)
	}
}

func TestLoad_ExistingConfigsUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "powerhour.yaml"), `
version: 1
collections:
  songs:
    plan: songs.csv
    output_dir: songs
`)

	cfg, err := Load(filepath.Join(dir, "powerhour.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Collections["songs"]; !ok {
		t.Fatal("expected songs collection")
	}
	if len(cfg.CollectionFiles) != 0 {
		t.Error("expected no collection_files")
	}
}

// --- Helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
