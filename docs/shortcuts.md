---
title: Keyboard Shortcuts
description: Kero's default keyboard shortcuts and the action ids to rebind them.
---

# Keyboard Shortcuts

Rebind any of these in **Settings › Keyboard**, or with `keybind` in the [configuration file](./configuration.md#keyboard).

On Windows and Linux, <kbd>Ctrl</kbd> with a letter belongs to the shell (<kbd>Ctrl</kbd> <kbd>D</kbd>, <kbd>Ctrl</kbd> <kbd>R</kbd>, <kbd>Ctrl</kbd> <kbd>W</kbd>…), so Kero's shortcuts add <kbd>Shift</kbd>, as Windows Terminal's do, or use <kbd>Alt</kbd>. A dash means the action has no shortcut there.

## Kero

| Action                       | macOS                                  | Windows & Linux                               | Action id        |
| ---------------------------- | -------------------------------------- | --------------------------------------------- | ---------------- |
| Settings…                    | <kbd>⌘</kbd> <kbd>,</kbd>              | <kbd>Ctrl</kbd> <kbd>,</kbd>                  | `settings.open`  |
| Quit (sessions keep running) | <kbd>⌘</kbd> <kbd>Q</kbd>              | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>Q</kbd> |                  |
| Quit and End All Sessions…   | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>Q</kbd> | –                                             | `app.quitAll`    |

## Session

| Action                      | macOS                                  | Windows & Linux                               | Action id                |
| --------------------------- | -------------------------------------- | --------------------------------------------- | ------------------------ |
| New Session                 | <kbd>⌘</kbd> <kbd>T</kbd>              | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>T</kbd> | `session.new`            |
| New Session in Home         | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>T</kbd> | –                                             | `session.newHome`        |
| Open Project…               | <kbd>⌘</kbd> <kbd>O</kbd>              | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>O</kbd> | `project.open`           |
| Go to Session…              | <kbd>⌘</kbd> <kbd>P</kbd>              | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>P</kbd> | `palette.open`           |
| Jump to Session Needing You | <kbd>⌘</kbd> <kbd>J</kbd>              | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>J</kbd> | `session.attention`      |
| Next Waiting Session    | –                                      | –                                             | `attention.next-blocked` |
| Next Session                | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>]</kbd> | <kbd>Ctrl</kbd> <kbd>PgDn</kbd>               | `session.next`           |
| Previous Session            | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>[</kbd> | <kbd>Ctrl</kbd> <kbd>PgUp</kbd>               | `session.previous`       |
| Rename Session              | <kbd>⌘</kbd> <kbd>R</kbd>              | –                                             | `session.rename`         |
| Restart Pane                | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>R</kbd> | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>R</kbd> | `pane.restart`           |
| Close                       | <kbd>⌘</kbd> <kbd>W</kbd>              | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>W</kbd> | `pane.close`             |
| Close Session               | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>W</kbd> | –                                             | `session.close`          |

<kbd>⌘</kbd> <kbd>1</kbd>–<kbd>9</kbd> (<kbd>Ctrl</kbd> <kbd>1</kbd>–<kbd>9</kbd> on Windows and Linux) go to the sessions in the navigation order, in a sidebar or the tabs. Use the platform-specific **Next Session** shortcut above to move to the next one. Choosing a session whose tab is scrolled out of sight, or whose project is folded, brings it back into view.

## Pane

| Action        | macOS                                  | Windows & Linux                                   | Action id          |
| ------------- | -------------------------------------- | ------------------------------------------------- | ------------------ |
| Split Right   | <kbd>⌘</kbd> <kbd>D</kbd>              | <kbd>Alt</kbd> <kbd>Shift</kbd> <kbd>=</kbd>      | `pane.split.right` |
| Split Down    | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>D</kbd> | <kbd>Alt</kbd> <kbd>Shift</kbd> <kbd>-</kbd>      | `pane.split.down`  |
| New Plugin Pane | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>P</kbd> | <kbd>Ctrl</kbd> <kbd>Alt</kbd> <kbd>P</kbd>   | `pane.new.plugin`  |
| Zoom Pane     | <kbd>⌘</kbd> <kbd>⇧</kbd> <kbd>↩</kbd> | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>Enter</kbd> | `pane.zoom`        |
| Next Pane     | <kbd>⌘</kbd> <kbd>]</kbd>              | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>]</kbd>     | `pane.next`        |
| Previous Pane | <kbd>⌘</kbd> <kbd>[</kbd>              | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>[</kbd>     | `pane.previous`    |
| Pane Left     | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>←</kbd> | <kbd>Alt</kbd> <kbd>←</kbd>                       | `pane.left`        |
| Pane Right    | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>→</kbd> | <kbd>Alt</kbd> <kbd>→</kbd>                       | `pane.right`       |
| Pane Above    | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>↑</kbd> | <kbd>Alt</kbd> <kbd>↑</kbd>                       | `pane.up`          |
| Pane Below    | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>↓</kbd> | <kbd>Alt</kbd> <kbd>↓</kbd>                       | `pane.down`        |
| New Pane…     | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>N</kbd> | <kbd>Ctrl</kbd> <kbd>Alt</kbd> <kbd>N</kbd>       | `pane.new`         |
| New Terminal Pane | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>T</kbd> | <kbd>Ctrl</kbd> <kbd>Alt</kbd> <kbd>T</kbd> | `pane.new.terminal` |
| New Tasks Pane | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>L</kbd> | <kbd>Ctrl</kbd> <kbd>Alt</kbd> <kbd>L</kbd>       | `pane.new.tasks`   |
| New Files Pane | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>E</kbd> | <kbd>Ctrl</kbd> <kbd>Alt</kbd> <kbd>E</kbd>       | `pane.new.files`   |
| New Browser Pane | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>B</kbd> | <kbd>Ctrl</kbd> <kbd>Alt</kbd> <kbd>B</kbd>     | `pane.new.browser` |
| New Attention Pane | <kbd>⌘</kbd> <kbd>⌥</kbd> <kbd>A</kbd> | <kbd>Ctrl</kbd> <kbd>Alt</kbd> <kbd>A</kbd>   | `pane.new.attention` |

## View

| Action                       | macOS                     | Windows & Linux                               | Action id             |
| ---------------------------- | ------------------------- | --------------------------------------------- | --------------------- |
| Toggle Navigation            | <kbd>⌘</kbd> <kbd>B</kbd> | <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>B</kbd> | `sidebar.toggle`      |
| Navigation in Left Sidebar   | –                         | –                                             | `nav.placement.left`  |
| Navigation in Right Sidebar  | –                         | –                                             | `nav.placement.right` |
| Navigation in Tabs Across the Top    | –                         | –                                             | `nav.placement.top`   |
| Navigation in Tabs Across the Bottom | –                         | –                                             | `nav.placement.bottom` |
| Bigger                       | <kbd>⌘</kbd> <kbd>=</kbd> | <kbd>Ctrl</kbd> <kbd>=</kbd>                  | `text.bigger`         |
| Smaller                      | <kbd>⌘</kbd> <kbd>-</kbd> | <kbd>Ctrl</kbd> <kbd>-</kbd>                  | `text.smaller`        |
| Actual Size                  | <kbd>⌘</kbd> <kbd>0</kbd> | <kbd>Ctrl</kbd> <kbd>0</kbd>                  | `text.reset`          |

The placement actions have no default shortcut; assign one in Settings › Keyboard if you want it.
