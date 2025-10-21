package cli

import (
	"io"
	"os"
	"runtime"
	"strings"
)

func nonEmptyOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func detectInteractiveProgress(out io.Writer, disabled bool) bool {
	if disabled {
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if runtime.GOOS != "windows" {
		term := os.Getenv("TERM")
		if term == "" || strings.EqualFold(term, "dumb") {
			return false
		}
	}
	return true
}
