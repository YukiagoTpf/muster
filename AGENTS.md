# AGENTS.md

Agent entrypoint for working on this repo. Read this first; it stays short — long-form design lives in the docs it points to.

## Project model

Muster launches coding agents per credential and shows every subscription's quota at a glance. It is a Go rewrite of AgentSwap (the Node tool, preserved at tag `v0.4.0` and on `main`), developed on the `muster` branch. The plan is [docs/vision.md](docs/vision.md), the vocabulary is [CONTEXT.md](CONTEXT.md) — use its terms (credential, subscription account, default credential, shared settings, …) in code and docs. Design: [docs/architecture.md](docs/architecture.md); decisions: [docs/adr/](docs/adr/).

Three mechanisms carry the tool:

1. **A credential owns the provider's env** — exactly the agent's auth/routing env namespace (base URL, key, model mapping). Everything else in the agent's settings is shared settings, which Muster syncs but never mixes with credentials.
2. **Launch env ownership** — a launch strips inherited auth/routing env and injects the credential through a private temp `--settings` file that also blanks every default-credential key the credential doesn't set, so the default can't leak into another credential's instance.
3. **Transactional mutation** — one OS file lock (`flock` / `LockFileEx`, released by the kernel if the holder dies) around every read → decide → write; writes are tmp + rename, 0600.

## Design principles

- Every change should leave the system simpler: search for and reuse existing components first; delete dead code; fix the main path instead of hiding defects behind fallbacks.
- One canonical path per concern; no compatibility layers or fallback paths without explicit agreement.
- No defensive branches for hypothetical inputs.
- New complexity must prove long-term payoff above its implementation, runtime, and maintenance cost.

## Rules

The binding rules live in [CONTRIBUTING.md](CONTRIBUTING.md) — read it. The ones most often violated:

- **No product mentions** anywhere — code, docs, commits, issues.
- **Every direct dependency is justified** in CONTRIBUTING.md's dependency table; `go test ./...` enforces it.
- **Atomic commits** — one logical change per commit, conventional subject; commit only named files (`git add <paths>`, never `-A`).
- **Every mutating command goes through `Store.WithLock`**; `--json` contracts are versioned (`{ "version": 1, ... }`).
- **Never push or tag** until the user declares the Muster rewrite done.
- Docs update with code — the routing table in [docs/index.md](docs/index.md) maps changed paths to owning docs, and `go test ./...` enforces it.

## Workflow

```bash
go test ./...      # unit, e2e and repo guardrails — must be green before committing
go vet ./...
```

- Platform differences live only in `internal/platform`.
- Tests never touch a real config: they point `MUSTER_HOME` and `CLAUDE_CONFIG_DIR` at temp dirs.
