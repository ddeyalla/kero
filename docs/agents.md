---
title: Coding Agents
description: Teach Claude Code, Codex and other agents to run work beside themselves and report their progress in Shibu Code.
---

# Coding Agents

Agents running in Shibu Code can use the `shibu` command, and the `kero` one too, like you do: start a dev server in a pane below them, hand a review to a second agent, wait for tests, and tell you when they're blocked.

## Install the skill

```sh
shibu skill setup
```

This installs two things:

- A **skill** that teaches agents the CLI. Claude Code reads it from `~/.claude/skills/kero`; Codex, OpenCode and other agents from `~/.agents/skills/kero`.
- A **hook** that runs as an agent starts a session. It tells the agent which Shibu Code session and pane it's in, so it doesn't spend time finding out, and has it name an unnamed session after its task, such as `fix-login-redirect`, so you don't have to. It also tells Shibu Code the agent's own session, so that after kerod restarts, as after a reboot, the agent [picks up its conversation](./sessions.md#kerod). For Claude Code and Codex, the same hook runs as a turn starts, asks for permission and ends, and [tells the pane's status](./status.md#agents-that-report-their-turns).

The hook goes where each agent looks for one, for the agents you have installed:

| Agent       | Where the hook goes                                   |
| ----------- | ----------------------------------------------------- |
| Claude Code | Hooks in `~/.claude/settings.json`: `SessionStart`, and the events of its turns |
| Codex       | Hooks in `~/.codex/hooks.json`: `SessionStart`, and the events of its turns |
| Pi          | An extension, `~/.pi/agent/extensions/kero.ts`         |
| OpenCode    | A plugin, `~/.config/opencode/plugins/kero`            |

Shibu Code follows `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `PI_CODING_AGENT_DIR` and `XDG_CONFIG_HOME` when they move an agent's files, and leaves the rest of each settings file as it was.

Pass `--agent` with `claude`, `codex`, `pi` or `opencode` to set up one agent only. The skill comes from your Shibu Code binary, so run `shibu skill setup` again after updating Shibu Code. `shibu skill status` tells you whether each part is current.

```sh
shibu skill status    # what's installed, and whether it's current
shibu skill show      # print the skill
shibu skill remove    # uninstall the skill and the hooks
```

The hook works only in agent sessions started after setup, and agents may ask first:

- **Claude Code** runs hooks only in folders you trust.
- **Codex** asks you to approve a new hook: approve Shibu Code's with `/hooks`. After an update that adds events, approve the new ones too; `SessionStart` stays approved.
- **OpenCode** loads plugins as its background service starts: run `opencode service restart` once, when no session is busy.

OpenCode runs an agent's commands in its background service rather than in the pane, so they don't have `KERO_SESSION` and `KERO_PANE`. Shibu Code tells the agent its pane instead, for it to name as the target. For the same reason, `shibu report` from an OpenCode agent doesn't reach its pane.

Outside Shibu Code, the hook does nothing.

## What agents can do

Run something beside themselves:

```sh
shibu split --down -- npm run dev
shibu new -n review -- claude
```

Drive it and read the result:

```sh
shibu prompt review "review the changes in src/auth"
shibu wait tests --for idle,error --timeout 10m
shibu read tests -n 60
```

Run tests beside themselves and read what failed:

```sh
pane=$(shibu split --down -- go test ./...)
shibu wait "$pane" --exit || shibu read "$pane" -n 80
```

Tell you what's going on:

```sh
shibu rename $KERO_SESSION fix-login-redirect
shibu report working "Migrating the database" --progress 40
shibu report blocked "Approve the production deploy?"
shibu report done "Deployed to staging"
```

A `blocked` report turns the session to *waiting* and notifies you, so agents should use it only when they can't continue without you.

## Remote Control

With **Remote Control** on (the default, in **Settings › Agents**), agents can also drive the app itself: show a session with `shibu focus`, or run menu actions with `shibu do`. Turn it off with `remote-control = false` to keep agents to their own sessions.

## Environment

Every pane gets:

| Variable       | Value                          |
| -------------- | ------------------------------ |
| `KERO_SESSION` | The id of the pane's session   |
| `KERO_PANE`    | The id of the pane             |

Check `$KERO_PANE` to tell whether a script runs inside Shibu Code.
