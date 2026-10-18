# Architecture

Muster is one Go binary. The plan and scope are in [rebrand/vision.md](rebrand/vision.md); the words used here are defined in [rebrand/CONTEXT.md](rebrand/CONTEXT.md).

## Layout

| Package | Owns |
| --- | --- |
| `cmd/muster` | The entry point; nothing but `cli.Main`. |
| `internal/cli` | The command table, argument parsing, human and `--json` output. Help and `help --json` generate from the table. |
| `internal/claude` | Everything Muster knows about Claude Code: its settings path, which env keys belong to the provider, the model-alias env keys. Another agent would get its own package. |
| `internal/credential` | The credential registry. |
| `internal/platform` | Every OS difference. Nothing outside it branches on the operating system. |
| `internal/store` | The data directory, its lock, and atomic JSON writes. |
| `internal/repocheck` | Repo guardrails, as tests. |

## Data directory

`MUSTER_HOME`, or `%APPDATA%\muster` on Windows and `$XDG_CONFIG_HOME/muster` (default `~/.config/muster`) elsewhere; created 0700.

## Transactional mutation

- **Lock** — one lock file (`<MUSTER_HOME>/lock`) under an OS lock: `flock` on Unix, `LockFileEx` on Windows. Every mutating command holds it across the whole read → decide → write sequence, so a concurrent edit can't land between the decision and the write. The kernel drops the lock when the holder exits — killed or crashed included — so there is no stale-owner detection, no stealing and no fencing. Two handles in one process contend like two processes. `WithLock` is not reentrant: a command takes it once, at the top. A contender waits up to 10 s, then fails naming the lock file.
- **Atomic writes** — bytes go to a tmp file beside the target, are fsynced, then renamed into place (0600), so a reader sees the old content or the new, never a torn one. On Windows a rename that a scanner or indexer transiently denies is retried. A symlinked target (a file linked into a dotfiles repo) is written through: the rename lands on the file the link points at, never on the link.

## Credentials

A credential owns exactly the **provider env** — the part of Claude Code's env that says where requests go and how they authenticate: the whole `ANTHROPIC_` namespace plus the auth-ish `CLAUDE_CODE_` keys (OAuth token, API keys, cloud-provider switches), matched on whole words so `CLAUDE_CODE_MAX_OUTPUT_TOKENS` stays a shared setting. Everything else in the agent's settings is shared settings and never belongs to a credential.

The registry is two files in the data directory:

- `credentials.json` — `{ "version": 1, "credentials": [ { name, provider, baseURL, env } ] }`. `env` is the rest of the provider env the credential carries: model mapping (`ANTHROPIC_DEFAULT_<ALIAS>_MODEL`), display names, headers. It never holds a secret: a key that looks like one (`…_TOKEN`, `…_KEY`, …) is refused, because this file is not encrypted.
- `secrets.json` (0600) — `{ "version": 1, "secrets": { name: key } }`. The key is the only secret a credential has; a launch sends it as `ANTHROPIC_AUTH_TOKEN`.

Names are matched exactly but kept unique ignoring case. Secrets are written before the registry, so a crash between the two leaves at worst an orphaned key, never a credential whose key was dropped. A key never travels on a command line: `cred add` takes it from stdin or imports it from `settings.json` (`--from-settings` imports the base URL, the key and every non-secret provider key, and leaves shared settings behind).

## Platform isolation

All platform differences live in `internal/platform`: data and home directories, file locking, renames, process liveness. Everything else is platform-agnostic.
