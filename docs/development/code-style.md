# Code Style

## Formatting

All Go source files must be formatted with `gofmt`:

```bash
gofmt -w $(find cmd internal pkg -name '*.go')
```

Run this before every commit. Non-canonical formatting will cause CI failures.

## Static Analysis

```bash
go vet ./...
```

## Package Conventions

- **`internal/`** — private packages, not importable by external projects
- **`pkg/`** — public packages (currently just `csvplan`)
- **`cmd/`** — binary entry points

## Naming

- Standard Go naming conventions (camelCase for unexported, PascalCase for exported)
- File names match command names in `internal/cli/` (e.g., `fetch.go` for the fetch command)
- Collection-aware variants use `collections_` prefix (e.g., `collections_fetch.go`)

## Error Handling

- Return errors, don't panic
- Wrap errors with context using `fmt.Errorf("context: %w", err)`
- Aggregate validation errors where possible (CSV loader reports all row errors, not just the first)

## Config Design

- YAML tags on all config struct fields
- Built-in defaults for every field
- Profiles under `profiles.overlays` are the single source of truth for overlay configuration
- Collections and legacy `clips.song` are mutually exclusive

## Testing

- Table-driven tests with descriptive names
- Temp directories for file-based tests
- Mock runners for external tool dependencies
- See [Testing](/development/testing) for details
