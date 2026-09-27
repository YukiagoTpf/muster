# Documentation index

Routing table for documentation upkeep. When code changes, find the owning docs from the changed paths first.

`npm run check` machine-checks this table: every tracked path must match a route, and every doc named in the second column must exist. An empty owner (`—`) means the path needs no doc.

## Code path → doc owners

| Changed path | Docs to check |
| --- | --- |
| `AGENTS.md`, `CLAUDE.md` | `docs/index.md` |
| `bin/agentswap.mjs`, `src/commands/**` | `README.md`, `docs/architecture.md` |
| `src/commands/sync.mjs`, `src/commands/sync-agent.mjs`, `src/commands/remote.mjs` | `docs/sync.md`, `README.md` |
| `src/templates.mjs` | `docs/architecture.md`, `README.md` |
| `src/commands/create-profile.mjs` | `docs/architecture.md`, `README.md` |
| `src/store.mjs`, `src/reconcile.mjs`, `src/settings.mjs`, `src/live.mjs` | `docs/architecture.md` |
| `src/sync.mjs`, `src/ssh.mjs`, `src/remotes.mjs` | `docs/sync.md` |
| `src/web/**` | `README.md`, `docs/architecture.md` |
| `src/paths.mjs`, `src/launch.mjs`, `src/editor.mjs`, `src/reserved.mjs` | `README.md`, `docs/architecture.md` |
| `skills/**` | `README.md`, `docs/architecture.md` |
| `scripts/**`, `package.json` | `CONTRIBUTING.md`, `docs/index.md` |
| `src/cli-spec.mjs` | `README.md`, `CONTRIBUTING.md`, `docs/architecture.md` |
| `test/**` | `CONTRIBUTING.md` |
| `test/helpers/**` | `CONTRIBUTING.md` |
| `docs/**` | `docs/index.md` |
| `README.md`, `CONTRIBUTING.md` | `docs/index.md` |
| `.gitignore`, `.gitattributes`, `LICENSE` | — |
| `.github/**` | — |

If a changed path fits no row, add a row here while updating the nearest doc. Keep the table small and route-oriented; detailed behavior belongs in the owning doc.
