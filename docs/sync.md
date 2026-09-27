# Multi-machine sync

Machines running AgentSwap exchange **profiles** over SSH. There is no daemon, no cloud, no account, and no listening port. The local CLI `ssh`s to a host that already has `agentswap` installed and is reachable with **key-based** auth; the remote only has to answer that one command. Password prompts are forbidden (`BatchMode=yes`) — a hung passphrase would stall a sync and eat the JSON piped to a write.

The same four local mechanisms still apply on each side: a profile is a whole-file snapshot, direction is a three-way hash reconcile, mutations run under `withLock`, and AgentSwap never parses settings semantics. Sync is not a fifth runtime — it is those mechanisms pointed at another machine.

## Topology

```
this machine                          remote machine
<AGENTSWAP_HOME>                      <AGENTSWAP_HOME>
  profiles/*.json          SSH          profiles/*.json
  config.json (meta)   ───────────►   agentswap __sync …
  sync.json (local)
```

`agentswap remote add` records `{ host, bin }` in **this** machine's `config.json`. `host` is an SSH alias (or `user@host`); `bin` defaults to `agentswap` and can be a full path if it is not on the remote PATH. The registry is machine-local and is never itself synced — laptop and desktop each name the other's SSH target independently.

The remote handler is hidden: `agentswap __sync list|read|write`. It always prints a `{ "version": 2, ... }` envelope on stdout so the transport can parse success and application errors the same way.

## What moves, what stays

**Synced** (the payload of a push/pull):

- the profile file — the complete settings snapshot
- per-profile meta in `config.json`: `notes`, `launchArgs`, `unsetEnv`, and `remotes` (membership)

**Not synced** (each machine's own provenance and recovery):

- `state.json` — which profile is sticky-active *here*, and the live/profile baseline
- `sync.json` — last-synced content hashes per remote (this is the sync baseline; mirroring it would collapse "who moved")
- `config.json` top-level `remotes` — SSH targets
- backups, journal, lock, `run/` temp files

A profile's `remotes` array is membership ("this profile is kept in sync with vps and desktop"), not the SSH registry. The two live in the same `config.json` on purpose: membership rides with the profile, targets do not.

## Three-way reconcile

`src/sync.mjs` is the same idea as [the live/profile/baseline machine](architecture.md#2-three-way-reconcile), pointed at a different triple:

| Hash | Meaning |
| --- | --- |
| **local** | sync-unit hash of the profile + its meta on this machine |
| **remote** | sync-unit hash the other side just reported (`__sync list` / `read`) |
| **baseline** | `sync.json` — the hash as of the last successful sync with **that** remote |

The sync unit is the profile file **plus its synced meta** (`notes`, `launchArgs`, `unsetEnv`, `remotes`): the hash must cover everything a remote write overwrites, or a meta-only change (a note, a membership edit) would be invisible to the reconcile and the CAS. `remotes` is set semantics (sorted, deduped) before hashing.

Comparing only local vs remote detects divergence but not *direction*. The per-(remote, profile) baseline is the common ancestor that makes push vs pull decidable.

| State | When | Action |
| --- | --- | --- |
| `absent` | neither side has the profile | skip |
| `in-sync` | local == remote | record a baseline if this pair never had one; an equal pair always advances the baseline to the current hash (equal content is the new common ancestor) |
| `push` | local exists, remote missing; or remote still equals the baseline and local moved | send local → remote |
| `pull` | remote exists, local missing; or local still equals the baseline and remote moved | take remote → local |
| `conflict` | both moved, **or** both present, different, and there is no baseline | refuse until `--force` / `--force-push` / `--force-pull` |

Fail-closed: a missing baseline is a conflict, never a silent overwrite. `sync.json` is fail-closed the same way `state.json` is — absent, corrupt, or the wrong version reads as "no baselines".

Deletions **do not propagate**. `agentswap rm` is local-only. A profile missing on one side re-adopts from the other: local-only → push, remote-only → pull. Dropping a profile on the laptop and then `sync`ing will pull it back if the remote still has it. That is the point — `rm` is not a distributed delete. Names never merge across case either: a remote `Work` is not pulled beside a local `work`, and a remote refuses a pushed `Work` beside its own `work` — even with `--force`, which overrides conflicts, not collisions. Both report `name-collision`, because the two would be one file on macOS/Windows. For the same reason as deletions, the web UI refuses to rename a synced profile (one with membership or a recorded baseline): a rename is a local delete plus a create, so the next sync would pull the old name back beside the new one.

## Compare-and-swap

A push first `list`s the remote, decides "push", then `write`s. Between those two round-trips the remote can move. The write therefore carries `expectedHash`:

- key present, value a hash — remote content must still equal that hash
- key present, value `null` — the profile must still be absent (create)
- key **absent** — force; overwrite regardless

Mismatch → `{ error: "conflict" }`, no write. `--force` on push/pull (and `--force-push` / `--force-pull` on sync) omits the key.

The remote write runs under the remote's own `withLock`. Local membership and `sync.json` update under the local lock **after** the remote accepts the write, so a CAS rejection cannot leave a lying baseline.

## Membership and multiple remotes

Each profile has a `remotes` array (multi-select). `push` / `pull` with explicit remote names override membership for that invocation; a successful push also **adopts** the named remote into membership so a later bare `sync` covers it. A multi-target push (`push work vps desktop`) adopts **all** its targets before the first write, so every target receives the same unit — membership is part of the hash, and adopting one target per write would leave each remote holding a different one.

`agentswap sync` with no args walks every registered remote. For each remote the scope is: local members of that remote ∪ profiles the remote listed ∪ profiles that already have a baseline — so a profile born on the remote is discovered, and a local-only member is pushed.

All remote lists are fetched **before** any write. A profile present on two or more remotes with different hashes is a conflict on **all** of them: sync must not converge the remotes through this machine (pull A then push B is propagation, not sync). Resolve by pulling from one remote explicitly. The one exception is a difference no remote made: when the profile exists locally and every copy still equals the baseline this machine recorded for that remote, nothing would be pulled — the copies differ only by this machine's own earlier pushes (e.g. a remote added to membership later), so each remote simply catches up.

Pulling the same profile from **several** remotes in one command requires they agree on content — the same rule, checked up front. Silently adopting A then B would record baselines that a later sync pushes back onto A — propagation by accident. On disagreement, name one remote: `agentswap pull work vps`.

## Commands

```
agentswap remote add <name> --host <ssh-host> [--bin <path>]
agentswap remote ls
agentswap remote rm <name>            # drops registry + membership + sync.json subtree

agentswap push <name> [remote...] [--force]
agentswap pull <name> [remote...] [--force]
agentswap sync [remote...] [--force-push] [--force-pull]
```

`remote add` probes with an actual `__sync list` call over SSH — a capability handshake, not just a version string, so an older agentswap without the protocol fails at registration instead of at the first sync. `remote rm` does not delete profiles on the other machine.

`--json` envelopes are `{ "version": 2, ... }` like every other command. Only `pushed` / `pulled` / `in-sync` count as success: conflicts, missing profiles, and unreadable profiles exit non-zero.

## Worked example

Laptop and a VPS, key-based SSH already working (`ssh vps` does not prompt):

```bash
# on the laptop — register the VPS and send a profile
agentswap remote add vps --host vps
agentswap push work vps

# on the VPS — edit that profile, save
agentswap use work
# …change settings…
agentswap save

# back on the laptop — the VPS moved, we didn't → pull
agentswap sync
```

If both sides edited `work` since the last sync, `sync` reports `conflict` and leaves both copies alone. `agentswap sync --force-pull` takes the VPS; `--force-push` takes the laptop. Same choice as `--discard` / `--overwrite-profile` on a local switch: the tool will not pick a side.
