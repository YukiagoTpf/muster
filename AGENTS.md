# AGENTS.md

Agent entrypoint for working on this repo. Read this first; it stays short — long-form design lives in the docs it points to.

## Project model

AgentSwap is a multi-platform config switcher and launcher for coding agents. Four mechanisms carry the whole tool (full design: [docs/architecture.md](docs/architecture.md)):

1. **Profile = whole-file snapshot** — a profile is a complete agent `settings.json`; switching is a verbatim whole-file overwrite. AgentSwap never parses settings semantics.
2. **Three-way reconcile** — live / profile / baseline hashes decide `in-sync` / `live-unsaved` / `profile-edited` / `conflict`; every entry point funnels through `src/reconcile.mjs`.
3. **Transactional mutation** — one cross-process lock file (`pid:token`, `link(2)` atomic create, stale-owner stealing, `ELOCKFORMAT` fail-closed) held across read → reconcile → decide → write; a journal recovers a crash mid-commit; tmp+rename 0600 writes; split backup pools.
4. **Launch env ownership** — inherited auth/routing env is stripped before the profile's `env` is injected; `run` mode isolates via a private 0600 temp settings file.

Multi-machine sync over SSH landed in 0.4.0 ([docs/sync.md](docs/sync.md)); it is built under the same four mechanisms, not a fifth runtime. What remains frozen from v1 is listed in [docs/v1.md](docs/v1.md): Windows real-machine verification, field merging, settings semantics, background daemon. Do not reopen those without explicit direction.

## Design principles

- Every change should leave the system simpler: search for and reuse existing components first; delete dead code; fix the main path instead of hiding defects behind fallbacks.
- One canonical path per concern; no compatibility layers or fallback paths without explicit agreement.
- No defensive branches for hypothetical inputs.
- New complexity must prove long-term payoff above its implementation, runtime, and maintenance cost.

## Rules

The binding rules live in [CONTRIBUTING.md](CONTRIBUTING.md) — read it. The ones most often violated:

- **No product mentions** anywhere — code, docs, commits, issues.
- **Zero runtime dependencies** — Node.js stdlib only.
- **Atomic commits** — one logical change per commit, imperative subject; commit only named files (`git add <paths>`, never `-A`).
- **Every mutating command goes through `withLock`**; `--json` contracts are versioned (`{ "version": 2, ... }`).
- Docs update with code — the routing table in [docs/index.md](docs/index.md) maps changed paths to owning docs, and `npm run check` enforces it.

## Workflow

```bash
node --test        # full suite, must be green before committing
npm run check      # repo guardrails: zero deps, skill contract, doc routing, redlines
```

- Platform differences live only in `src/paths.mjs` and `src/launch.mjs`.
- The bundled agent skill lives in `skills/agentswap/`; its frontmatter follows the open SKILL.md spec (name matches the directory, description ≤ 1024 chars).
