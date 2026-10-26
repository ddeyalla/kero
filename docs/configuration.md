---
title: Configuration
description: Shibu Code's configuration file, its keys and their defaults.
---

# Configuration

Shibu Code keeps its settings in one plain-text file. Settings (<kbd>⌘</kbd> <kbd>,</kbd>), over the main window, edits it for you, keeping your comments and order, and Shibu Code reloads it when you edit it by hand.

## The file

Shibu Code reads, in order of preference:

1. `$KERO_CONFIG`
2. `$XDG_CONFIG_HOME/kero/config`
3. `~/.config/kero/config`

It uses Ghostty's syntax: one `key = value` per line, `#` for comments, and repeated lines for keys that hold lists. Where a key means what it means in Ghostty, it has Ghostty's name.

```ini
# ~/.config/kero/config
font-size = 14
theme = light:GitHub Light Default,dark:Catppuccin Mocha
cursor-style = bar

env = EDITOR=nvim
env = HOMEBREW_NO_AUTO_UPDATE=1

keybind = cmd+shift+d=pane.split.down
```

The file only needs what you change. Everything else keeps its default.

```sh
shibu config edit             # open it in $EDITOR
shibu config show             # every key with its current value
shibu config check            # report unknown keys and bad values
```

## General

| Key                     | Default   | Description                                                   |
| ----------------------- | --------- | ------------------------------------------------------------- |
| `new-session-directory` | `current` | Where a new session starts: `current`, `project` or `home`    |
| `session-name`          | `focused` | What names a session you haven't: `focused`, `lead` or `folder` ([more](./sessions.md#how-an-unnamed-session-is-named)) |
| `confirm-close`         | `true`    | Ask before closing a pane that a program is running in        |
| `restore-sessions`      | `true`    | Start sessions again after kerod restarts, as after a reboot  |
| `resume-agents`         | `true`    | As sessions start again, resume the conversation of the agent each pane ran |

## Appearance

| Key                  | Default  | Description                                                     |
| -------------------- | -------- | --------------------------------------------------------------- |
| `window-theme`       | `system` | The app's appearance: `system`, `light` or `dark`               |
| `language`           | `system` | The app's language: `system`, `en` or `zh-Hans`                 |
| `accent`             | `blue`   | The accent's preset: `blue`, `plum`, `rose`, `sea`, `moss` or `graphite` — its color and the terminals' selection. The key leaves the wash alone: that is `background`, which Settings › Appearance › Accent sets to the preset's wash as you pick one there |
| `background`         | `petal`  | The window's wash: `petal`, `mist`, `meadow`, `dusk`, `plum`, `rose`, `sea` or `plain` |
| `window-translucent` | `true`   | Let the desktop show through the window                         |
| `grain`              | `true`   | A faint grain over the wash, so flat gradients read as material |

## Terminal

| Key                   | Default | Description                                                              |
| --------------------- | ------- | ------------------------------------------------------------------------ |
| `theme`               | `kero`  | Terminal colors: `kero`, any Ghostty theme, or `light:<theme>,dark:<theme>` |
| `font-family`         |         | Empty for JetBrains Mono Nerd Font, which Shibu Code includes; `monospace` for the system's |
| `font-size`           | `13`    | In points                                                                |
| `font-weight`         | `400`   | `100` to `900`                                                           |
| `line-height`         | `1.22`  | A multiple of the font's                                                 |
| `font-ligatures`      | `true`  | Join characters the way the font draws them                              |
| `font-thicken`        | `false` | A heavier stroke, as in Ghostty                                          |
| `minimum-contrast`    | `3`     | The least contrast text keeps with its background, from `1` (none) to `21`, so that colors meant for a dark background stay readable on a light one, as in Ghostty |
| `cursor-style`        | `block` | `block`, `bar` or `underline`; programs may change it                    |
| `cursor-style-blink`  | `true`  | Blink the cursor                                                         |
| `macos-option-as-alt` | `false` | Option sends Alt (Meta) to programs                                      |
| `copy-on-select`      | `false` | Copy what you select to the clipboard                                    |
| `clipboard-read`      | `ask`   | Programs reading the clipboard (OSC 52): `ask`, `allow` or `deny`        |
| `right-click-action`  | `context-menu` | `context-menu`, `paste` or `copy-or-paste`                        |

## Shell

| Key                | Default | Description                                                    |
| ------------------ | ------- | -------------------------------------------------------------- |
| `command`          |         | The shell of new panes; empty for your login shell             |
| `env`              |         | Added to every pane's environment as `NAME=value`; repeat it   |
| `scrollback-limit` | `4`     | The output each pane keeps, in megabytes; `0` for none         |

## Notifications

| Key                 | Default | Description                                    |
| ------------------- | ------- | ---------------------------------------------- |
| `notify-on-waiting` | `true`  | When a session needs you                       |
| `notify-on-done`    | `true`  | When an agent finishes                         |
| `notify-on-error`   | `true`  | When a program reports it failed               |
| `notify-sound`      | `false` | Play a sound with notifications                |
| `notify-throttle-minutes` | `5` | How long the same session and state stays quiet after a notification, in minutes, when it asks again within 30 seconds of the answer; `0` tells every time |
| `silence-session`   |         | A session whose notifications are off, by its id; repeat it |
| `dock-badge`        | `true`  | Show how many sessions need you on the Dock icon |
| `dock-bounce`       | `false` | Bounce the Dock icon once when a session comes to need you |

## Agents

| Key              | Default | Description                                         |
| ---------------- | ------- | --------------------------------------------------- |
| `remote-control` | `true`  | Let `shibu do` and `shibu focus` drive the app        |
| `agent-hooks`    | `true`  | Let Claude Code's and Codex's hooks tell a pane's status as a turn starts, asks for permission and ends; `false` leaves it to their screens ([Status](./status.md#agents-that-report-their-turns)) |

## Keyboard

`keybind` rebinds an action, as `<keys>=<action>`. Repeat it for each binding. Use `unbind` as the action to free keys.

```ini
keybind = cmd+shift+d=pane.split.down
keybind = cmd+k=palette.open
keybind = cmd+b=unbind
```

Run `shibu actions` for every action id, and see [Keyboard Shortcuts](./shortcuts.md) for the defaults.
