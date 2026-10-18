# Architecture

Muster is one Go binary. The plan and scope are in [rebrand/vision.md](rebrand/vision.md); the words used here are defined in [rebrand/CONTEXT.md](rebrand/CONTEXT.md).

## Layout

| Package | Owns |
| --- | --- |
| `cmd/muster` | The entry point; nothing but `cli.Main`. |
| `internal/cli` | The command table, argument parsing, human and `--json` output. Help and `help --json` generate from the table. Logic lives in the packages below, so every surface runs the same code. |
| `internal/claude` | Everything Muster knows about Claude Code: its settings path, which env keys belong to the provider, the model-alias env keys. Another agent would get its own package. |
| `internal/credential` | The credential registry, how a credential is shown, and the default credential (which one `settings.json` holds, and making one the default). |
| `internal/launch` | Turning a credential into a running instance, and the last-launch state. |
| `internal/quota` | Reading quota from every source, merging it into one account list, caching it, and the shared bar/percentage rendering. |
| `internal/picker` | The interactive chooser behind a bare `muster`. |
| `internal/fakeagent` | A stand-in agent for tests (the test binary re-executes itself as one). |
| `internal/platform` | Every OS difference. Nothing outside it branches on the operating system. |
| `internal/syncrepo` | Multi-machine sync: the sync repo's layout and encryption, joining it, and the export → merge → apply → push cycle. |
| `internal/setup` | The setup list, what this machine installed from it, and running its commands. |
| `internal/store` | The data directory, its locks, backups, and atomic writes. |
| `internal/repocheck` | Repo guardrails, as tests. |

## Data directory

`MUSTER_HOME`, or `%APPDATA%\muster` on Windows and `$XDG_CONFIG_HOME/muster` (default `~/.config/muster`) elsewhere; created 0700.

## Transactional mutation

- **Lock** — one lock file (`<MUSTER_HOME>/lock`; a sync holds a second one around it, see [Sync](#sync)) under an OS lock: `flock` on Unix, `LockFileEx` on Windows. Every mutating command holds it across the whole read → decide → write sequence, so a concurrent edit can't land between the decision and the write. The kernel drops the lock when the holder exits — killed or crashed included — so there is no stale-owner detection, no stealing and no fencing. Two handles in one process contend like two processes. `WithLock` is not reentrant: a command takes it once, at the top. A contender waits up to 10 s, then fails naming the lock file.
- **Atomic writes** — bytes go to a tmp file beside the target, are fsynced, then renamed into place (0600), so a reader sees the old content or the new, never a torn one. On Windows a rename that a scanner or indexer transiently denies is retried. A symlinked target (a file linked into a dotfiles repo) is written through: the rename lands on the file the link points at, never on the link.

## Credentials

A credential owns exactly the **provider env** — the part of Claude Code's env that says where requests go and how they authenticate: the whole `ANTHROPIC_` namespace plus the auth-ish `CLAUDE_CODE_` keys (OAuth token, API keys, cloud-provider switches), matched on whole words so `CLAUDE_CODE_MAX_OUTPUT_TOKENS` stays a shared setting. Everything else in the agent's settings is shared settings and never belongs to a credential.

The registry is two files in the data directory:

- `credentials.json` — `{ "version": 1, "credentials": [ { name, provider, baseURL, env } ] }`. `env` is the rest of the provider env the credential carries: model mapping (`ANTHROPIC_DEFAULT_<ALIAS>_MODEL`), display names, headers. It never holds a secret: a key that looks like one (`…_TOKEN`, `…_KEY`, …) is refused, because this file is not encrypted.
- `secrets.json` (0600) — `{ "version": 1, "secrets": { name: key } }`. The key is the only secret a credential has; a launch sends it as `ANTHROPIC_AUTH_TOKEN`.

Names are matched exactly but kept unique ignoring case. Secrets are written before the registry, so a crash between the two leaves at worst an orphaned key, never a credential whose key was dropped. `cred edit` changes a credential in place (its name stays; `--key-stdin` replaces the key, otherwise the key is kept). A key never travels on a command line: `cred add` takes it from stdin or imports it from `settings.json` (`--from-settings` imports the base URL, the key and every non-secret provider key, and leaves shared settings behind).

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

- **local logins** of other clients, each a source only on a machine where that client is logged in, read-only, with the access token used as is and **never refreshed** — refreshing would rotate the client's refresh token and log the client out. A token past its expiry reports so, and the client renews it the next time it runs:
  - **Codex** — the Codex CLI's ChatGPT login (`$CODEX_HOME/auth.json`, default `~/.codex/auth.json`) → `GET https://chatgpt.com/backend-api/wham/usage`: plan, the rate-limit windows (named by length, e.g. a 604800 s window is `week`), and the credit balance.
  - **Cursor** — the editor's login, the `cursorAuth/accessToken` row of `<app config dir>/Cursor/User/globalStorage/state.vscdb`. The editor keeps that SQLite database open in WAL mode, so recent writes may sit only in the `-wal` file; Muster reads it through SQLite itself, opened `mode=ro`, and never changes the main file. Then `POST https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage` and `GetPlanInfo` (Connect protocol, body `{}`): the billing cycle and its included usage. The used fraction is `totalSpend / limit` (cents); the API's own percentage fields don't agree with those two.
  - **Grok** — the Grok CLI's login (`~/.grok/auth.json`) → `GET https://cli-chat-proxy.grok.com/v1/billing?format=credits` (without `format=credits` the endpoint answers in an older monthly shape whose numbers are all zero): the credit usage of the current period and per product. Its access token lives about six hours and only the CLI renews it, so a machine that hasn't run grok lately shows the login as expired.

Every source is isolated: one that fails — network, HTTP status, expired login — reports its error in its own place and the others are unaffected. `quota.Merge` folds the reports into one list in which an account reachable through several gateway keys appears once (the freshest observation wins; `via` lists every key). A window whose reset time has passed since it was observed shows as reset rather than with a stale percentage.

Every fetch is cached in `<MUSTER_HOME>/quota.json`, so views that must not wait on the network (the picker, `quota --cached`) show the last numbers with their age. When the picker finds the cache older than a minute (or missing) it starts a detached `muster __refresh-quota` — plumbing, not listed in help — and goes on with what it has; the next view is fresh. A detached process runs in its own session on Unix and with no console in its own process group on Windows, so it outlives the picker and a Ctrl-C in the terminal can't reach it. `quota --json` is `{ "version": 1, "accounts": [...], "usage": { credential: {today, week, month} }, "errors": [ {source, error} ], "fetchedAt" }` (`fetchedAt`: when the oldest of the numbers was fetched).

Human output is colored only on a terminal and when `NO_COLOR` is unset; anywhere else the escapes are stripped.

## Statusline

`muster statusline` prints the quota line of a Claude Code statusline: the plan of the account the session is pinned to, each of its windows as a bar with its reset time, and what the credential spent today; `switched → <plan>` leads the line for ten minutes after the gateway moves the session to another account. `--lang zh` switches the labels to Chinese. A statusline script calls it with the statusline JSON on stdin (it reads `session_id`) and prints its own first line itself.

A render must take milliseconds — Claude Code cancels a slow statusline — so it never touches the network. The session's credential is the registered gateway credential whose base URL and key match the provider env the session carries (a launch or the default credential put it there); a session on anything else prints nothing. Each session has its own state, `<MUSTER_HOME>/statusline/<session>.json`: the render reads it and, once it is due, marks the next refresh a minute out and starts a detached `muster __refresh-statusline <session>`, which asks the gateway with `X-Claude-Code-Session-Id` so the answer marks the pinned account. A changed account id between two refreshes is a switch. Until the gateway pins the session, the line says so and the state is refreshed every 10 s. A failed refresh keeps the last known account and says why (`HTTP 401`, network). States not refreshed for a week are dropped.

## Picker

A bare `muster` in a terminal is the quickest launch: a list of credentials — each gateway credential with the windows of the account in its pool that has the most headroom (the one a new session can count on), then provider, gateway host and mapped models — then that credential's model aliases plus the agent's own default; Enter launches, so a repeat of the last launch is two keypresses. Arrows or j/k move, a digit picks a row directly, Esc goes back, q quits. A credential that maps no alias skips the model screen. Without a terminal on both ends a bare `muster` prints help instead.

`picker.Screen` is pure — keys in, cursor and lines out — and `picker.Terminal` does the I/O: raw mode, hidden cursor, the frame redrawn in place with every line cut to the terminal width (a wrapped line would break the redraw), and the frame erased on exit so only the launch line remains. On a Windows console, virtual-terminal processing is switched on for the picker's lifetime.

## Sync

`muster sync` keeps the user's machines in step through a private git repo of their own (on GitHub, say), separate from Muster's. Muster drives the user's `git`, so the remote authenticates however their git already does; git never prompts (`GIT_TERMINAL_PROMPT=0`), and every call overrides the config that would change the outcome: no line-ending conversion, no commit signing, a fixed committer.

**What syncs.** The repo's layout:

```
muster.json                {"version": 1}: a Muster sync repo
identity.age               the repo's age identity, encrypted with the user's passphrase
credentials/<name>.age     each credential with its key, encrypted to the identity
<platform>/settings.json   the platform's shared settings
<platform>/setup.json      the platform's setup list
<platform>/files/<path>    the platform's tracked files, by their path under the home directory
```

`<platform>` is `windows`, `macos` or `linux`: machines on one platform share its settings, setup list and files; credentials are shared by all. The shared settings are `settings.json` minus the provider env, so the default credential never leaves the machine; a settings file whose remaining env has a secret-looking key (`…_TOKEN`, `…_API_KEY`) is refused rather than sent. Tracked files are files the user wrote and named (`muster track <path>`), under the home directory; `settings.json` and Muster's own data can't be tracked. Nothing else syncs — not what installers put down, not logins, not session history.

**Secrets.** Credentials are age files, readable with the age tool alone. Each is encrypted to the repo's identity — a hybrid post-quantum age key made with the repo — and the identity is encrypted with the passphrase (scrypt). A machine asks for the passphrase once, when it joins, and keeps the identity in `<MUSTER_HOME>/sync.json` (0600), as it keeps keys in `secrets.json`. An unchanged credential keeps its ciphertext, so encryption's randomness never shows up as a change.

**Joining** — `muster sync init <repo>` clones the repo into `<MUSTER_HOME>/sync`. An empty repo becomes a sync repo seeded with this machine (the passphrase is new and asked twice). A repo other machines use restores this one: what the repo has wins — `settings.json` keeps its own provider env, and every file overwritten is backed up first — and what only this machine has is added (its other credentials, the shared settings of a platform new to the repo). A sync repo is its `main` branch; a remote with branches but no `main`, or without `muster.json`, is refused. A join that fails leaves the machine as it was, with no clone.

**Syncing** — the clone's HEAD is what this machine last synced, so every sync is a three-way merge, and git does the merging:

1. **Export** — the machine's state is written into the clone and committed: shared settings, credentials (a removed one is deleted), tracked files. A tracked file missing here is restored by the apply, not deleted; only `muster untrack` stops tracking a file.
2. **Merge** — fetch, then merge `origin/main`. Edits to different parts of the shared settings merge by hunk; different credentials are different files. When both sides changed the same thing, the merge is undone, nothing is applied, and the conflicts are reported (exit 1) until `--prefer local` or `--prefer remote` picks a side: per hunk where git can, per file where it can't (changed on one side, deleted on the other).
3. **Apply** — the merge goes onto the machine: `settings.json` gets the merged shared settings with this machine's provider env put back where it stood (a file whose shared part didn't change is left alone, formatting and all), the registry gets the merged credentials, the home directory the tracked files. Every file overwritten is backed up (`backups/<name>-<time>-<pid><ext>`, newest 20 per name).
4. **Push** — if another machine pushed in the meantime, the sync merges again (three rounds at most). A push that fails leaves the merge applied and committed; the next sync pushes it.

A sync spends seconds on the network, so it holds a lock of its own (`sync.lock`) throughout, and the store lock only while it reads and writes the machine. The apply re-reads the machine under the store lock and starts over if anything changed since the export; the agent takes no lock, so an edit it makes in that same instant can still lose, and the backup is the way back. A sync killed in the middle of a merge leaves it in the clone; the next sync undoes it before anything else.

Syncing is manual. `muster sync status` works out offline what the next sync sends and whether a push is still owed. JSON: `sync` and `sync init` print `{ "version": 1, "sent": [...], "received": [...], "conflicts": [...] }` with items `{kind: settings|setup|credential|file, name}`; `sync status` prints `{ "version": 1, "repo", "platform", "pending": [{kind, name, change: added|modified|removed}], "unpushed", "tracked": ["~/…"] }`; `track` and `untrack` print `{ "version": 1, "tracked": [...] }`.

### Setup list

What a machine installs beyond Claude Code itself — a set of skills, a framework, a plugin marketplace — is the platform's setup list, `<platform>/setup.json` in the sync repo: entries of a name, an install command, and optionally an update and a check command. Muster runs the commands and never parses what they install; plugins need no entry, since `enabledPlugins` and `extraKnownMarketplaces` in the shared settings make Claude Code install them. The list is edited in the clone (`setup add`, `setup rm`, under the sync lock) and travels with the next sync, which reports a changed list and names the entries this machine hasn't installed.

`muster setup install` installs what this machine lacks: an entry whose check exits 0 is present already, as is one without a check that this machine installed with the same install command (`<MUSTER_HOME>/setup-state.json` records each install; a changed command is a new install). `muster update` runs the update command of every entry installed here. A command line runs as the user wrote it, in the platform's shell — `sh -c` on Unix, `cmd.exe /d /s /c` on Windows — with the terminal's stdin and output (under `--json` the output goes to stderr, and stdout carries `{ "version": 1, "results": [{name, action: installed|present|updated|failed|skipped, exitCode, reason}] }`). A failed command fails the run (exit 1) but not the entries after it.

## Agent skill

`skills/muster/SKILL.md` teaches an agent the agent-facing surface: `cred list --json`, `default --json` and `run --dry-run --json` to look, and `muster run` in a sibling pane to start another instance beside itself (the skill holds the herdr commands; Muster itself never drives a multiplexer), and `sync status --json` and `sync --json` to sync, with the choice of side in a conflict left to the user. It forbids the agent from handling keys and the sync passphrase. Its frontmatter follows the open SKILL.md spec, which `go test ./internal/repocheck` checks.

## Platform isolation

All platform differences live in `internal/platform`: data, home and application-config directories, the platform's name (which part of the sync repo a machine uses), the shell a user's command line runs in, file locking, renames, process liveness, how a command is spawned (in the foreground or detached) and how its exit code reads. Everything else is platform-agnostic.
