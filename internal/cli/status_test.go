package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusCommandTableOutput(t *testing.T) {
	prevProject := projectDir
	prevJSON := outputJSON
	defer func() {
		projectDir = prevProject
		outputJSON = prevJSON
	}()

	projectDir = t.TempDir()
	outputJSON = false

	csvPath := filepath.Join(projectDir, "powerhour.csv")
	data := "title,artist,start_time,duration,name,link\n" +
		"Track,Artist,0:30,60,,https://example.com\n"
	if err := os.WriteFile(csvPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	cmd := newStatusCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command returned error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Project: "+projectDir) {
		t.Fatalf("expected project path in output, got %q", got)
	}
	if !strings.Contains(got, "INDEX") || !strings.Contains(got, "TITLE") {
		t.Fatalf("expected table headers in output, got %q", got)
	}
	if !strings.Contains(got, "Track") {
		t.Fatalf("expected row data in output, got %q", got)
	}

	if stderr.Len() != 0 {
		t.Fatalf("expected no validation errors, got %q", stderr.String())
	}
}

func TestStatusCommandJSONOutput(t *testing.T) {
	prevProject := projectDir
	prevJSON := outputJSON
	defer func() {
		projectDir = prevProject
		outputJSON = prevJSON
	}()

	projectDir = t.TempDir()
	outputJSON = true

	csvPath := filepath.Join(projectDir, "powerhour.csv")
	data := "title,artist,start_time,duration,name,link\n" +
		"Track,Artist,0:30,60,,https://example.com\n"
	if err := os.WriteFile(csvPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	cmd := newStatusCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command returned error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "\"rows\"") {
		t.Fatalf("expected JSON output, got %q", got)
	}
	if !strings.Contains(got, "Track") {
		t.Fatalf("expected track name in JSON output, got %q", got)
	}
}
