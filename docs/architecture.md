# Architecture

Muster is one Go binary. The plan and scope are in [rebrand/vision.md](rebrand/vision.md); the words used here are defined in [rebrand/CONTEXT.md](rebrand/CONTEXT.md).

## Layout

| Package | Owns |
| --- | --- |
| `cmd/muster` | The entry point; nothing but `cli.Main`. |
| `internal/cli` | The command table, argument parsing, human and `--json` output. Help and `help --json` generate from the table. |
| `internal/claude` | Everything Muster knows about Claude Code: its settings path, which env keys belong to the provider, the model-alias env keys. Another agent would get its own package. |
| `internal/credential` | The credential registry. |
| `internal/launch` | Turning a credential into a running instance, and the last-launch state. |
| `internal/quota` | Reading quota from every source, merging it into one account list, caching it, and the shared bar/percentage rendering. |
| `internal/picker` | The interactive chooser behind a bare `muster`. |
| `internal/fakeagent` | A stand-in agent for tests (the test binary re-executes itself as one). |
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

## Launch env ownership

`muster run <credential>` starts Claude Code in the foreground of the current terminal. Which pane it runs in is the user's business — Muster never drives a terminal multiplexer. A launch owns the provider env end to end:

1. **Strip** — the child's process env is the parent's minus every provider key, so a stale token exported in the shell can't shadow the credential.
2. **Inject** — the credential's whole provider env (base URL, key as `ANTHROPIC_AUTH_TOKEN`, carried env) goes into a private settings file passed as `--settings`. The `--settings` layer outranks the user settings key by key, which is what lets the credential override the default credential while every hook, permission and plugin of the shared settings still applies.
3. **Blank** — every provider key the default credential sets in `settings.json` that this credential does not set is put into the same file as `""`. Claude Code treats a blank value as unset, so, say, the default credential's `ANTHROPIC_DEFAULT_SONNET_MODEL=…[1M]` can't leak into an instance whose credential maps no sonnet. (Verified against Claude Code 2.1.296; see `docs/rebrand/vision.md` §7.)

The private settings file is `<MUSTER_HOME>/run/<pid>-<nonce>.json`, 0600, removed when the agent exits; it holds the key, so it never sits on a command line or in output. A file whose owning muster process is gone (crash, kill) is collected by the next launch; a live owner's file is kept however old it is. While the agent runs, Ctrl-C belongs to the agent; muster returns the agent's exit code (128 + signal on Unix when it was killed).

`--dry-run` prints the decided launch — the command with a placeholder for the settings file, and the names of the keys it sets, blanks and strips — and never a value. The agent binary is `claude` on `PATH`, or `MUSTER_CLAUDE_BIN`. On Windows an npm-installed `.cmd` shim runs through `cmd.exe /d /s /c` with one fully quoted command line.

Each launch records the credential, and the model per credential, in `state.json`; the picker offers them first.

## Default credential

The default credential is whatever provider env `~/.claude/settings.json` (or `$CLAUDE_CONFIG_DIR/settings.json`) sets: it is what plain `claude` and IDE plugins use, since they don't go through Muster. `muster default` reports which registered credential that is — `match`, `drift` (a credential's base URL and key, but other provider keys differ), `unregistered` or `none` — and `muster default <credential>` makes one the default.

Setting it is the one place Muster writes the agent's settings file, so it edits as little as it can: the provider keys in `env` are replaced by the credential's provider env, written where the first of them stood; every other key, top level or in `env`, keeps its value and its place (values are carried verbatim and the file is re-indented with two spaces). A file it cannot read as a JSON object is refused and left untouched. The previous file is backed up first (`<MUSTER_HOME>/backups/settings-<time>-<pid>.json`, newest 20 kept), the write is atomic, keeps the file's mode, and goes through a symlink to the file it points at. All of it happens under the store lock; the agent itself does not take that lock, so an edit it makes in the same instant can still lose — the backup is the way back.

## Quota

Quota belongs to a **subscription account** (5-hour, weekly, monthly … windows, each with a used fraction and a reset time); a credential has none of its own, only what it spent. `muster quota` reads every source this machine has, concurrently, each bounded by 10 s:

- **gateway credentials** (`provider: astergate`) — `GET <baseURL>/me/usage` with the credential's key. It is read-only and spends no quota. It answers with the subscription accounts in the key's pool and what the key spent today, this week and this month (cut in the `timezone` Muster sends: `TZ`, else the `Etc/GMT±N` zone of the local offset). Only account-wide windows count; a model-scoped window is not the account's limit.

Every source is isolated: one that fails — network, HTTP status, expired login — reports its error in its own place and the others are unaffected. `quota.Merge` folds the reports into one list in which an account reachable through several gateway keys appears once (the freshest observation wins; `via` lists every key). A window whose reset time has passed since it was observed shows as reset rather than with a stale percentage.

Every fetch is cached in `<MUSTER_HOME>/quota.json`, so views that must not wait on the network (the picker, `quota --cached`) show the last numbers with their age. `quota --json` is `{ "version": 1, "accounts": [...], "usage": { credential: {today, week, month} }, "errors": [ {source, error} ] }`.

Human output is colored only on a terminal and when `NO_COLOR` is unset; anywhere else the escapes are stripped.

## Picker

A bare `muster` in a terminal is the quickest launch: a list of credentials (provider, gateway host, mapped models), then that credential's model aliases plus the agent's own default; Enter launches, so a repeat of the last launch is two keypresses. Arrows or j/k move, a digit picks a row directly, Esc goes back, q quits. A credential that maps no alias skips the model screen. Without a terminal on both ends a bare `muster` prints help instead.

`picker.Screen` is pure — keys in, cursor and lines out — and `picker.Terminal` does the I/O: raw mode, hidden cursor, the frame redrawn in place with every line cut to the terminal width (a wrapped line would break the redraw), and the frame erased on exit so only the launch line remains. On a Windows console, virtual-terminal processing is switched on for the picker's lifetime.

## Agent skill

`skills/muster/SKILL.md` teaches an agent the agent-facing surface: `cred list --json`, `default --json` and `run --dry-run --json` to look, and `muster run` in a sibling pane to start another instance beside itself (the skill holds the herdr commands; Muster itself never drives a multiplexer). It forbids the agent from handling keys. Its frontmatter follows the open SKILL.md spec, which `go test ./internal/repocheck` checks.

## Platform isolation

All platform differences live in `internal/platform`: data and home directories, file locking, renames, process liveness, how a command is spawned and how its exit code reads. Everything else is platform-agnostic.
