---
title: Plugins
description: How a Shibu Code plugin describes itself, talks to the app and draws its widget.
---

# Plugins

A plugin is a program that draws a widget in a pane. The app starts it, sends
it what happened, and draws the parts it sends back. The app's own plugins use
the same contract, so this page is also the seed of the author guide.

## The manifest

A plugin ships a `kero-plugin.json` next to it:

```json
{
  "id": "example",
  "name": "Example",
  "version": "1.0.0",
  "description": "What it does.",
  "run": ["example-plugin"],
  "widgets": [
    { "name": "main", "title": "Example", "minWidth": 280, "minHeight": 150 }
  ],
  "commands": [{ "id": "refresh", "title": "Refresh Example" }],
  "permissions": ["secrets", "openURL"],
  "settings": [
    {
      "key": "apiKey", "label": "API Key", "kind": "text", "secret": true,
      "help": "The key of your Example account.",
      "link": "https://example.com/account/keys"
    },
    { "key": "project", "label": "Project", "kind": "text" }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `id` | The plugin's id: lowercase letters, digits and dashes, 2 to 41 characters, starting with a letter or digit. It names the plugin's code folder and its data folder. |
| `name`, `version`, `description` | What Settings › Plugins shows. |
| `run` | The argv that starts the plugin process. A built-in plugin's run is the app's own executable with `plugin-run <id>`. |
| `widgets` | Each widget has a `name` (unique), a `title` and a least size. |
| `commands` | Ids and titles the app sends with `command`. Ids are unique. Each command is an item of the command palette, titled as the manifest says, and the `kero do plugin.<id>.<command>` action, which `kero actions` lists. |
| `permissions` | `secrets` (read and write its own secrets), `notify` (post a notification) and `openURL` (open an http or https URL in the browser, or System Settings' Privacy & Security, `x-apple.systempreferences:com.apple.preference.security` with an optional `?` anchor; any other scheme is refused). A call without its permission is refused. |
| `settings` | Optional fields shown in Settings › Plugins: `key`, `label`, `kind` (currently `text`), `secret`, `help` (the line under the label) and `link` (an http or https page, opened by the field's **Open Link** button). The page draws only what the manifests declare. |

Built-in plugins are roles of the app's binary: `kero plugin-run <id>`. An
installed plugin's code lives in `plugins/<id>/` under the Shibu data folder
(`~/Library/Application Support/kero-local` unless `KERO_HOME` overrides it).
What the user gave it, its settings and secrets, lives apart, in
`plugin-data/<id>/`: an install that replaces the code keeps them.

## Commands

A command runs on a pane of its plugin: the focused pane of the session
shown, another pane of that session, then a pane of another session. A pane
that has not opened on the plugin's process yet opens for the command. With no
pane of the plugin anywhere, the command opens the plugin's first widget in
the session shown, and the plugin hears the command right after that pane's
`open`. A plugin that crashed waits for its **Restart** button: a command does
not start it.

## The pane and its target

A plugin pane's view is `plugin`. Its target is the JSON

```json
{ "plugin": "example", "widget": "main", "state": { "anything": "the plugin saved" } }
```

The plugin's `state.save` replaces the `state` through the pane's target, so
the pane comes back after a restart with the same plugin, widget and state.
The app does not add fields to the pane for a plugin.

## The protocol

The app speaks JSON-RPC 2.0 with the plugin over its stdin and stdout: one
message per line, with a 4 MB limit on a line. The protocol version is 1.

The app sends `hello {protocol, host}` first, and the plugin answers
`{protocol}`. The app refuses any other version. After that:

| The app sends | When |
| --- | --- |
| `open {pane, widget, state, size}` | A pane of this plugin opens. A plugin process serves every pane of its plugin. |
| `close {pane}` | The pane closes. The app stops the process five seconds after the last pane closes. |
| `resize {pane, size}` | The pane changed size. |
| `visible {pane, visible}` | The pane was shown or hidden. |
| `theme {dark, accent}` | The appearance or the accent changed. |
| `settings {values}` | The plugin starts or its settings change; secret values are never sent. |
| `event {pane, node, kind, value}` | A part was pressed, changed, committed, selected or submitted. |
| `command {id}` | The user ran one of the manifest's commands, from the palette or `kero do`. |

| The plugin sends | The app answers with |
| --- | --- |
| `render {pane, tree}` | `{}` once the tree is accepted. |
| `state.save {pane, state}` | `{}` once the target is saved. |
| `secret.get {key}` | `{"value": "..."}`, or an empty value. |
| `secret.set {key, value}` | `{}`. |
| `notify {title, body}` | `{}`, if the plugin has `notify`. |
| `openURL {url}` | `{}`, if the plugin has `openURL`. |

The plugin side of this is `internal/plugin/sdk`, which does the handshake,
runs the loop, and offers part builders such as
`sdk.Text(id, "Now playing").Role("title")`.

## The parts

A render sends a tree. The app validates it (known types, unique ids, known
color roles and icon names, a depth limit, a node limit) and draws it with the
app's palette, fonts and motion.

| Part | Fields | Event |
| --- | --- | --- |
| `row`, `column` | `gap`, `padding`, `align`, `grow`; children | — |
| `text` | `text`, `role` (title, body, caption, mono), `color` (text, muted, accent), `lines`, `marquee` | — |
| `image` | `url` (a URL or a `data:` URI), `width`, `height`, `radius`, `tint` | — |
| `button`, `iconButton` | `label` or `icon` (from the app's set), `primary`, `enabled` | `press` |
| `slider` | `value`, `min`, `max`, `step` | `change` while dragging, `commit` on release |
| `progress` | `value`, `color`, optional `rate` and `at` | — |
| `list` | `rows` of `image`, `title`, `subtitle`, `trailing` | `select` |
| `searchField` | `placeholder`, optional `text` (the query to show, such as one saved before a restart; the app puts it in the field when the field first shows and when the plugin sends another one, and ignores an echo of what the field last sent) | `change` after a short pause, `submit` |
| `emptyState` | `title`, `detail`, one action | the action's `press` |
| `spacer`, `divider` | — | — |

The app keeps focus and scroll with a part's id across renders. An event
returns the part's `id` (`node`). Images load off the UI goroutine and are
cached; a progress part with a `rate` (fraction of the work per second) and an
`at` (Unix milliseconds of the reading) moves itself between renders.

## Settings, secrets and logs

Settings › Plugins lists the built-in and installed plugins with their version
and permissions and draws each plugin's fields, each with its saved value. A
`secret` field is written to the plugin's `plugin-data/<id>/secrets.json` (file
`0600`; the folder is made, or closed, to `0700`); the other fields go to
`settings.json` in the same folder. The app sends only the non-secret
values in the `settings` message; the plugin reads its secrets with
`secret.get`, and it can read only its own.

A plugin's stderr goes to `logs/plugins/<id>.log` under the data folder,
rolled at 1 MB. A crash shows an error and a **Restart** button in the pane,
once; a plugin that stops reading its input is stopped after five seconds.

A plugin's `notify` shows a system notification. A click on it shows a pane
of the plugin, found at the click.

## Built-in plugins

Two plugins ship in the binary, and both use everything on this page:
`kero plugin-run <id>` starts them, and the app opens their widget like any
plugin pane.

| Plugin | Widget | Permissions | What it shows |
| --- | --- | --- | --- |
| `spotify` | `player` | `secrets`, `openURL` | The desktop player in a compact or a large layout: play, pause, skip, seek, volume, shuffle and repeat; albums and playlists from the Spotify Web API after a sign-in; a scrolling title and album-art tint. Set the Client ID in Settings › Plugins. |
| `usage` | `usage` | none | Each account's usage with a bar and a percent (or its balance), and the machine's CPU and RAM; the large layout adds the reset time, the plan and every window. It refreshes only while it is visible, at most every two minutes unless you press refresh. |

`openURL` opens the sign-in page, the Spotify developer dashboard and the
download page in the browser, and Privacy & Security › Automation in System
Settings;
`secrets` keeps the Spotify tokens in the plugin's `secrets.json`. Neither
plugin is installed from a folder: the app embeds their manifests and starts
them as roles of its own binary.

## Not here yet

Installing a plugin from a folder or a git URL, updating and removing it, the
public SDK, the plugin index, and the sidebar plugin slot are later rounds
(BRU-206, BRU-207, BRU-165). This page will grow with them.
