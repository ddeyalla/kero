---
title: Panes
description: Split a session into panes to run a dev server, tests or a second agent side by side.
---

# Panes

Split a session to run things side by side: an agent on the left, its dev server on the right, a test watcher below.

| Action             | Shortcut                                          |
| ------------------ | ------------------------------------------------- |
| Split right        | <kbd>⌘</kbd> <kbd>D</kbd>                         |
| Split down         | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>D</kbd>            |
| Zoom pane          | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>↩</kbd>            |
| Next / previous pane | <kbd>⌘</kbd> <kbd>]</kbd> / <kbd>[</kbd>        |
| Move between panes | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>←</kbd> <kbd>→</kbd> <kbd>↑</kbd> <kbd>↓</kbd> |
| Restart pane       | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>R</kbd>            |
| Close pane         | <kbd>⌘</kbd> <kbd>W</kbd>                         |

**Zoom** fills the session with one pane; press it again to bring the others back.

A pane can also show a plugin's widget. Open one from the New Pane chooser
(the plugin rows come first), the command palette, <kbd>⌘</kbd> <kbd>⌥</kbd>
<kbd>P</kbd>, or `kero split --plugin <id> [--widget <name>]`. The pane keeps
its plugin, widget and state in its target, so it comes back after a restart.
A plugin starts when its first pane opens and stops a few seconds after the
last one closes. Two plugins ship in the app: **Spotify** (`kero split --plugin
spotify`) and **Usage** (`kero split --plugin usage`); their widgets are rows
in the chooser and actions in the palette.

When a pane's program exits, the pane stays so you can read its output. **Restart Pane** starts its shell again in the same directory.

## Views

A pane can show something other than a shell:

| View          | What it shows                                              |
| ------------- | ---------------------------------------------------------- |
| **Tasks**     | The project's Linear issues, as the board and the list      |
| **Files**     | The project's folder, with a file read beside it            |
| **Browser**   | A page, most often a dev server                            |
| **Attention** | Every session that needs you, in one list                   |

<kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>N</kbd>, or the pane header's **+**, opens the chooser: <kbd>T</kbd>, <kbd>L</kbd>, <kbd>E</kbd>, <kbd>B</kbd> and <kbd>A</kbd> pick a kind, and holding <kbd>⇧</kbd> opens it below rather than beside. Each kind also has its own shortcut (<kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>A</kbd> for Attention), and **Show in This Pane** in the pane's menu turns one kind into another.

**Attention** lists the sessions that need you, the most urgent first: what each is doing or asking, the session and its project, the agent, and how long it has been so. A click opens the pane that is asking, with the keyboard in it, so you can answer there. The chips narrow the list to **Needs you**, **Working**, **Unread** or **Done** and count each; **All** is every session. The search finds a row by its words.

## From the command line

`kero split` starts a pane beside another and prints its id:

```sh
kero split --down -- npm run dev
kero split --right --ratio 0.3 -- go test ./... -count=1
```

| Flag                                  | Meaning                                        |
| ------------------------------------- | ---------------------------------------------- |
| `--right` (default), `--down`, `--left`, `--up` | Where the new pane goes               |
| `-r`, `--ratio <0..1>`                | The new pane's share of the space (default 0.5) |
| `-d`, `--dir <dir>`                   | Start it in another directory                  |
| `-w`, `--wait`                        | Wait for its command to end, and exit with its status |
| `--plugin <id>`                       | Open a plugin's widget pane instead of a shell |
| `--widget <name>`                     | The widget to open (default: the plugin's first) |
| `--tasks`                             | The Linear plugin's board, not a shell          |
| `--files [--open <file>]`             | The project's files, with one open             |
| `--browser <url>`                     | A page, such as a dev server                   |
| `--attention`                         | Every session that needs you                   |

A view runs no command: `kero split --attention` opens the pane and prints its id.

Run inside Kero, `kero split` splits the pane it runs in. Elsewhere, name a target: `kero split my-session --down`.
