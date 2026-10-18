---
name: muster
description: Launch Claude Code instances with a chosen credential and model through the `muster` CLI — in this terminal or in a new side pane — and inspect which credentials exist and which one plain `claude` uses. Use when the user wants another agent beside this one on a different provider, key or model ("开个副 agent 用 codex 池", "用 xx 凭据起一个 Claude", "side pane with haiku"), asks which credentials or models are available, or asks which credential is the default.
---

# Muster

Muster keeps the user's coding-agent credentials and launches Claude Code with one of them. A **credential** is a provider's base URL, its key and its model mapping (opus / sonnet / haiku … → model ids). Everything else in Claude Code's settings — hooks, permissions, plugins — is shared by every instance; a launch changes only the provider part.

Every command takes `--json` and prints `{ "version": 1, ... }`. Keys are never printed.

## Look before launching

```bash
muster cred list --json          # credentials: name, provider, baseURL, models [{alias, model}], hasKey
muster default --json            # which credential plain `claude` uses: state match|drift|unregistered|none
muster run <credential> -m <alias> --dry-run --json   # what a launch would set, blank and strip (key names only)
```

Pick the credential and model alias from `cred list`; don't guess names. If the user names a model that is not one of the credential's aliases, pass it anyway with `-m` — Claude Code resolves model ids itself.

## Launch beside this agent (herdr)

Run each instance in its own pane; never in this pane, which is running you. Split a sibling pane, keep the user's focus here, then run `muster run` in it:

```bash
herdr pane split --current --direction right --cwd "$PWD" --no-focus   # wide pane: right; tall or narrow: down
# read the new pane id from .result.pane.pane_id
herdr pane run <pane-id> "muster run <credential> -m <alias>"
herdr pane read <pane-id> --source recent-unwrapped --lines 40         # confirm it started
```

Arguments after `--` go to Claude Code, e.g. `muster run codex-pool -m opus -- --permission-mode plan`. Put nothing secret in the pane command: the credential name is all `muster run` needs.

## Rules

- Never handle a key. Don't read `secrets.json` or `settings.json` to find one, and never put one on a command line. If the user wants a new credential, show them the command and let them supply the key: `muster cred add <name> --provider <p> --base-url <url> --model opus=<id> --key-stdin < key-file`, or `--from-settings` to import what `~/.claude/settings.json` already uses.
- `muster default <credential>` rewrites the provider part of the user's `settings.json` (it backs the file up first). Only run it when the user asks to change what plain `claude` uses.
- A bare `muster` opens an interactive picker meant for a human; agents always use `muster run`.
