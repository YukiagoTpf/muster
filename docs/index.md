# Documentation index

Routing table for documentation upkeep. When code changes, find the owning docs from the changed paths first.

`go test ./internal/repocheck` machine-checks this table: every tracked path must match a route, and every doc named in the second column must exist. An empty owner (`—`) means the path needs no doc.

## Code path → doc owners

| Changed path | Docs to check |
| --- | --- |
| `AGENTS.md`, `CLAUDE.md` | `docs/index.md` |
| `cmd/**`, `internal/cli/**` | `README.md`, `docs/architecture.md` |
| `internal/platform/**` | `docs/architecture.md`, `CONTRIBUTING.md` |
| `internal/**` | `docs/architecture.md` |
| `internal/repocheck/**`, `go.mod`, `go.sum` | `CONTRIBUTING.md`, `docs/index.md` |
| `skills/**` | `README.md`, `docs/architecture.md` |
| `web/**` | `docs/architecture.md`, `CONTRIBUTING.md` |
| `docs/**` | `docs/index.md` |
| `README.md`, `CONTRIBUTING.md`, `CONTEXT.md` | `docs/index.md` |
| `install.sh`, `install.ps1` | `README.md`, `CONTRIBUTING.md` |
| `.gitignore`, `.gitattributes`, `LICENSE` | — |
| `.github/**` | — |

If a changed path fits no row, add a row here while updating the nearest doc. Keep the table small and route-oriented; detailed behavior belongs in the owning doc.
