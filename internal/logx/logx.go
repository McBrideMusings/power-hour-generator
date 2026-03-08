package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"powerhour/internal/paths"
)

const maxGlobalLogFiles = 50

// New creates a logger that writes to a timestamped file inside the project's
// logs directory. The returned closer should be closed when logging is no
// longer needed.
func New(p paths.ProjectPaths) (*log.Logger, io.Closer, error) {
	if err := os.MkdirAll(p.LogsDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("ensure logs directory: %w", err)
	}

	filename := time.Now().Format("20060102-150405") + ".log"
	filePath := filepath.Join(p.LogsDir, filename)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}

	logger := log.New(file, "", log.LstdFlags|log.Lmicroseconds)
	return logger, file, nil
}

// NewGlobal creates a logger that writes to ~/.powerhour/logs/<timestamp>.log.
// Use this for logging that happens outside a project context or for
// debugging CLI startup issues.
func NewGlobal(prefix string) (*log.Logger, io.Closer, error) {
	logsDir, err := paths.GlobalLogsDir()
	if err != nil {
		return nil, nil, err
	}

	filename := time.Now().Format("20060102-150405")
	if prefix != "" {
		filename = prefix + "-" + filename
	}
	filename += ".log"

	filePath := filepath.Join(logsDir, filename)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open global log file: %w", err)
	}

	logger := log.New(file, "", log.LstdFlags|log.Lmicroseconds)
	return logger, file, nil
}

// nopCloser is a no-op closer for when logger creation fails.
type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// StartCommand creates a global logger for a CLI command and returns a printf-style
// logging function along with a closer. The caller must defer closer.Close().
// On failure the returned logf is a no-op and closer is safe to call.
func StartCommand(prefix string) (logf func(string, ...any), closer io.Closer) {
	logsDir, _ := paths.GlobalLogsDir()
	if logsDir != "" {
		pruneGlobalLogs(logsDir, maxGlobalLogFiles)
	}

	glog, c, err := NewGlobal(prefix)
	if err != nil || c == nil {
		return func(string, ...any) {}, nopCloser{}
	}
	return func(format string, v ...any) { glog.Printf(format, v...) }, c
}

// pruneGlobalLogs removes the oldest log files when the count exceeds maxFiles.
// Errors are silently ignored — this is best-effort cleanup.
func pruneGlobalLogs(logsDir string, maxFiles int) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return
	}

	// Filter to only .log files
	var logs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".log" {
			logs = append(logs, e.Name())
		}
	}

	if len(logs) <= maxFiles {
		return
	}

	// Sort by name ascending (timestamps sort naturally)
	sort.Strings(logs)

	// Remove the oldest files beyond the cap
	toRemove := logs[:len(logs)-maxFiles]
	for _, name := range toRemove {
		os.Remove(filepath.Join(logsDir, name))
	}
}
