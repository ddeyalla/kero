---
title: Status & Notifications
description: How Kero tracks what each pane is doing, and how programs report their own status.
---

# Status & Notifications

Kero tracks what every pane is doing, so you can leave agents to work and come back when one needs you.

## Pane status

Every pane has a status, shown in the sidebar:

| Status    | Meaning                                       |
| --------- | --------------------------------------------- |
| `working` | A program is busy: an agent thinking, a tool it runs |
| `waiting` | It needs you: a question, a permission prompt |
| `idle`    | Nothing is happening; the shell is at a prompt |
| `error`   | The program reported that it failed           |
| `exited`  | The program ended; the pane keeps its output  |

A session's mark says its state at a glance: a turning ring while it works,
or a ring filling to the progress the program reported; an amber ring with a
dot when it needs you; a filled check when it is done and you have not read
it, and a faint check once you have; and a half moon when the session is
inactive, its shell exited. The count chips in the footer use the same marks.

Kero tells a coding agent's status from what its screen shows: its spinner, a permission prompt, its prompt waiting for you. Redrawing the same screen, as when you resize a pane, changes nothing. Claude Code and Codex also [report their turns](#agents-that-report-their-turns) through Kero's hook, which is quicker and surer than the screen. Other programs are `working` while their title shows a spinner, or when they [report their status](#reporting-status-from-a-program).

### Agents that report their turns

With the hook `kero skill setup` installs, Claude Code and Codex tell Kero as a turn starts, asks for permission and ends, and the pane shows it at once:

| Event                                       | Claude Code | Codex | The pane    |
| ------------------------------------------- | ----------- | ----- | ----------- |
| You send a prompt (`UserPromptSubmit`)      | yes         | yes   | `working`   |
| It asks for permission or a choice          | `Notification` | `PermissionRequest` | `waiting`, with the agent's words |
| A tool ran, as after you approved it (`PostToolUse`) | yes | yes | `working` |
| The turn ends (`Stop`)                      | yes         | yes   | `idle`, as an agent that finished |
| The turn fails on an API error (`StopFailure`) | yes      |       | `error`     |
| The session ends (`SessionEnd`, also on `/clear`) | yes   |       | what it reported goes |

The screen still counts. An agent says nothing when you stop a turn with <kbd>Esc</kbd>, so once its prompt has shown for a moment the pane goes `idle`; and once a permission you granted is gone from the screen, the pane is `working` again. What an agent reports stands beside what other programs in the pane report, by the same order: `blocked` over `working` over `error` over `done`.

The hook reports only for the agent its pane runs: a `claude -p` an agent starts in its pane, or a subagent, reports nothing. Outside Kero it does nothing, and it never writes to the agent. Codex skips a hook until you trust it: approve Kero's new ones with `/hooks`. Set `agent-hooks = false` in the [configuration](./configuration.md#agents) to leave the status to the screen alone; it applies at once.

A session shows the status of its panes, and sessions that need you stand out. Press <kbd>⌘</kbd> <kbd>J</kbd> to jump to the next one.

## Notifications

Kero notifies you when you're looking elsewhere and:

- a session comes to need you (`notify-on-waiting`)
- an agent finishes (`notify-on-done`)
- a program reports that it failed (`notify-on-error`)

A program's own desktop notifications show as they would in any terminal, and leave its status alone.

The Dock icon shows how many sessions need you (`dock-badge`) and can bounce once when one starts to (`dock-bounce`). See [Configuration](./configuration.md).

## Reporting status from a program

Programs and agents can tell Kero what they're doing with `kero report`:

```sh
kero report working "Migrating the database" --progress 40
kero report blocked "Approve the production deploy?" --kind permission
kero report done "Deployed to staging"
kero report clear
```

The states are:

| State     | Meaning                                                    |
| --------- | ---------------------------------------------------------- |
| `idle`    | At rest, waiting for the next instruction                  |
| `working` | Busy; `--progress` gives a percentage                      |
| `blocked` | It can't go on without you; `--kind` says what it needs    |
| `done`    | It finished, and you haven't looked yet                    |
| `error`   | It failed and stopped                                      |
| `clear`   | Not a state: removes what the program reported             |

Kero shows the message in the sidebar. A `blocked` report turns the session to *waiting*, and `done` or `blocked` notify you when you're away. `--kind` is `permission` (approve something), `question` (type an answer) or `auth` (a login or a token).

`working` and `blocked` last only as long as the program: they end when it exits. `done` and `error` stay until you type in the pane, so a program that finishes while you're away leaves its result for you to find. A program that is interrupted should report `idle`.

To flag a pane as needing you without changing its state:

```sh
kero notify "Need your API key to continue"
```

### Several tasks in one pane

A program that runs several things at once can report each of them under an `--id`, and name it with `--title`:

```sh
kero report working "Deploying v2.4.1" --app deploy
kero report working "Pushing image" --id us-east --title "US East" --progress 40
kero report blocked "Approve deploy to production?" --id eu-west --title "EU West" --kind permission
kero report clear --id us-east
```

Each report replaces what was there under its id, so repeat `--app` and `--title` in every one. An id can have children, as `build/test` is a child of `build`, and `clear` with an id removes it and its children. The pane shows the report that most needs you: `blocked` over `working` over `error` over `done`.

`--app` is a stable name for the program, such as `cargo` or `terraform`, which ids under it take on.

### The escape sequence

`kero report` writes an OSC 7501 escape sequence, the [Program Status Protocol](https://www.superlogical.com/rex/docs/build/program-status), which any program can write itself. It isn't Kero's own: a program that adopts it reports to every terminal that reads it.

```txt
ESC ] 7501 ; state=<state> [:id=<id>] [:kind=<kind>] [:progress=<n>] [:app=<app>] [:title=<base64>] [:msg=<base64>] ESC \
```

Fields are separated by `:`, and `msg` and `title` are Base64-encoded UTF-8. For example, in a shell:

```sh
status() {
  printf '\e]7501;state=%s:msg=%s\e\\' "$1" "$(printf '%s' "$2" | base64 | tr -d '\n')"
}
status working "Syncing photos"
rsync -a ~/Photos backup:/photos && status done "Photos synced" || status error "rsync failed"
```

A program can ask whether the terminal reads the protocol by writing `ESC ] 7501 ; ? ESC \`. Kero answers with the same sequence.

Kero also reads ConEmu's progress sequence, OSC 9;4, which some build tools write, until a program reports with OSC 7501.
