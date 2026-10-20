# Shibu Code changes and implementation

For release-by-release history see [Changelog](changelog.md). For daily use
and credential configuration see the [local edition guide](local-fork.md).

## Linear workspace

The completed v1 scope, setup and activation limits are recorded in
[Linear board and session links](linear-workspace.md). Secret guards and their
verification are documented in [Security checks](security.md). Home/composer
work remains separate. The installed app is not restarted by this feature's
test or build workflow.

The app shows a Linear workspace: the issues of the team, as a board or a
list, with the issue open beside them. The sidebar has a **Linear** row that
opens it, and `Cmd+Shift+L` does the same from anywhere.

The board's columns are the team's workflow states, in the order the team
puts them in — a state is data, not a constant, so a team that renames or
reorders its states is followed. The list is the same issues, one under
another. Both are narrowed by a search, a project, a milestone and a state;
the filters are kept between runs.

An issue opens beside the board with its description, its properties, its
relations and its comments. The description and the comments are rendered as
markdown, with a line break read as a break, as Linear's editor reads it.
The attachments are listed: a task prompt is a link to the file in the
repository.

**A session and the task it works on are one thing.** `kero link <session>
<BRU-25>` binds a session to an issue; the link is kept with the session
across restarts. A bound issue shows the session's name on its card, and its
detail shows the session, the agent's own session id — the one that resumes
it — with a button that copies it, and a button that opens the session. The
sidebar's Linear row counts the issues a session is working on.

The daemon reads the workspace, so the app and the CLI see the same one.
Linear's webhooks want a public address, which a desktop app does not have,
so the daemon polls: every thirty seconds it asks what changed, and every
five minutes it reads the projects and the milestones whole. A request is
bounded at two seconds, and a workspace that cannot be read is a state the
app shows, never a failure the daemon dies of. The last good read is kept.

The key is a Linear personal API key, kept at mode `0600` in the personal
data directory, and read from `LINEAR_API_KEY` when that is set. It is read
per request, so replacing it takes effect at once, which is what rotating a
key is. Set it in Settings › Agents, or with `kero linear auth <key>`.

Code: [`linear_view.go`](../linear_view.go),
[`internal/linear/`](../internal/linear/),
[`internal/daemon/linear.go`](../internal/daemon/linear.go).
Automation: `kero linear status|board|issue|set-state|comment`, `kero link`.

## Panes that show anything

A pane is a terminal or a view: **Tasks** (the project's Linear issues, one
per session), **Files** (the folder as a tree, with files read beside it) or
**Browser** (a web page in the system's web view, most often the dev
server). `Cmd+Alt+N` or the pane header's **+** opens a chooser; `T`, `L`,
`E` and `B` pick, and Shift with any of them opens the pane below rather than
beside. `Cmd+Alt+T/L/E/B` open each directly, and **Show in This Pane**
turns an existing pane into another kind. What a view shows is the pane's
Target, which kerod keeps, so it opens again after a restart. Agents open
views with `kero split --browser 5173`, `--files --open docs/plan.md` or
`--tasks`.

- **Files**: folders are read off the main thread and refreshed with git's
  changes every few seconds. Markdown renders, and links to its headings or
  to other files of the folder open in the pane. The header's Find box
  (`Cmd+F` in the pane) matches any file of the project loosely. A row
  dragged to a terminal types its path. Unfolded folders are kept across
  restarts.
- **Browser**: a Safari user agent, no permissions granted, web addresses
  only. In a focused Browser pane, `Cmd+L` selects the address, `Cmd+=`,
  `Cmd+-` and `Cmd+0` zoom the page, and `Cmd+R` reloads it rather than
  renaming the session.
- **Layout**: panes keep at least 280×150 points, and a split without room
  is refused. Double-clicking a divider, or `Cmd+Ctrl+=`, makes the panes
  even. New panes grow in and closing ones give their room back; terminal
  sizes wait until the glide ends, so a program redraws once.

Code: [`pane_chooser.go`](../pane_chooser.go),
[`pane_views.go`](../pane_views.go), [`files_pane.go`](../files_pane.go),
[`browser_pane.go`](../browser_pane.go), [`tasks_pane.go`](../tasks_pane.go),
[`panes.go`](../panes.go).

## Agent-reported status

Claude Code and Codex tell a pane's status from their own hooks, so it turns
working, waiting or done the moment the agent does, and the screen
detection (`internal/detect`) stays as the fallback.

`kero skill setup` installs the one command, `'…/kero' hook claude` (or
`codex`), for each event: Claude Code's `SessionStart`, `UserPromptSubmit`,
`Notification`, `PostToolUse`, `Stop`, `StopFailure` and `SessionEnd`, and
Codex's `SessionStart`, `UserPromptSubmit`, `PermissionRequest`,
`PostToolUse` and `Stop`. The hook reads the event from its input.
`SessionStart` does what it did; an install of `SessionStart` alone reads as
outdated, and setup adds the other events without moving or changing it,
since Codex trusts a hook by its place in `hooks.json`. The new events have
a 2–3 second timeout, and the hook gives kerod one second.

Every other event maps to a state (`eventReport` in
[`internal/cli/hook_events.go`](../internal/cli/hook_events.go)):
`UserPromptSubmit` and `PostToolUse` to working; a permission or elicitation
`Notification`, or Codex's `PermissionRequest`, to blocked with the agent's
words; `Stop` to done; `StopFailure` to error; `SessionEnd` to clear (not
done: it also fires on `/clear`). Subagents' events, and `Stop`-like events
with an `agent_id`, report nothing. Nothing goes to stdout: Claude Code
would take it into its context. A hook without `$KERO_PANE`, or of a Codex
whose `CODEX_THREAD_ID` isn't the event's session, does nothing, and so
does one with `agent-hooks = false`, which it reads as it runs.

The hook doesn't write OSC 7501 to `/dev/tty`: a hook has no terminal, and
Codex's app server, which panes share, runs hooks for panes other than its
own. It calls kerod's `report` method instead (`proto.ReportParams`), which
an older kerod refuses, so the hook does nothing until kerod restarts; the
protocol version stays 3. kerod gives the report to the pane whose agent's
session, as `SessionStart` told it, is the event's, that of `$KERO_PANE`
first; a nested `claude -p`, of a session of its own, reaches none
(`hookReport` in [`internal/daemon/hooks.go`](../internal/daemon/hooks.go)).

The report is a record of the agent's own id (`claude`, `codex`), beside
those other programs report, with `app` the agent. It is soft: it sets the
status at once but doesn't switch detection off as a program's own report
does (`declared`). Claude Code fires no `Stop` when you press Esc, so once
the screen shows the prompt for `idleHold` the pane goes idle and the
record goes; a screen that shows the agent work again ends a stale wait, as
after a granted permission (`overrule`). Claude Code also writes its own
OSC 7501 reports, a root record of app `claude-code`: what it reports after
its hooks stands, so its working ends a wait they told and its idle a turn
they began. Messages are cut to one line of
2 KB, without controls. Nothing of it is saved: records never were.

Known limit: a nested `claude -p` (or Codex) whose own `SessionStart` runs
Kero's hook in an agent's pane still points the pane at its session, as
before. From then on its events are the pane's and the outer agent's reach
no pane: the nested run's `Stop` can mark the pane done mid-turn, and
notify, unless the outer agent reports for itself (Claude Code's own OSC
7501). Only a nested run that fires no `SessionStart` reports nothing.
Fixing it belongs in `SessionStart`, which this change leaves as it was.

Code: [`internal/cli/hook_events.go`](../internal/cli/hook_events.go),
[`internal/cli/hook.go`](../internal/cli/hook.go) (install),
[`internal/daemon/hooks.go`](../internal/daemon/hooks.go),
[`internal/daemon/session.go`](../internal/daemon/session.go) (`look`).
Tests: `hook_events_test.go`, `internal/daemon/hooks_test.go`.

## Account usage

Claude and Codex use existing CLI login credentials. OpenCode Go uses its
workspace key; DeepSeek shows direct API balance. These are provider-specific
allowances and balances, not a combined subscription budget. Reads run in
the background every two minutes, preserve the last successful observation
after errors, and respect rate-limit backoff. Explicit provider-key overrides
are atomically stored with mode `0600` in the personal data directory.

The sidebar opens an in-window modal with allowance windows, reset times,
refresh and provider configuration. Cmd+Shift+U opens it; Cmd+R refreshes
while open; Esc/Cmd+W closes it. There is no extra tab or window.

Code: [`usage.go`](../usage.go), [`internal/usage/`](../internal/usage/).
Automation: `kero usage --json` and `kero do usage.open`.

## Settings in the window

Settings opens over the main window as a modal, as the palette and the usage
modal do, rather than in a window of its own (BRU-158). The card is at most
900 by 640 and keeps 24 points from the window's edges, over the window
dimmed by the palette's overlay color. It is painted with the window's own
wash, opaque, with the page on its raised card, in light and dark alike.
Under 700 points wide the sidebar shows the pages' icons alone, their names
as tooltips, and the search moves into the page's header.

`Cmd+,` (`settings.open`) opens it, and closes it while it shows; the
palette's **Settings: <page>** items open it on their page. Esc closes it,
after clearing a search or ending a shortcut being recorded, as does a click
on the backdrop, the header's close button and `Cmd+W` or Close Session,
which close Settings rather than the pane under it. While it shows, Find
goes to its search, and the palette, the New Pane chooser, the usage modal
and Rename wait. A confirmation it asks for, as for restarting the service,
shows over it and goes back to it. Closing the main window ends Settings
with it, letting go of what it holds, so a window shown again starts without
it.

The settings' state stays in `settingsState` (`a.ss`), with `open` for the
modal. The system's, the updater's and kerod's answers are asked as it opens
in the running app only, which keeps the tests from reaching the daily
kerod.

Code: [`settings_view.go`](../settings_view.go) (`openSettings`,
`settingsModal`, `closeSettings`), mounted in [`view.go`](../view.go); the
actions in [`actions.go`](../actions.go). Tests:
[`settings_modal_test.go`](../settings_modal_test.go).

## CPU and memory

Two equal-width sidebar meters show CPU busy percentage and RAM used. They
open details in the usage modal. Bars are clamped to 0–100%, turn amber at
90%, and stay empty when a measurement is unavailable.

macOS Mach calls sample every two seconds in the background without shell
processes. CPU covers all cores; RAM includes active, wired and compressed
physical memory, excluding reclaimable cache. These are whole-Mac readings,
not Shibu process usage or memory pressure.

Code: [`resources.go`](../resources.go),
[`internal/resources/`](../internal/resources/).
Automation: `kero resources --json`.

## Terminal responsiveness and attachments

The local build targets the host architecture: Apple Silicon is `arm64`;
Intel/AMD is `amd64`. Continuous status animation was removed. The daemon's
synchronized-redraw scheduling now advances completed frames past an older
50ms retry, with a fake-clock regression test. The measured isolated transport
median improved from 55.93ms to 16.90ms (15 samples per build). GPU rendering,
OS input delivery and full typing latency were not established by that probe.

Finder files paste as quoted absolute paths without submitting a command.
Clipboard screenshots are saved as private PNG files and inserted as paths;
native file/image drops use the same attachment flow. Text paste retains
normal terminal behavior. The receiving agent must support local files.
Attachment files persist until removed; remote shells require a separate
upload workflow. Native paste/drop still needs an interactive user trial.

Code: [`attachments.go`](../attachments.go),
[`internal/daemon/session.go`](../internal/daemon/session.go),
[`internal/daemon/frame_test.go`](../internal/daemon/frame_test.go).
Probe: [`scripts/measure-terminal-latency.py`](../scripts/measure-terminal-latency.py).
See [Herdr comparison](herdr-porting.md) for architecture and future candidates;
those candidate ports are not implemented.

## Title bar and traffic lights

Shibu follows Apple's title bar on macOS rather than drawing its own:

- MyGo's inset title bar style (`TitleBarHiddenInset`, in
  [`platform.go`](../platform.go)) gives the window an empty `NSToolbar`
  with the automatic toolbar style, which AppKit lays out as the unified
  title bar and toolbar. Measured on macOS 27.0.1: the bar is 52 points,
  the buttons 14 points, 23 apart, the close button 19 from the left, and
  their middle 26 from the top. (The window's content layout rect starts
  lower, 66 while the window is key, but the lights center on 26.)
- Shibu no longer moves the lights (`TrafficLightPosition` is gone): they
  stay where AppKit puts them, and the app lines up with them. `topBar` is
  52 and `barMid` 26 ([`view.go`](../view.go)), so the sidebar's buttons
  and the cards' headers, 36 tall from `cardGap` down, center on the
  lights' line. Windows and Linux are unchanged (`ownControls`).
- In full screen macOS hides the lights and brings them down with the menu
  bar as the pointer reaches the top of the screen, as in Finder, Safari or
  Xcode; Shibu leaves that alone. MyGo reports an empty title bar there,
  which put the sidebar's buttons at the window's edge, under the lights
  as they come down. `titleBar` ([`title_bar.go`](../title_bar.go)) keeps
  the bar as it last was in the window instead, so nothing moves going
  full screen, during the animation too. A window that opens in full
  screen keeps room where AppKit puts the lights (`systemBar`).

An earlier version of this fix (`f777a8c`) drew Shibu's own lights in full
screen and a 48-point bar; it was dropped for the system's own controls and
behaviour.

Tests: [`title_bar_test.go`](../title_bar_test.go) (the unified bar's
height, the sidebar's buttons centered with the lights and unmoved in full
screen, with the sidebar shown and hidden, and a window that opens in full
screen).

## Identity and icon

- App name: **Shibu Code** (previously **Kero Local**).
- Bundle identifier: `dev.ddeyalla.kero`.
- Icon source: [`resources/shibu-icon.png`](../resources/shibu-icon.png),
  supplied `Desktop/Shibu Code.png` golden retriever terminal artwork, packaged into native ICNS.
- CLI/protocol names stay `kero`; data identity stays `kero-local`.
- Upstream binary auto-updates are disabled; source changes can still be merged.

Code: [`mygo.json`](../mygo.json),
[`internal/proto/proto.go`](../internal/proto/proto.go),
[`internal/proto/local_test.go`](../internal/proto/local_test.go).

## Updating the personal edition

Shibu consumes stable Kero releases through its own signed build pipeline.
Use the sidebar's circular-arrow button to check Kero now; conflicts/failures
can open an isolated background Codex repair session, with review before release.
See [Automatic updates](automatic-updates.md) for credentials, controls, tests
and the fork-only publishing boundary. No automatic window or daemon restart
is enabled.

## Sessions and data continuity

The official and personal editions have separate daemons and data:

| Data | Original Kero | Shibu Code |
| --- | --- | --- |
| macOS application | `/Applications/Kero.app` | `~/Applications/Shibu Code.app` |
| App data | `~/Library/Application Support/kero` | `~/Library/Application Support/kero-local` |
| Settings | `~/.config/kero/config` | `~/.config/kero-local/config` |

`KERO_HOME`, `KERO_CONFIG` and `KERO_SOCKET` can override defaults; development
builds use `kero-local-dev`. Keep those overrides consistent and never point
two independent daemons at the same mutable state directory.

On 2026-10-20, the requested one-time import copied three sessions / five panes
into Shibu, retaining its existing session for four sessions / six panes total.
Shibu's shells restarted and copied Claude/Codex panes resumed. Original Kero's
shell PIDs remained unchanged. Running shell processes cannot be copied; saved
state/history and agent resume IDs were copied instead.

The pre-import backup is local at
`~/Library/Application Support/kero-local/backups/before-kero-import-20261020-194341`.
This is not an automatic importer or a promise of ongoing synchronization.
Session contents, credentials and backups do not belong in the repository.
