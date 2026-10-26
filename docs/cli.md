---
title: Command Line
description: Drive Shibu Code sessions and panes from a shell or a script with the shibu CLI.
---

# Command Line

The `shibu` command drives sessions from any shell: yours, a script's or an agent's. Install it with **Shibu Code › Install Command Line Tool…**. The same executable is installed as `kero`, which runs every command here.

## Targets

Most commands take a **target**: a session or a pane. A target can be:

- an id, or any unique prefix of it
- a session's name
- a session, which stands for its focused pane

Inside Shibu Code, `$KERO_SESSION` and `$KERO_PANE` name the session and pane a command runs in, and they're the default target.

## Sessions and panes

```sh
shibu ls                          # every session and its panes
shibu new -n api -d ~/code/api    # start a session, print its id
shibu new -- npm run dev          # start one running a command
shibu split --down -- npm test    # a pane beside this one
shibu split --plugin spotify      # a plugin's widget in a pane
shibu rename api backend          # rename a session
shibu focus backend               # show it in the app
shibu close backend               # end a session (or, given a pane, the pane)
shibu restart <pane>              # start an exited pane's shell again
```

`shibu split --plugin <id> [--widget <name>]` opens a plugin pane instead
of a shell; `--plugin` takes the plugin's id and `--widget` the widget's name.
The built-in plugins are `spotify` (widget `player`) and `usage` (widget
`usage`); `shibu split --plugin spotify` opens the player.

`shibu new` options:

| Flag                        | Meaning                                         |
| --------------------------- | ----------------------------------------------- |
| `-p`, `--project <name\|id>` | The project to put it in (default: by directory) |
| `-n`, `--name <name>`       | Name the session                                |
| `-d`, `--dir <dir>`         | Start it there (default: the current directory) |
| `-f`, `--focus`             | Show it in the app                              |
| `-w`, `--wait`              | Wait for its command to end, and exit with its status |

## Typing and reading

```sh
shibu send api "npm run migrate" --enter   # type text, then press Enter
shibu send api -e 'y\n'                    # interpret \n \r \t \e \xHH escapes
shibu key api ctrl+c                       # press keys
shibu read api -n 40                       # the last 40 lines on screen
shibu read api -a                          # the screen with its scrollback
```

`shibu key` understands `enter`, `tab`, `escape`, `up`, `down`, `left`, `right`, `backspace`, and modifiers such as `ctrl+c` or `ctrl+d`.

## Waiting

```sh
shibu wait api                             # until it stops working
shibu wait api --for waiting,error --timeout 10m
shibu prompt review "summarize the diff"   # send + Enter, wait, print the screen
```

`shibu wait` prints the pane's status when it returns. `shibu prompt` sends text and Enter, waits until the pane stops working, then prints its screen. It's the quickest way to ask another agent something.

### Waiting for a command

A command such as `make test` doesn't show as *working*, so to wait for one to finish, start it with `--wait`. `shibu` then exits with the command's own status:

```sh
if shibu split --down --wait -- make test; then
  echo "tests passed"
fi
```

`shibu wait --exit` does the same for a pane that is already running a command it started with:

```sh
pane=$(shibu split --down -- go test ./...)
# … do other things …
shibu wait "$pane" --exit --timeout 10m
echo "go test exited with $?"
```

The pane stays open after the command, with its output, so you can `shibu read` it. Waiting for a command doesn't work with `cmd` as the shell, which can't tell when one ends.

## Status

```sh
shibu status api --json                    # status, program, directory
shibu report working "Running migrations"  # report your own status
shibu notify "Need your input"             # flag this pane as needing you
```

See [Status & Notifications](./status.md) for the states.

## Configuration and setup

```sh
shibu config path                 # where the configuration file is
shibu config show                 # every key with its value
shibu config show --defaults      # every key as shibu comes
shibu config edit                 # open it in $EDITOR
shibu config check                # report unknown keys and bad values
shibu projects                    # list projects
shibu skill setup                 # teach coding agents to use shibu
shibu daemon status|stop|restart  # manage kerod
```

## Running app actions

`shibu do` runs any of the app's actions, the same ones as its menus. It needs Remote Control, which is on by default in **Settings › Agents**.

```sh
shibu actions                     # list them
shibu do pane.split.right
shibu do sidebar.toggle
shibu do nav.placement.right                     # left, right, top or bottom
shibu do session.attention session=<id or name>  # focus what this session needs
shibu do attention.next-blocked   # the next session waiting on you
shibu do session.silence session=<id or name> state=on   # on, off; no state toggles
```

A plugin's commands are actions too, as `plugin.<id>.<command>`. `shibu actions` lists them with the app's own. A command runs on a pane of its plugin, and opens one when there is none.

```sh
shibu do plugin.example.refresh
```
Arguments are `key=value` pairs after the action; an action reads the ones it knows.

`attention.next-blocked` goes to the next session whose attention state is
`waiting-user` (for an older daemon, the fold of all terminal panes), longest
wait first, and puts the pane that needs you in
front. With none waiting it does nothing and says so. It has no default
shortcut; the Session menu, the command palette and `shibu do` reach it.


## Exit status

| Code | Meaning                              |
| ---- | ------------------------------------ |
| `0`  | OK                                   |
| `1`  | Failed                               |
| `2`  | Bad usage                            |
| `3`  | No such target, or an ambiguous one  |
| `4`  | kerod is unreachable                 |

Commands that wait for a program, `shibu wait --exit` and `--wait`, exit with the program's status instead, as does `shibu wait` when the pane's shell exits with an error. A wait that times out exits with `1`.

Add `--json` to `ls`, `status` and `projects` for machine-readable output.
