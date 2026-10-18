# Contributing

Thanks for your interest in Muster. (Agents working on this repo: read [AGENTS.md](AGENTS.md) first.)

## Ground rules

- **No product mentions.** This repo stays neutral: don't name, compare with, or screenshot other config tools — in code, docs, commits, or issues.
- **One static binary; every dependency earns its place.** Prefer the Go standard library. A direct dependency must be listed under Dependencies below with the reason the standard library can't do the job; `go test ./...` fails on an unlisted one.
- Every mutating command goes through `Store.WithLock` and writes atomically (tmp + rename, 0600).
- Never break the `--json` contracts (`{ "version": 1, ... }`) silently — bump the version field if a shape changes.
- New commands register in the commands table in `internal/cli`. Help and `help --json` generate from it; a command missing from the table has no usage string or schema.
- Secrets stay masked by default. New output paths need an explicit opt-in for raw values; a credential's key never appears on a command line.

## Dependencies

| Module | Why the standard library is not enough |
| --- | --- |
| `golang.org/x/term` | Raw terminal mode and terminal size for the interactive picker; the standard library has neither. |
| `modernc.org/sqlite` | Reading the Cursor editor's login, which lives in a SQLite database the editor keeps open in WAL mode — recent writes sit in the `-wal` file, so only a real SQLite reads them correctly. Pure Go, no cgo; it adds about 6 MB to the binary. |
| `golang.org/x/sys` | Windows file locking (`LockFileEx`), process liveness (`OpenProcess`) and transient rename errors. The standard library's `syscall` package does not expose them on Windows. |
| `filippo.io/age` | Encrypting the credentials that go to the sync repo. The standard library has the primitives but no file format; age is an audited one with a standalone tool, so the user can decrypt their own backup without Muster. Passphrase (scrypt) and hybrid post-quantum keys come with it. |

## Development

```bash
go test ./...                                   # run everything, including the repo guardrails
go vet ./...
MUSTER_HOME=/tmp/m CLAUDE_CONFIG_DIR=/tmp/c go run ./cmd/muster cred list   # isolated manual testing
```

Tests live next to the code. End-to-end tests run the CLI against temp `MUSTER_HOME` and `CLAUDE_CONFIG_DIR` directories and stand in a fake agent binary (`MUSTER_CLAUDE_BIN`) — never touch a real config. Sync tests need `git` on `PATH`; they sync through a local bare repo, never the network.

When code changes, check [docs/index.md](docs/index.md) for the owning docs — the routing table there is machine-checked by `go test ./internal/repocheck`. The redline check reads `.redlines.local.json` (gitignored) if present; the term list never enters the repo.

## Commit style

Small, atomic commits: one logical change per commit, conventional subject (`fix: …`, `feat: …`, `docs: …`).

## Cross-platform changes

Platform differences (data and config paths, the platform's name, process liveness, file locking, process spawning, the shell, console mode) are confined to `internal/platform` on purpose; everything else is platform-agnostic. `go test ./...` must pass on Linux, macOS and Windows. The tree is LF everywhere (`.gitattributes`), and a test whose premise only holds on some platforms (POSIX file modes, creating symlinks) skips elsewhere with the reason — it never fails there.
