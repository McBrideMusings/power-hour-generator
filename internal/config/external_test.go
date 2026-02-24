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

func TestLoadProfileFiles_Single(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profiles.yaml"), `
custom-profile:
  segments:
    - name: title
      template: "{title}"
`)

	cfg := Config{
		ProfileFiles: []string{"profiles.yaml"},
		Profiles:     ProfilesConfig{},
	}
	if err := cfg.loadProfileFiles(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Profiles["custom-profile"]; !ok {
		t.Fatal("expected custom-profile to be loaded")
	}
}

func TestLoadProfileFiles_Multiple(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), `
profile-a:
  segments: []
`)
	writeFile(t, filepath.Join(dir, "b.yaml"), `
profile-b:
  segments: []
`)

	cfg := Config{
		ProfileFiles: []string{"a.yaml", "b.yaml"},
		Profiles:     ProfilesConfig{},
	}
	if err := cfg.loadProfileFiles(dir); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(cfg.Profiles))
	}
}

func TestLoadProfileFiles_DuplicateInlineVsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profiles.yaml"), `
song-main:
  segments: []
`)

	cfg := Config{
		ProfileFiles: []string{"profiles.yaml"},
		Profiles: ProfilesConfig{
			"song-main": {},
		},
	}
	err := cfg.loadProfileFiles(dir)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(err.Error(), "song-main") || !strings.Contains(err.Error(), "inline config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadProfileFiles_DuplicateAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), `
shared:
  segments: []
`)
	writeFile(t, filepath.Join(dir, "b.yaml"), `
shared:
  segments: []
`)

	cfg := Config{
		ProfileFiles: []string{"a.yaml", "b.yaml"},
		Profiles:     ProfilesConfig{},
	}
	err := cfg.loadProfileFiles(dir)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadProfileFiles_MissingFile(t *testing.T) {
	cfg := Config{
		ProfileFiles: []string{"nonexistent.yaml"},
		Profiles:     ProfilesConfig{},
	}
	err := cfg.loadProfileFiles(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadProfileFiles_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "empty.yaml"), "")

	cfg := Config{
		ProfileFiles: []string{"empty.yaml"},
		Profiles: ProfilesConfig{
			"existing": {},
		},
	}
	if err := cfg.loadProfileFiles(dir); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 1 {
		t.Fatalf("expected 1 profile unchanged, got %d", len(cfg.Profiles))
	}
}

func TestLoadProfileFiles_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.yaml"), "{{{{not yaml")

	cfg := Config{
		ProfileFiles: []string{"bad.yaml"},
		Profiles:     ProfilesConfig{},
	}
	err := cfg.loadProfileFiles(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadProfileFiles_NilProfiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "p.yaml"), `
new-profile:
  segments: []
`)

	cfg := Config{
		ProfileFiles: []string{"p.yaml"},
	}
	if err := cfg.loadProfileFiles(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Profiles["new-profile"]; !ok {
		t.Fatal("expected new-profile to be created from nil map")
	}
}

func TestLoadProfileFiles_NoFiles(t *testing.T) {
	cfg := Config{
		Profiles: ProfilesConfig{"existing": {}},
	}
	if err := cfg.loadProfileFiles(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 1 {
		t.Fatal("profiles should be unchanged with no profile_files")
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

	writeFile(t, filepath.Join(dir, "profiles", "songs.yaml"), `
ext-profile:
  segments:
    - name: title
      template: "{title}"
`)
	writeFile(t, filepath.Join(dir, "collections", "extras.yaml"), `
extras:
  plan: extras.csv
  output_dir: extras
`)

	writeFile(t, filepath.Join(dir, "powerhour.yaml"), `
version: 1
profile_files:
  - profiles/songs.yaml
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

	if _, ok := cfg.Profiles["ext-profile"]; !ok {
		t.Error("expected ext-profile from external file")
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
	if len(cfg.ProfileFiles) != 0 {
		t.Error("expected no profile_files")
	}
	if len(cfg.CollectionFiles) != 0 {
		t.Error("expected no collection_files")
	}
}

func TestLoad_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	absDir := t.TempDir()

	writeFile(t, filepath.Join(absDir, "profiles.yaml"), `
abs-profile:
  segments: []
`)

	writeFile(t, filepath.Join(dir, "powerhour.yaml"), `
version: 1
profile_files:
  - `+filepath.Join(absDir, "profiles.yaml")+`
collections: {}
`)

	cfg, err := Load(filepath.Join(dir, "powerhour.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Profiles["abs-profile"]; !ok {
		t.Error("expected abs-profile from absolute path")
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
