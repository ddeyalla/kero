---
title: Sessions & Projects
description: How Kero groups terminal sessions by project and keeps them running in kerod.
---

# Sessions & Projects

Kero organizes your work in two levels: projects (folders) hold sessions (tasks), and every session keeps running in kerod until you close it.

## Sessions

A **session** is a unit of work: one task, one agent, one long-running process. It holds one or more [panes](./panes.md), each running a shell or a program.

| Action                     | Shortcut                                     |
| -------------------------- | -------------------------------------------- |
| New session                | <kbd>⌘</kbd> <kbd>T</kbd>                    |
| New session in home folder | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>T</kbd>       |
| Go to session…             | <kbd>⌘</kbd> <kbd>P</kbd>                    |
| Jump to session needing you | <kbd>⌘</kbd> <kbd>J</kbd>                   |
| Next / previous session    | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>]</kbd> / <kbd>[</kbd> |
| Rename session             | <kbd>⌘</kbd> <kbd>R</kbd>                    |
| Silence or resume a session's notifications | Session menu, its context menu, or `kero do session.silence [session=<id>] [state=on\|off]` |
| Close session              | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>W</kbd>       |

Notifications follow a session's attention: one when it needs you, one when
its turn ends unseen, and none of a loop (the same session and state again
within 30 seconds of leaving that state) inside `notify-throttle-minutes` (five by
default). [Silencing a
session](./changes.md#notifications-from-attention) keeps it quiet until you
resume it, also across a restart.

Name sessions after their task, such as `fix-login-redirect` or `review-pr-42`, so you can find them in the sidebar and target them from the [CLI](./cli.md).

### Pin and reorder sessions

Right-click a session and choose **Pin Session in Project**. It appears before
unpinned sessions in that same project, marked with a pin. Choose **Unpin
Session** to return it to the unpinned section. A collapsed project continues
to show only its selected session.

Drag a session onto the upper or lower half of another session to place it
before or after that session. A neutral insertion line shows where it will land.
The carried row lifts on glass; dragging leaves your active terminal selected.
Reordering stays inside the same project and pin section. The context menu's
**Move Session Up** and **Move Session Down** provide the same control without
dragging. The Session menu and command palette offer these actions too; keys
can be assigned in Settings > Keyboard Shortcuts.

Pins and order survive restart. New sessions start unpinned. In the selected
session's project they follow it in the saved order; pinned rows still appear
first. Creating from a different project's header appends to that project.
Moving a session to another project keeps its pin and places it last in that
section.
The palette, number shortcuts and next/previous session shortcuts follow the
sidebar order. Session organization does not change its panes or running work.

Agents can run the same actions:

```sh
kero do session.pin session=<session-id>
kero do session.unpin session=<session-id>
kero do session.moveUp session=<session-id>
kero do session.moveDown session=<session-id>
```

Omit `session=<session-id>` to act on the selected session. Reordering requires
a connected terminal service; pinning is saved in the app's preferences.

### Where sessions appear

Sessions and their projects live in one of four saved places: the left sidebar
(the default), a right sidebar, tabs across the top, or tabs across the bottom.
Horizontal tabs replace the sidebar. Choose a placement in **Settings ›
General › Placement**, the **View** menu or the command palette, or run `kero do nav.placement.left`, `kero do nav.placement.right`,
`kero do nav.placement.top` or `kero do nav.placement.bottom`. None has a
default shortcut;
you can bind one in Settings › Keyboard. The placement survives a restart.

In the tabs, projects run from the left in their saved order, with pinned
sessions first and **Other** last. Click a project header to fold its sessions
away; it shows the session count and how many need you, keeps the selected
session's panes on screen, and marks itself while folded. Tabs shrink to a
readable width and then the row scrolls; the button at the right lists every
session by project. A session chosen there, in the palette, by a shortcut, in
an attention jump or from a plugin notification opens its project and scrolls
its tab into view. An attention jump reveals the tab even when that session
is already selected. A session whose project is unknown or empty is shown in
**Other**, and choosing it opens **Other** too. Folding a project changes no
selection and stays folded until an explicit selection asks to reveal it.

<kbd>⌘</kbd> <kbd>B</kbd> hides or shows whichever navigation surface is in
use without changing the placement. The compact utility icons in either
layout keep projects, search, new sessions, usage, attention and settings in
reach. At the narrowest sidebar, **Sidebar Actions** holds project open,
search and any status actions that do not fit; Usage, Settings, attention and
the connection/restart control stay in the footer.

### Where a new session starts

The `new-session-directory` setting picks the folder:

- `current` (default): the directory of the session you're in
- `project`: the project's root folder
- `home`: your home folder

### How an unnamed session is named

Until you or an agent name a session, the `session-name` setting (**Name sessions after** in Settings) picks what the sidebar shows:

- `focused` (default): the title of the program in the pane you're in. A pane running only its shell defers to the pane that needs you most.
- `lead`: the title of the program in the pane that needs you most, such as an agent that's working or waiting on you
- `folder`: the first pane's folder, whatever runs in it

## Projects

A **project** is a folder. Kero groups sessions under their project in the sidebar. Open one with <kbd>⌘</kbd> <kbd>O</kbd>, or let `kero new` pick it by the directory a session starts in.

```sh
kero projects            # list projects
kero new -p my-app       # start a session in a project by name
```

### Reorder projects

Drag a project's header above or below another project group. The insertion
line marks the boundary of the entire group, including its sessions. Sessions
keep their project and internal order. Project order survives daemon restart.
The synthetic **Other** group stays last and cannot be moved.

Right-click a project for **Move Project Up** / **Move Project Down**, also
available in the Session menu, palette and configurable shortcuts. Commands
use an explicit project id, or the selected session's project when omitted:

```sh
kero do project.moveUp project=<project-id>
kero do project.moveDown project=<project-id>
```

## kerod

Sessions run in **kerod**, a daemon that the app starts on demand. Kero is a window onto kerod:

- Quitting Kero leaves every session running.
- Opening Kero again shows every session as it was, scrollback included.
- After kerod itself restarts, such as after a reboot, Kero starts your sessions again in their directories. Turn this off with `restore-sessions = false`.
- A pane that was running Claude Code, Codex, Pi or OpenCode resumes its conversation, as the agent's own resume option would, once [Kero's skill](./agents.md#install-the-skill) is set up for that agent. Other programs don't start again unasked: their pane comes back at a shell. Turn this off with `resume-agents = false`.

```sh
kero daemon status       # is kerod running?
kero daemon restart      # restart it (sessions start again)
kero daemon stop         # stop it and end every session
```
