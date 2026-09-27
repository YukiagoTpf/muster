# Contributing

Thanks for your interest in AgentSwap. (Agents working on this repo: read [AGENTS.md](AGENTS.md) first.)

## Ground rules

- **No product mentions.** This repo stays neutral: don't name, compare with, or screenshot other config tools — in code, docs, commits, or issues.
- Keep it dependency-free. Node.js stdlib only; the whole tool must run on a fresh Node ≥ 20 install.
- Every mutating command goes through `withLock` and writes atomically (tmp + rename, 0600).
- Never break the `--json` contracts (`{ "version": 2, ... }`) silently — bump the version field if a shape changes.
- New commands register in BOTH `src/reserved.mjs` AND `src/cli-spec.mjs`. The registry generates HELP and `help --json`; a command missing from the registry has no usage strings or schema.
- Secrets stay masked by default. New output paths need an explicit opt-in for raw values.

## Development

```bash
node --test                 # run everything
npm run check               # repo guardrails (zero deps, skill contract, doc routing, redlines)
AGENTSWAP_HOME=/tmp/x node bin/agentswap.mjs list   # isolated manual testing
```

Tests live in `test/`. CLI e2e tests spawn the real binary with `AGENTSWAP_HOME` and the agent's config dir pointed at temp dirs — never touch a real config. Multi-machine sync e2e uses `test/helpers/fake-ssh.mjs` (an `ssh` shim that maps host aliases onto extra temp homes); those tests skip on Windows.

When code changes, check [docs/index.md](docs/index.md) for the owning docs — the routing table there is machine-checked by `npm run check`. The redline grep reads `.redlines.local.json` (gitignored) if present; the term list never enters the repo.

## Commit style

Small, atomic commits: one logical change per commit, imperative subject (`fix: …`, `feat: …`, `docs: …`).

## Cross-platform changes

If you touch platform-specific code (paths, process spawning, file locking), check `src/paths.mjs` and `src/launch.mjs` — platform differences are confined there on purpose. Windows behavior is designed but not CI-tested yet; be extra careful with `win32` branches.

`node --test` and `npm run check` must pass on Linux, macOS and Windows. The tree is LF everywhere (`.gitattributes`), and a test whose premise only holds on some platforms (POSIX file modes, creating symlinks, a POSIX `ssh` shim) skips elsewhere with the reason — it never fails there.
