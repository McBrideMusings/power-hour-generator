package csvplan

import (
	"strconv"
	"strings"
)

// ValidationError captures a single field-level validation problem.
type ValidationError struct {
	Line    int
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	if e.Field != "" {
		return strings.TrimSpace(
			strings.Join([]string{
				formatLine(e.Line),
				e.Field,
				e.Message,
			}, " "),
		)
	}
	return strings.TrimSpace(formatLine(e.Line) + " " + e.Message)
}

// ValidationErrors aggregates multiple validation issues.
type ValidationErrors []ValidationError

func (errs ValidationErrors) Error() string {
	if len(errs) == 0 {
		return "validation failed"
	}
	messages := make([]string, len(errs))
	for i, err := range errs {
		messages[i] = err.Error()
	}
	return strings.Join(messages, "; ")
}

// Issues returns a copy of the underlying validation errors.
func (errs ValidationErrors) Issues() []ValidationError {
	return append([]ValidationError(nil), errs...)
}

func formatLine(line int) string {
	if line <= 0 {
		return "row"
	}
	return "row " + strconv.Itoa(line)
}
