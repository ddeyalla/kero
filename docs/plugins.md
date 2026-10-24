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
    { "name": "main", "title": "Example", "icon": "pin", "minWidth": 280, "minHeight": 150 }
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
| `widgets` | Each widget has a `name` (unique), a `title`, a least size, and optionally an `icon` (a name from the icon set below), which the sidebar draws before the plugin's name when the widget is pinned. |
| `commands` | Ids and titles the app sends with `command`. Ids are unique. Each command is an item of the command palette, titled as the manifest says, and the `kero do plugin.<id>.<command>` action, which `kero actions` lists. |
| `permissions` | `secrets` (read and write its own secrets), `notify` (post a notification) and `openURL` (open an http or https URL in the browser, or System Settings' Privacy & Security, `x-apple.systempreferences:com.apple.preference.security` with an optional `?` anchor; any other scheme is refused), `web` (run a page of this machine in a web view with no window; see `web.open`), `ambient` (lend the pane's tint to the app's colors; see below), `focus` (be told which session the app has selected; see the protocol) and `clipboard` (put text the user chose to copy on the clipboard; a plugin cannot read it). A call without its permission is refused. |
| `settings` | Optional fields shown in Settings › Plugins: `key`, `label`, `kind` (`text`, or `action`: a button whose `key` is the id of one of the plugin's commands, which it runs; it holds no value, so it is neither `secret` nor linked), `secret`, `help` (the line under the label) and `link` (an http or https page, opened by the field's **Open Link** button). The page draws only what the manifests declare. |

Built-in plugins are roles of the app's binary: `kero plugin-run <id>`. An
installed plugin's code lives in `plugins/<id>/` under the Shibu data folder
(`~/Library/Application Support/kero-local` unless `KERO_HOME` overrides it).
What the user gave it, its settings and secrets, lives apart, in
`plugin-data/<id>/`: an install that replaces the code keeps them.

## Commands

Spotify's failed track and Up next lists offer **Retry** in place. A failed
read keeps the open album or playlist and any cached tracks. A play that a
newer press has superseded completes silently, including a late page error
that names an older play request; only the current command reports a failure.
A press still waiting for a cold engine is canceled by a newer press before
the engine takes it, while an accepted command still runs. Two clicks on one
row inside the engine's window are one accepted play whose request owns the
page error, and a retry after a current failure is sent rather than dropped.
The page compares the accepted request, so a later accepted play is not
treated as the earlier double click. An offline library also offers Retry,
without changing the selected tab. These paths live in
`internal/plugin/builtin/spotify/drill.go`, `view.go` and `spotify.go`.

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
| `focus {session}` | Only to a plugin with the `focus` permission: the id of the session the app has selected (empty for none), once when the plugin is ready and whenever the selection changes. The SDK has `Handlers.Focus`. |

| The plugin sends | The app answers with |
| --- | --- |
| `render {pane, tree}` | `{}` once the tree is accepted. |
| `state.save {pane, state}` | `{}` once the target is saved. |
| `secret.get {key}` | `{"value": "..."}`, or an empty value. |
| `secret.set {key, value}` | `{}`. |
| `notify {title, body}` | `{}`, if the plugin has `notify`. |
| `openURL {url}` | `{}`, if the plugin has `openURL`. |
| `clipboard.write {text}` | `{}`, if the plugin has `clipboard`. Up to 64 KB; the plugin cannot read the clipboard. Taken only within about a second of an event or command the host delivered (`userAt` in the host); a later write is refused and logged. The SDK has `Conn.Copy(text)`. |
| `web.open {id, url}` | `{}`, if the plugin has `web`. The app loads the page in a web view with no window, which plays audio but draws nothing. The URL must be `http` on a loopback address with a port (`127.0.0.1`, `localhost` or `::1`), so the page is one the plugin serves itself. The `id` is lowercase letters, digits and `-`; a plugin has at most two, and opening an id again loads the new URL. |
| `web.close {id}` | `{}`. The app also closes every web view of a plugin when its process ends. |

The plugin side of this is `internal/plugin/sdk`, which does the handshake,
runs the loop, and offers part builders such as
`sdk.Text(id, "Now playing").Role("title")`.

## The parts

A render sends a tree. The app validates it (known types, unique ids, known
color roles and icon names, a depth limit, a node limit) and draws it with the
app's palette, fonts and motion.

| Part | Fields | Event |
| --- | --- | --- |
| `row`, `column` | `gap`, `padding`, `align`, `grow`, `surface: "card"` (a soft panel with rounded corners, `radius` 10 unless set); children | — |
| `text` | `text`, `role` (headline, title, body, caption, mono, strong: body size in a heavier weight), `color` (text, muted, accent, danger, warning, success, orange, green, teal, blue, purple, gray), `lines`, `marquee`, `icon` or `mark` (a glyph or a program logo before the words), optional `label` (the spoken name of an icon-only text), optional `rate` and `at` (a clock that counts on its own) | — |
| `image` | `url` (a URL or a `data:` URI), `width`, `height`, `radius`, `tint` | — |
| `button`, `iconButton` | `label` or `icon` (from the app's set), `primary`, `enabled`; an `iconButton` with a `width` (up to 96) is a round button of that size, and a primary one is a filled disc | `press` |
| `slider` | `value`, `min`, `max`, `step`, optional `rate` and `at` (the host moves it while playing); a horizontal slider with a `width` keeps it, one without grows | `change` while dragging, `commit` once on release, even if the pointer left the track |
| `progress` | `value`, `color`, optional `marker` (a tick at a fraction of the bar, 0 to 1, such as where usage should be by now), optional `rate` and `at` | — |
| `segmented` | `items` (2 to 8 choices), `active` (the chosen one) | `select` |
| `donut` | `segments` (up to 32, each with `id`, `label`, `value`, `color`), `width` (the diameter, 48 to 240 points), `text` (in the middle), `detail` (the caption under it) | — |
| `list` | `rows` of `image`, `title`, `subtitle`, `trailing`, `leading`, `active` (the row is highlighted); `variant: "grid"` draws the rows as square tiles `width` wide, which wrap with 12 points between them | `select` |
| `searchField` | `placeholder`, optional `text` (the query to show, such as one saved before a restart; the app puts it in the field when the field first shows and when the plugin sends another one, and ignores an echo of what the field last sent) | `change` after a short pause, `submit` |
| `emptyState` | `title`, `detail`, one action | the action's `press` |
| `menu` | `label`, optional `icon`, `items` (up to 200; an item with `active` is checked, and several may be) | `select` with the item's id |
| `markdown` | `text`, GitHub-flavoured Markdown up to 64 KB, in the app's own look (headings, lists, code, tables, links; an image shows as its description). A link opens in the browser if the plugin has `openURL`, and otherwise sends `press` with the URL as the value | `press` (links without `openURL`) |
| `spacer`, `divider` | — | — |

A `row` or `column` may be `pressable`: it draws hover and pressed states,
takes the focus, is named for assistive technology by its `label`, and sends
`press` with its `id`. A pressable part may have a `contextMenu` (entries with
an id and a title, separators, and one level of submenu), which a right click
opens and which sends `context` with the entry's id as the value. A `row` may
set `scrollX` to scroll sideways with its children keeping their widths, such
as the columns of a board, and a `column` may set `scrollY` to scroll on its
own. A `text` may be a `chip` (its words on a soft fill, in a `#RRGGBB` color
or `neutral`) or set `hex` to color it and its icon. A tree may have a
`header` (a part fixed above the scrolling page; it needs `scroll`) and `keys`
(up to 8 of `j`, `k`, `up`, `down`, `left`, `right`, `enter`, `escape`, heard
while a pane of the plugin has the focus and no field takes the typing; each
sends `key`). In the SDK, `(*Node).ScrollingTreeUnder(header)` builds a scrolling
tree with a `Header`, `Tree.Keys` holds the keys, and `Handlers.Event` gets them as
events of the kind `key` (`protocol.KindKey`; the value is the key's name). A
context-menu choice is the kind `context` (`protocol.KindContext`). Build the
menu with `(*Node).ContextMenu(...)`, `sdk.MenuItem`, `sdk.Separator` and
`sdk.Submenu`; style with `(*Node).Pressable`, `ScrollX`, `ScrollY`, `Chip`
(`protocol.ChipNeutral` for the quiet fill) and `Hex`.

The fields `surface`, `marker`, `mark` and `segments` are valid only on their own node types (row and column, progress, text and donut); a tree that sets one elsewhere is refused. Plugin text is drawn with tabular digits.

A tree may set `scroll` (`ScrollingTree()` in the SDK). The whole pane then
scrolls as one page: the root takes its natural height, and a `list` draws at
full height instead of scrolling by itself. Use it when a pane holds lists
that would otherwise each scroll inside a fixed frame.

An icon button draws a real SF Symbol on macOS as a template, tinted when it is
drawn, so there is no flash of a fallback. Any node may also set `active` (the
part is shown as on, such as shuffle). The SDK sends a tree only when it differs
from the last one it sent for that pane. The mapping:

| Icon | Symbol | Icon | Symbol |
| --- | --- | --- | --- |
| `play` | `play.fill` | `search` | `magnifyingglass` |
| `pause` | `pause.fill` | `close` | `xmark` |
| `next` | `forward.fill` | `refresh` | `arrow.clockwise` |
| `previous` | `backward.fill` | `plus` | `plus` |
| `shuffle` | `shuffle` | `gear` | `gearshape` |
| `repeat` | `repeat` | `external` | `arrow.up.right.square` |
| `volume` | `speaker.wave.2.fill` | `volume-off` | `speaker.slash.fill` |
| `copy` | `doc.on.doc` | `flame` | `flame.fill` |
| `dot` | `circle.fill` | `warning` | `exclamationmark.triangle.fill` |
| `info` | `info.circle` | `circle` | `circle` |
| `circle-dashed` | `circle.dashed` | `circle-half` | `circle.lefthalf.filled` |
| `check-circle` | `checkmark.circle.fill` | `xmark-circle` | `xmark.circle.fill` |
| `filter` | `line.3.horizontal.decrease.circle` | `person` | `person.crop.circle` |
| `tag` | `tag` | `flag` | `flag` |
| `pin` | `pin` | `priority-urgent` | `exclamationmark.square.fill` |
| `milestone` | `diamond` | `blocked` | `nosign` |
| `blocking` | `arrow.turn.down.right` | `subissue` | `list.bullet.indent` |

The table lists a part of the set; `plugin_symbol.go` has all of it.

A plugin with the `ambient` permission lends its pane's tint (an image part's
`tint`) to the whole app: the accent, the focus edge, the terminals' cursor and
selection, and a soft wash of the card. The app caps the colour to 3:1
contrast. The user turns it off in Settings › Appearance › **Colour from
music** (`ambient-color`, on by default). A plugin lends a colour only while it
wants the app tinted; the Spotify pane does so only while a track plays.

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

Three plugins ship in the binary, and all use everything on this page:
`kero plugin-run <id>` starts them, and the app opens their widget like any
plugin pane.

| Plugin | Widget | Permissions | What it shows |
| --- | --- | --- | --- |
| `spotify` | `player` | `secrets`, `openURL`, `web`, `ambient` | A Spotify player that plays in the app, with no Spotify app open (Premium). A compact player (two rows from 440 points wide) over the library as a grid of covers; a playlist or album opens its track list, and an Up next tab shows the queue. Its album colour tints the app while a track plays. Set the Client ID in Settings › Plugins; **Sign Out** is next to it. |
| `linear` | `board` | `openURL`, `focus`, `clipboard` | The issues of the team as a list or a board, in the sections Current, Done, Todo and Backlog, with filters and a full issue detail. It is a client of the daemon, which holds the Linear key; see [Linear plugin](linear-workspace.md). Pin it to the sidebar with `kero plugin pin linear`. |
| `usage` | `usage` | none | A Cost card (total and provider list over Today, Yesterday or 30 Days, from local logs) and a section per provider with its mark, name, plan and each window's bar, pace tick, "% left", reset time and a "Limit in …" flame; a chevron opens Today and Last 30 Days in dollars and tokens. Small panes get a compact form. Accounts refresh only while it is visible, at most every two minutes unless you press refresh; cost reads every 5 minutes. |

`openURL` opens the sign-in page, the Spotify developer dashboard and the
download page in the browser, and Privacy & Security › Automation in System
Settings;
`secrets` keeps the Spotify tokens in the plugin's `secrets.json`; `web`
runs the page of Spotify's Web Playback SDK that plays the music; `ambient`
lends the album colour to the app. Neither
plugin is installed from a folder: the app embeds their manifests and starts
them as roles of its own binary.

## Pins

A widget can have a row in the sidebar: a pin. The app keeps the pins in its
preferences (`pinned`, a list of plugin and widget), and the row shows the
plugin's name and the widget's `icon`. A click focuses a pane of that widget in
the selected session, or splits one in beside the terminals. Pin and unpin from
the pane header's menu, Settings › Plugins, the command palette (**Pin X to
Sidebar**), or

```sh
kero plugin pin <plugin> [widget]
kero plugin unpin <plugin> [widget]
```

A plugin does not pin itself. A widget without `icon` has a row with no mark.

## Not here yet

Installing a plugin from a folder or a git URL, updating and removing it, the
public SDK, and the plugin index are later rounds (BRU-206, BRU-207, BRU-165).
This page will grow with them. A built-in plugin may use the daemon's socket
as the Linear plugin does. A third-party plugin is not given it, but that is a
policy and not a boundary: a plugin is a native executable with the user's
rights, and until plugins run in a sandbox it can open the socket (see
[Security checks](security.md)).
