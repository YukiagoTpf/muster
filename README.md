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
```

Every command accepts `--json` and prints `{ "version": 1, ... }`; `muster help --json` describes them all.

## Documentation

- [docs/rebrand/vision.md](docs/rebrand/vision.md) — what Muster is and what the first version covers
- [docs/rebrand/CONTEXT.md](docs/rebrand/CONTEXT.md) — the vocabulary
- [docs/architecture.md](docs/architecture.md) — the design
- [docs/adr/](docs/adr/) — decisions and why
- [docs/index.md](docs/index.md) — which docs to update when a path changes

## License

MIT
