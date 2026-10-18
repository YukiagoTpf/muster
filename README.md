# Muster

**Every AI subscription at a glance. Launch agents side by side.**

Muster keeps your coding-agent credentials in one place, shows how much quota every subscription account has left, and launches Claude Code with the credential and model you pick — one instance per terminal pane, each with its own provider, none touching the others.

> **Status: in development.** Muster is a Go rewrite of AgentSwap. The released Node tool is still on npm as [`agentswap@0.4.0`](https://www.npmjs.com/package/agentswap) (source: tag `v0.4.0`). The plan is [docs/rebrand/vision.md](docs/rebrand/vision.md).

## Build from source

Requires Go ≥ 1.25.

```bash
go build -o muster ./cmd/muster
./muster help
```

## Usage

```bash
# register a credential — import the gateway you already use from ~/.claude/settings.json …
muster cred add claude-pool --provider astergate --from-settings
# … or describe one, with the key on stdin (it never goes on the command line)
muster cred add codex-pool --provider astergate --base-url https://gw.example.com \
  --model opus=gpt-x --model haiku=gpt-x-mini --key-stdin < key.txt

muster cred list            # add --json for agents; keys are never shown
muster cred rm codex-pool

# launch Claude Code in this pane: pick a credential, then a model — the last launch is preselected
muster
# … or say it outright (agents and scripts do)
muster run claude-pool -m opus
muster run codex-pool -- --resume      # args after -- go to Claude Code
muster run codex-pool --dry-run        # what would be set, blanked and stripped — key names only
```

```bash
muster default              # which credential plain `claude` and IDE plugins use
muster default codex-pool   # make it codex-pool (rewrites only the provider part of settings.json, backs it up first)
```

A launch overrides only the provider part of Claude Code's settings — base URL, key, model mapping — and leaves your hooks, permissions and plugins alone. Whatever your `~/.claude/settings.json` sets for its own provider (the *default credential*, used when you run `claude` directly or from an IDE) is blanked for the launch, so nothing of it leaks into an instance that uses another credential. Split your terminal however you like and run one `muster run` per pane.

Agents get the same surface: `skills/muster/` is a skill that teaches one to list credentials, check a launch with `--dry-run --json`, and open another instance in a side pane.

Every command that reports something takes `--json` and prints `{ "version": 1, ... }`; `muster help --json` describes them all.

## Documentation

- [docs/rebrand/vision.md](docs/rebrand/vision.md) — what Muster is and what the first version covers
- [docs/rebrand/CONTEXT.md](docs/rebrand/CONTEXT.md) — the vocabulary
- [docs/architecture.md](docs/architecture.md) — the design
- [docs/adr/](docs/adr/) — decisions and why
- [docs/index.md](docs/index.md) — which docs to update when a path changes

## License

MIT
