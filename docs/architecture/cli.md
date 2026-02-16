# CLI Layer

The CLI is built with [Cobra](https://github.com/spf13/cobra) and lives in `internal/cli/`.

## Entry Point

`cmd/powerhour/main.go` calls `internal/cli.Execute()`, which sets up the root command and all subcommands.

## Command Files

Each file in `internal/cli/` corresponds to a command or command group:

| File | Command |
|------|---------|
| `init.go` | `powerhour init` |
| `fetch.go` | `powerhour fetch` |
| `render.go` | `powerhour render` |
| `validate.go` | `powerhour validate filenames/segments` |
| `collections_fetch.go` | Collection-aware fetch variant |
| `collections_render.go` | Collection-aware render variant |
| `validate_collection.go` | Collection-aware validation |

## Global Flags

| Flag | Description |
|------|-------------|
| `--project <dir>` | Project directory path |
| `--json` | Machine-readable output |
| `--index <n\|n-m>` | Filter to specific plan rows (repeatable) |
| `--collection <name>` | Target a specific collection |

## Command Routing

When collections are configured in the YAML, commands automatically route to collection-aware handlers (`collections_fetch.go`, `collections_render.go`). The routing is transparent to the user — the same CLI flags work regardless of whether the project uses collections or legacy clips.
