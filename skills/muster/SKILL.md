---
name: muster
description: Launch Claude Code instances with a chosen credential and model through the `muster` CLI — in this terminal or in a new side pane — and inspect which credentials exist and which one plain `claude` uses. Use when the user wants another agent beside this one on a different provider, key or model ("开个副 agent 用 codex 池", "用 xx 凭据起一个 Claude", "side pane with haiku"), asks which credentials or models are available, asks which credential is the default, or wants this machine's Claude Code settings synced with their other machines ("同步一下配置", "sync my settings").
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

## Sync with the user's other machines

```bash
muster sync status --json        # offline: what the next sync sends ("pending"), the tracked files
muster sync --json               # send and receive: "sent", "received"; exit 1 with "conflicts" when both sides changed the same thing
muster track <path> --json       # sync a file the user wrote (a hook or statusline script) with the shared settings
```

A conflict changes nothing on this machine. Show the user what conflicts and let them choose; run `muster sync --prefer local` or `--prefer remote` only with their answer.

## Rules

- Never handle a key or the sync passphrase. Don't read `secrets.json`, `sync.json` or `settings.json` to find one, and never put one on a command line. `muster sync init <repo>` asks for the passphrase, so leave it to the user: show them the command. If the user wants a new credential, show them the command and let them supply the key: `muster cred add <name> --provider <p> --base-url <url> --model opus=<id> --key-stdin < key-file`, or `--from-settings` to import what `~/.claude/settings.json` already uses.
- `muster default <credential>` rewrites the provider part of the user's `settings.json` (it backs the file up first). Only run it when the user asks to change what plain `claude` uses.
- A bare `muster` opens an interactive picker meant for a human; agents always use `muster run`.
