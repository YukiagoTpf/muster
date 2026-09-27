# Architecture

The design is deliberately small. Four mechanisms carry the whole tool.

## 1. Profile = whole-file snapshot

A profile file (`profiles/<name>.json`) **is** a complete agent `settings.json`. Switching is a verbatim whole-file overwrite. Consequences:

- AgentSwap never parses settings semantics — any present or future settings key works.
- No field merging, no managed-domain drift bugs.
- Shared config must be replicated per-profile (accepted trade-off; `agentswap save` and the web UI mitigate it).

Metadata that is NOT settings (notes, launchArgs, unsetEnv, and sync membership `remotes`) lives in `config.json`, never in the profile file.

## 2. Three-way reconcile

A single pure state machine (`src/reconcile.mjs`) over three content hashes:

- **live** — the agent's current `settings.json`
- **profile** — the active profile file
- **baseline** — `state.appliedHash`, what AgentSwap last wrote

Comparing only live vs profile can detect divergence but not *direction*. The baseline is the common ancestor that makes `live-unsaved` / `profile-edited` / `conflict` decidable. Every entry point (`use`, `save`, `current`, `doctor`, the web API) funnels through the same machine, so they can't disagree.

State is fail-closed: no baseline + live ≠ profile → `no-baseline`, refuse to overwrite until the user chooses (`--discard`).

## 3. Transactional mutation

- **Lock** — a single cross-process lock file (`<AGENTSWAP_HOME>/lock`, content `<pid>:<token>`). It is created atomically via `link(2)` from a fully-written tmp file (contenders never observe an empty lock), and stale-owner stealing re-reads the owner token so a thief only removes the SAME dead owner. Every mutating command holds it across the WHOLE read → reconcile → decide → write sequence (a concurrent edit can't land between the decision and the write). Reentrancy follows the async call chain (AsyncLocalStorage): a nested `withLock` in the same chain shares the hold, while two concurrent same-process transactions — e.g. simultaneous web requests — contend like separate processes.
- **Trusted state** — `state.json` carries a fingerprint of the agent settings path it was written for. Every reader goes through `readTrustedStateV2()`, which discards state whose fingerprint doesn't match the current config dir: after the agent's config directory changes, a stale `activeProfile` must not mark a profile active for the wrong live file or let a delete clear another dir's state.
- **Journal** — `journal.json` records an in-flight switch (from/to/hashes). A crash between "settings written" and "state committed" is recovered on the next invocation: if live already matches the target hash, the commit is finished; otherwise it's abandoned and reconcile re-derives truth.
- **Atomic writes** — tmp file + rename, 0600, with win32 EPERM retry. A symlinked target (a settings.json linked into a dotfiles repo) is written through: the rename lands on the file the link points at, never on the link.
- **Split backups** — live settings and each profile have independent rotated backup pools, so a busy profile can't evict settings recovery points.

## 4. Launch env ownership

`launchEnv` (shared by sticky and run modes) deletes the inherited auth/routing namespace (provider tokens, base URLs, and agent-specific auth keys) and then injects the profile's `env`. A stale token in the parent shell therefore cannot shadow the profile's provider. `run` mode additionally isolates via a private temp settings file, so N configs run in parallel without touching global state.

## Multi-machine sync

Machines running AgentSwap exchange profiles over SSH — no daemon, no server, no open port. Sync is not a fifth runtime: it is the four mechanisms above pointed at another machine. The remote only needs `agentswap` installed and key-based SSH. Direction is a three-way reconcile over **local / remote / last-synced baseline** (`sync.json`, machine-local, never itself synced), mirroring §2; a missing baseline is a conflict, never a silent overwrite. Remote writes are compare-and-swap (`expectedHash`) so a change between list and write cannot clobber. Deletions do not propagate: `rm` is local-only and a surviving copy re-adopts on the next sync.

Full design, membership, commands, and a worked example: [docs/sync.md](sync.md).

## CLI surface

The command surface is declared once in `src/cli-spec.mjs`. Human HELP and the `help --json` schema both generate from it, so they cannot drift. Adding a command means registering it there and in `src/reserved.mjs`.

## Web UI

`src/web/server.mjs` is a stdlib HTTP server bound to loopback. Static single-page app (`src/web/public/index.html` plus small pure helpers such as `env-project.js` and `row-collect.js`, no build step) + a small JSON API. Every API call — reads included — requires a per-session token, delivered via the `#fragment` of the startup URL (fragments never reach the server, so the token stays out of logs and out of reach of other local processes). The Host header must be loopback and mutations require a loopback Origin, closing DNS-rebinding and CSRF. The server is just another consumer of the same command library — the switch API literally calls `use()`, and all mutations run under the same `withLock` as the CLI. Local profile saves are compare-and-swap like remote ones: the GET returns the settings content hash, the PUT carries it back as `expectedHash` (checked under the lock), and a mismatch is a 409 the editor resolves with reload or overwrite — the same no-silent-overwrite rule the reconcile machinery enforces everywhere else. In the page itself, every navigation bumps a generation counter that async continuations re-check before committing (slow remote reads, probes, and save responses can't cross-target), and unsaved edits gate navigation and tab close. Choosing "discard" releases the draft rather than clearing it: it stays dirty until a view actually replaces it, so a navigation that fails hands it back still protected, and no draft can be saved while a navigation (a template reload included) is replacing it — the response would land on whatever replaces it.

The daily surface is an **env key/value projection** (`src/web/public/env-project.js`): it only rewrites string entries under `settings.env`, preserves unknown top-level keys and locked non-string env values (including `__proto__` / `constructor` via null-proto + `defineOwn`), and treats delete ≠ `""`. The previous structured tabs live under an explicit Advanced mode over the same `#f-json` — one canonical JSON, dirty guard, generation, and CAS unchanged. Env ↔ Advanced switches commit the leaving surface into that JSON before hydrating the other, so a stale Advanced hydration cannot overwrite a just-edited env table. Unedited launch metadata (`notes` / `launchArgs` / `unsetEnv`) is resolved with `resolveMeta` so an env-only save does not re-split a multi-word `launchArgs` entry. Platform templates (darwin / linux / win32) are edited as full settings snapshots via `GET/PUT /api/templates` and seed `POST /api/profiles { name, platform }`; template conflict is reload-only. Async saves advance the CAS hash but only clear the edit revision they sent; a newer draft stays dirty. Template reloads are navigations onto the same template: they only replace the displayed draft after a successful read and matching navigation/edit revisions.

The remotes API (`/api/remotes`, `/api/remotes/:r/profiles/:n`) is a third consumer of the sync protocol: it drives the same `__sync list/read/write` SSH agent the CLI uses, with no new remote-side surface. `POST`/`DELETE /api/remotes` mirror `remote add`/`remote rm` (probe-then-lock; membership and baselines stripped on removal). A remote edit is a compare-and-swap — the read's sync-unit hash goes back as `expectedHash`, so a concurrent remote edit is a 409 the UI resolves with reload or force-overwrite — and the profile's `remotes` membership round-trips untouched, since the write overwrites the whole meta. Remotes are probed on demand (startup, manual refresh, after a save), never on the local-state poll interval.

### The structured editor is a projection, not a parser

Mechanism 1 says AgentSwap never parses settings semantics, and [docs/v1.md](v1.md) lists settings semantics as deliberately not done. The editor's structured tabs do know key names — but only in the page, and only as a **lossless projection over the JSON**, which stays the canonical value. Nothing in `src/store.mjs`, `src/reconcile.mjs`, `src/sync.mjs` or the CLI learned a single settings key, so switching, hashing, reconciling and syncing are still whole-file operations on opaque content.

Four rules keep the projection honest, and they are the reason a form over settings.json is safe here at all:

1. **Own path only.** Every control is one descriptor (`SET_FIELDS`, `LIST_FIELDS`) naming one path. `collect*` writes that path and nothing else, so a key no descriptor mentions is never read, never written, and never dropped.
2. **Lock what you cannot express.** If the JSON holds a shape the control cannot represent — a string where a boolean belongs, a marketplace entry carrying `ref`, a hook handler of type `http` — the row locks, says *edit in JSON*, and `collect*` skips it. A locked control never rewrites its path.
3. **Absence is a value.** Booleans are tri-state (`unset | true | false`) because writing `false` where the user meant "leave the key out" changes what the agent does. `attribution.commit` goes further: `""` hides the trailer while absence gives the default one, so it carries an explicit *default / custom / blank* mode.
4. **Don't churn what nobody edited.** The unit is the control, not the tab: every control — down to a single row of a rule list or a single plugin key — remembers the value it loaded with, and a control still showing that value writes it back verbatim. No trimming, no re-spelling of an env boolean (`"1"` vs `"true"`), no dropping of an empty string or an empty array. Containers the editor created are pruned when they end up empty, so toggling a key on and back off restores the file exactly. Removing every row of a list or map the file had writes it back empty (`[]` / `{}`) — never the original, which would silently undo the deletion (the row rules live in `row-collect.js`). Hook cards remember which matcher group they came from, so two matcher-less groups on one event stay two groups instead of being folded into one — semantically identical, but a rewrite of a file nobody touched.

A shape the editor cannot read at all — a hooks tree with a non-array event, a group with no handlers — locks that whole editor rather than being rebuilt from a partial reading. The JSON tab is always the way out.

The hook library (`src/web/hook-library.mjs` data, `src/web/hook-store.mjs` storage, `GET/POST /api/hooks`, `DELETE /api/hooks/:id`) sits outside profiles on purpose: a profile is a settings.json snapshot, a library is a shelf you pick from. Saved presets live in `AGENTSWAP_HOME/hooks.json`; a corrupt shelf reads as empty rather than failing the page. A preset carries one command per platform (`darwin` / `linux` / `win32`) because a hook is portable in intent and not in syntax, and the platform picker defaults to this machine but does not bind to it — the machine you edit for is not always the machine you edit on. Inserting a preset produces an ordinary hook card; from that moment it is profile content like any other.

### Platform templates

Platform templates (`src/templates.mjs`, storage `AGENTSWAP_HOME/templates.json`, `GET /api/templates`, `PUT /api/templates/:platform`) provide baseline settings templates for `darwin`, `linux`, and `win32`.
When absent, default `{ env: {} }` templates are served.
Creating a profile with `agentswap add <name> [--platform <platform>]` or `POST /api/profiles` copies the chosen platform template into a new, independent profile snapshot.
Template edits only affect future creations and never alter existing profiles, preserving the whole-file snapshot model without introducing inheritance, layering, or field merging.
Template updates use optimistic concurrency control via whole-settings content hash compare-and-swap (`expectedHash`), ensuring concurrent saves to different platforms do not clobber each other.


## Platform isolation

All platform differences live in two files:

- `src/paths.mjs` — config dir resolution (XDG-style on POSIX, `%APPDATA%` on Windows, `AGENTSWAP_HOME` override)
- `src/launch.mjs` — spawn resolution for the agent, doctor's probe and `$EDITOR` alike (direct on POSIX, `cmd.exe` wrapper for `.cmd` shims such as npm bins or VS Code's `code` on Windows; of `where`'s results only launchable `.exe`/`.com`/`.cmd`/`.bat` count — npm's extensionless sh shim is listed first and cannot be spawned)

Everything else is platform-agnostic.
