package tui

import (
	"io"
	"os"
	"runtime"
	"strings"
)

// OutputMode describes how progress output should be rendered.
type OutputMode int

const (
	// ModeTUI uses bubbletea for interactive progress rendering.
	ModeTUI OutputMode = iota
	// ModePlain writes a static table after all work completes.
	ModePlain
	// ModeJSON writes structured JSON output.
	ModeJSON
)

// DetectMode determines the appropriate output mode for the given writer.
func DetectMode(out io.Writer, noProgress, jsonOutput bool) OutputMode {
	if jsonOutput {
		return ModeJSON
	}
	if noProgress {
		return ModePlain
	}
	file, ok := out.(*os.File)
	if !ok {
		return ModePlain
	}
	info, err := file.Stat()
	if err != nil {
		return ModePlain
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return ModePlain
	}
	if runtime.GOOS != "windows" {
		term := os.Getenv("TERM")
		if term == "" || strings.EqualFold(term, "dumb") {
			return ModePlain
		}
	}
	return ModeTUI
}
