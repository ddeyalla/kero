# Linear plugin and session links

Linear is a built-in plugin, not a screen of the app. The plugin draws the
issues of the team in a pane, as a list or a board, beside terminals and
agents. The daemon still reads Linear and keeps the key; the plugin is a client
of the daemon that shows what the daemon holds. This page covers the plugin,
session links, the key, and the limits. The plugin contract is in
[Plugins](plugins.md).

## Open it

- **Sidebar pin.** The Linear row of the sidebar is a pin of the plugin. It
  was the old Linear row, and an install that had the row on keeps a pin for
  it. Click it to show the plugin beside the terminals of the selected session:
  if that session has a Linear pane the click focuses it, else it splits a new
  one in. Right-click the row to unpin it.
- **Pin or unpin** from the pane header's menu, Settings › Plugins, the command
  palette (**Pin Linear to Sidebar**), or `kero plugin pin linear` and
  `kero plugin unpin linear`.
- **New Pane chooser.** The **Tasks** entry (`L`, `Cmd+Alt+L`) opens the same
  plugin pane, so it stays reachable without a pin.
- **From agents and scripts.** `kero split --tasks` opens it beside the
  terminal. An older client or script that asks the daemon for the view
  `tasks` gets the same pane.

## Use it

The pane has two views, **List** and **Board**, and keeps its own state (view,
filters, search, folds, the issue open) in its target, so it comes back after a
restart.

- **Sections**, in this order: **Current** (started), **Done** (completed; the
  20 most recent, then **Show more**), **Todo** (unstarted) and **Backlog**
  (backlog and triage). Canceled issues are hidden; a filter shows them.
- **Folds.** Each section folds. Inside a section the issues group by project,
  and an issue with sub-issues folds over them. Folds are kept per pane.
- **Current follows the focused session.** The issue linked to the session you
  are in comes first, then In Progress issues that have a live linked session,
  then the rest assigned to you, by last update.
- **Filters:** Assignee (Me, Anyone or a person), Project, Label, Priority, a
  text search over the identifier and title, and a toggle for canceled issues.
  Scope is **All** or the **Project** of the pane's session (the Linear project
  its Shibu project is linked to; with none, the pane offers **Show all** or
  says to link one in the project's menu).
- **Board.** The four sections are columns that scroll sideways; cards press
  to open the issue. A column shows about 250 cards, then "N more".
- **Issue detail** has the full description (Markdown), properties, a status
  picker, the sessions that worked on it with **Resume**, sub-issues, relations
  (blocking and blocked by), attachments and the activity feed. Web links open
  in the browser. Remote issue content cannot open local files or custom
  command URL schemes; Markdown images do not fetch automatically.
- **Settings** of the plugin, in Settings › Plugins: empty board columns, and
  density (comfortable or compact). The refresh interval and the key stay on
  the Linear page of Settings, because the daemon reads Linear.

Choose a session in the issue detail and click **Link session**. The detail
can open that session, copy its resume ID when known, or unlink it. This
changes local Shibu metadata; it does not change the issue in Linear. Explicit
links survive daemon restarts alongside the agent resume metadata.

Agents can report their task without deriving it from the branch:

```sh
kero link <session> TEAM-123
kero report working "Checking the change" --id TEAM-123 --app linear --progress 40
kero link <session> # remove the explicit link
```

OSC reports are live associations. Clearing a report removes that association;
removing an explicit link does not clear a separate report still emitted by an
agent. An issue's badge shows the linked session and reported progress.

## What changed from the built-in screen

- The Linear screen, its sidebar row, the `Cmd+Shift+L` shortcut and the
  **Tasks** pane of the app are gone. A saved state that was showing the screen
  opens the selected session.
- Saved Tasks panes become Linear plugin panes once, when the daemon reads
  state that is older than version 3, as a list of the session's project with
  finished issues hidden. Panes with other views are untouched.
- Settings for the sidebar row, empty columns and card density left the Linear
  page. The old `linear-sidebar` setting is read once to decide whether to pin
  the plugin; `linear-empty-columns` and `linear-card-density` are still read
  from the configuration file but no longer have rows in Settings.

## Connect and rotate a key

In Settings › Linear, enter a Linear personal API key in the masked field,
save it, or use **Remove Linear key**. The input clears after saving and when
Settings closes. The key stays in `credentials.json` under the
personal app data directory, written atomically with file mode `0600`.
It is absent from preferences, session state and source control.

`LINEAR_API_KEY` overrides the saved key; `LINEAR_TEAM` selects the team by key
or ID. Otherwise Shibu reads the first accessible team. Removing a saved key
does not remove an environment override. Keys are reloaded for each operation.

CLI authentication accepts stdin rather than a secret command argument:

```sh
kero linear auth --stdin # pipe from a password manager; do not paste into shell history
kero linear auth --remove
kero linear status
kero linear board --json
kero linear issue TEAM-123 --json
```

A key previously pasted into chat should be rotated in Linear and replaced
locally. Secret scanning cannot revoke a credential or erase a transcript.

## Reliability and limits

- Polls run in the daemon, outside its state mutex: deltas every 30 seconds,
  full refreshes every five minutes. Manual refreshes are coalesced.
- Full reads paginate issues, projects and milestones. Issue details paginate
  comments, attachments, relations and children. Fractional workflow positions
  and inverse dependency relations are handled.
- Delta watermarks precede requests, preventing edits during a request from
  falling into a gap. A full refresh also removes deleted/archived issues.
- Offline/error states retain the last board in private session state. Rate
  limits respect reset headers; authentication failures back off. Requests have
  deadlines. No failure restarts the daemon or closes a terminal.
- An open issue refreshes after ten seconds following a successful read. Failed
  reads stop that loop and show Retry. Late responses cannot replace a different
  issue or reopen a closed detail pane.
- The plugin draws again only when the data it shows changes and only while its
  pane is visible; the daemon's own saves of the pane target do not redraw it.
  A tree has at most 2000 parts: folded groups send nothing, and a board shows
  about 250 cards.
- Board cards summarize up to ten relations in each direction. Open detail for
  the complete dependency list.
- The existing explicit CLI `set-state` and `comment` commands perform one
  request. They are not automatically queued/replayed after an uncertain result.
  No real Linear mutation is sent during verification.

The plugin adds fields to the daemon protocol (an issue's assignee, priority
and parent, and the viewer) and moves saved state to version 3. Activating it
requires one planned restart of the terminal service; restarting shells is not
a seamless live handoff. Build/test success alone never authorizes ending the
user's active work. A build from before this change can read a migrated state
file but cannot open the migrated panes as Tasks: it shows plugin panes it may
not have. Do not roll back after the daemon saved state at version 3.

The plugin never reads or logs the key. It is a client of the daemon: it
subscribes to state, and asks the daemon to set a state, comment, load an
issue or link a session. The key stays in the daemon.

## Verification

Verification includes the full Go suite, MyGo vet, race checks for the client
and daemon, real read-only workspace queries, native preview inspection, and
secret-guard fixtures. See [Security checks](security.md) for the local hooks,
GitHub protection and their limits. The native preview uses its own bundle ID,
data directory and socket, with automatic app updates disabled.

API behavior follows Linear's [pagination](https://linear.app/developers/pagination)
and [rate-limit](https://linear.app/developers/rate-limiting) documentation.
