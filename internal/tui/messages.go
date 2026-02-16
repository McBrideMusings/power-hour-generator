package tui

// RowUpdateMsg updates a single row's fields by column name.
type RowUpdateMsg struct {
	Key    string
	Fields map[string]string
}

// WorkDoneMsg signals that all background work has completed.
type WorkDoneMsg struct{}

// ErrorMsg signals a fatal error; the TUI should quit.
type ErrorMsg struct {
	Err error
}
