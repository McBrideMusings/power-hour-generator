# Testing

## Running Tests

```bash
# Run all tests
go test ./...

# Run a single package
go test ./internal/render/...
go test ./pkg/csvplan/...

# Verbose output
go test -v ./internal/render/...

# Run a specific test by name
go test -run TestFilterGraph ./internal/render/...
```

### Sandboxed Environments

In environments where the default Go build cache is not writable, point `GOCACHE` at a temporary directory:

```bash
GOCACHE=$(mktemp -d) go test ./...
```

## Testing Patterns

The project uses Go's standard `testing` package with consistent patterns:

### Table-Driven Tests

Tests are organized as tables of test cases with descriptive names:

```go
tests := []struct {
    name     string
    input    string
    expected string
}{
    {"empty input", "", ""},
    {"basic case", "hello", "HELLO"},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

### Temp Directories

File-based tests use temporary directories:

```go
dir := t.TempDir()
// write test files to dir
// run code under test
// assert results
```

### Mock Command Runners

Cache tests use a `runner.go` abstraction to mock external commands (`yt-dlp`, `ffprobe`) without requiring the actual tools.

### Shared Test Helpers

The render package has `test_helpers_test.go` with shared utilities for building test fixtures.

### Testable Service Construction

The `newCacheService` variable in `fetch.go` is typed as the `cache.NewService` signature, allowing test injection. The status-aware variant `newCacheServiceWithStatus` adds a `StatusFunc` callback for TUI integration.

## Test Coverage by Package

| Package | Coverage | Notes |
|---------|----------|-------|
| `pkg/csvplan` | Good | Loader and validation tests |
| `internal/render` | Good | Filter graph construction, templates |
| `internal/cache` | Good | Uses mock runners |
| `internal/config` | Partial | Config parsing and defaults |
| `internal/tui` | Good | Progress model, marquee, tick animation |
| `internal/project` | Needs work | Collections resolver needs coverage |

## Known Failures

See [Troubleshooting — Known Test Failures](/development/troubleshooting#known-test-failures) for pre-existing test failures in `status_test.go`.
