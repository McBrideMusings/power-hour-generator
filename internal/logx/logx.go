package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"powerhour/internal/paths"
)

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
