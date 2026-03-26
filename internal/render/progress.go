package render

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// progressWriter parses ffmpeg's -progress output and calls onProgress with
// a value between 0.0 and 1.0 based on out_time_us vs totalDurationUs.
type progressWriter struct {
	totalDurationUs int64
	onProgress      func(pct float64)
	buf             strings.Builder
}

func newProgressWriter(clipDurationSec float64, onProgress func(pct float64)) *progressWriter {
	return &progressWriter{
		totalDurationUs: int64(clipDurationSec * 1_000_000),
		onProgress:      onProgress,
	}
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.buf.Write(p)

	for {
		line, err := pw.readLine()
		if line == "" && err != nil {
			break
		}
		pw.parseLine(line)
	}
	return n, nil
}

func (pw *progressWriter) readLine() (string, error) {
	s := pw.buf.String()
	idx := strings.IndexByte(s, '\n')
	if idx < 0 {
		return "", io.EOF
	}
	line := s[:idx]
	pw.buf.Reset()
	pw.buf.WriteString(s[idx+1:])
	return strings.TrimSpace(line), nil
}

func (pw *progressWriter) parseLine(line string) {
	if pw.totalDurationUs <= 0 || pw.onProgress == nil {
		return
	}
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])

	if key != "out_time_us" {
		return
	}
	us, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return
	}
	pct := float64(us) / float64(pw.totalDurationUs)
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	pw.onProgress(pct)
}

// drainProgress reads remaining progress output after ffmpeg exits.
func drainProgress(pw *progressWriter, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		pw.parseLine(scanner.Text())
	}
}
