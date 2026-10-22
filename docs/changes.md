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
per session), **Files** (the folder as a tree, with files read beside it),
**Browser** (a web page in the system's web view, most often the dev
server) or **Attention** (every session that needs you, one list).
`Cmd+Alt+N` or the pane header's **+** opens a chooser; `T`, `L`,
`E`, `B` and `A` pick, and Shift with any of them opens the pane below rather than
beside. `Cmd+Alt+T/L/E/B/A` open each directly, and **Show in This Pane**
turns an existing pane into another kind. What a view shows is the pane's
Target, which kerod keeps, so it opens again after a restart. Agents open
views with `kero split --browser 5173`, `--files --open docs/plan.md`,
`--tasks` or `--attention`.

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
- **Attention**: the sessions that need you, from the state kerod folds
  (below), with the pane each came from; a row opens that pane.
- **Layout**: panes keep at least 280×150 points, and a split without room
  is refused. Double-clicking a divider, or `Cmd+Ctrl+=`, makes the panes
  even. New panes grow in and closing ones give their room back; terminal
  sizes wait until the glide ends, so a program redraws once.

Code: [`pane_chooser.go`](../pane_chooser.go),
[`pane_views.go`](../pane_views.go), [`files_pane.go`](../files_pane.go),
[`browser_pane.go`](../browser_pane.go), [`tasks_pane.go`](../tasks_pane.go),
[`attention_pane.go`](../attention_pane.go), [`panes.go`](../panes.go).

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

## Attention in kerod

Each session has one attention state — `running`, `waiting-user`, `unread`,
`done` or `idle` — folded by kerod from its panes: the most urgent of them,
one line of what it says, the pane it came from and the age of it. It rides
on the state the app already receives (`Session.Attention`,
[`internal/proto/proto.go`](../internal/proto/proto.go)), so the attention
pane and the notifications read one truth instead of each folding the panes
its own way. The fold is in
[`internal/daemon/attention.go`](../internal/daemon/attention.go)
(`sessionAttention`, filled by `sessionInfo`).

A pane's own word comes from its marks, in this order: the agent's hook
report, then the program's OSC 7501, then the transcript scan, then the
screen detection. The first three are records on the pane (`records`,
`soft`); the fold reads the resulting status, so only the most urgent
pane's words show, and of two as urgent, the first pane's.

The transcript scan is the fallback for what no other signal says.
Claude Code and Codex keep their conversations on disk, and the daemon
reads the end of the file (the last 256 KB, whole lines only) every few
tick loops, and once at start: a call that ran for 45 seconds with no
result is taken for one waiting on the user, one that just started for
work, a turn's end for done. It is local-only and cheap, and it never
changes a status the hooks or the screen already hold: it speaks only for
a pane with no other record, takes back what it said when the transcript
does, and retires on any report of another's. `internal/detect` stays the
last fallback for an agent with no transcript or no hooks.

Capability note: **Codex has no permission-prompt hook**, so the scan is
its fallback for approval detection — 45 seconds after the call began, not
the moment it asks. Claude Code reports the prompt itself.

An end that comes while the app is away survives a restart: the pane's
summary is saved with its unread mark (`savedPane.Summary`,
[`internal/daemon/store.go`](../internal/daemon/store.go)) and a restored
pane shows it. Nothing else of the fold is saved; it is computed again from
the marks the daemon restored. The protocol and the saved state stay
additive (Version 3 and 2): the field is optional and a daemon of before
says none.

Code: [`internal/daemon/attention.go`](../internal/daemon/attention.go),
[`internal/daemon/store.go`](../internal/daemon/store.go),
[`internal/daemon/osc.go`](../internal/daemon/osc.go) (`applyReport`),
[`internal/proto/proto.go`](../internal/proto/proto.go) (`Attention`).
Tests: `attention_test.go`, `store_test.go` (`TestRestoreKeepsSummary`).

## The Attention pane

The **Attention** pane shows what needs the user, of every session, in one
list. It reads `Session.Attention` as kerod sends it and holds no state of
its own beyond the list: it runs nothing in the daemon, so it needs no
protocol change, and a kerod that does not send the state leaves the list
empty rather than breaking it. Its own state — the filter, the search,
where it is scrolled to — is the app's, one per pane (`App.attention`,
`attention_pane.go`). The pane itself is a view as Tasks, Files and
Browser are: kerod keeps it and its Target across a restart.

A row says the state's mark and name, the session's name and its project,
the agent, the line the state came with, and how long it has been so
(`ago`). Rows are the sessions of the sidebar's order, the most urgent
first — `waiting-user`, `running`, `unread`, `done`, `idle`, the daemon's
order — and of two as urgent, the one whose state came last. A click
selects the session and puts the keyboard in the pane the state came from
(`Attention.Pane`), where the decision is; it does not zoom, so the panes
beside it stay in sight.

The chips narrow the list to a state and count what each shows: **All**,
**Needs you**, **Working**, **Unread**, **Done**. The copy is the app's
own, as the sidebar and the footer say it: `waiting-user` is **Needs
you**. Idle sessions, at rest, have no chip: they are in **All**, last.
The search finds what a row says — its name, project, agent, message and
state — as plain text, and Escape clears it.

An approval shows the hook's own words. Codex has no permission hook, so
its wait comes from the transcript scan (above) and may carry no message
from the agent: where the message is absent, the row says **Focus pane to
approve** instead of nothing, since the approval is in the pane. A hook's
message is shown exactly as the hook sent it.

Code: [`attention_pane.go`](../attention_pane.go) (`attentionPane`,
`attentionRows`, `gotoAttention`, `attentionSummary`),
[`pane_views.go`](../pane_views.go) (`viewPane`), [`app.go`](../app.go)
(`App.attention`), [`internal/cli/cli.go`](../internal/cli/cli.go)
(`--attention`). Tests: `attention_pane_test.go`.

## SF Symbols

MyGo draws icons from SVG, and the app's icons are its own set
(`icons.go`). An icon the app adds now is the system's, drawn by macOS
through AppKit: an SF Symbol, so it matches the marks the menu bar,
Settings and the system apps show.

`symbolImage(name, points, tint, dark)` (`symbols_darwin.go`) asks
`NSImage` for the symbol, configures it for its point size, draws it into
an `NSBitmapImageRep` for the light or dark appearance under
`performAsCurrentDrawingAppearance:`, tints its pixels through their alpha
and returns a `ui.Bitmap`, which `ui.Image` shows as any other. AppKit is
reached through purego's Objective-C runtime, as the title bar is
(`title_bar_darwin.go`), so there is no cgo; AppKit is loaded on first use,
which a test binary has not done.

A symbol is drawn twice its size in points and shown at half it, so it
stays crisp on the app's screens, and it is drawn once per name, size,
tint and appearance: `symbolCache` holds the bitmap, so no frame draws a
symbol again. On a system without symbols (`symbols_other.go`) it is nil,
and the mark that asked for it draws nothing.

`symbolMark` ([`icons.go`](../icons.go)) shows one; `paneViewMark`
([`pane_views.go`](../pane_views.go)) picks a view's symbol or its icon.
The Attention pane's mark and its rows' states are symbols:
`bell.badge`, `hand.raised.fill`, `arrow.triangle.2.circlepath`,
`checkmark.circle.fill`, `checkmark.circle` and `circle`. The app's older
icons stay SVG, as they are. Tests: `symbols_test.go`.
## Notifications from attention

The app tells of a session by comparing the attention kerod folded
(`Session.Attention`) with the fold before it, in
[`attention_notify.go`](../attention_notify.go) (`attentionWatch`, `foldOf`):

- a session that comes to `waiting-user` tells once, and so does one that
  comes to `unread` (a turn ended unseen). A return to `running` tells
  nothing: it clears, as `done` and `idle` tell nothing;
- one notification per change of state, not per line of output. The state
  is what changes, and the same session and state inside
  `notify-throttle-minutes` (five by default, `0` for no throttle) is
  counted silently, so a chatty agent's loop does not fill Notification
  Center. A turn that begins again after the window tells again;
- an unseen end whose pane failed keeps the error's title and body, and
  `notify-on-error` decides it; `notify-on-waiting` and `notify-on-done`
  decide the rest, as they did. The sound, the Dock badge and the bounce
  stay as they were; the badge counts the sessions whose fold waits
  (`waiting-user`), not a pane's status.

Focus is the only filter: the session the user is in, in a window in
front, tells nothing. The watch itself knows nothing of windows or
profiles — it is handed every session the daemon holds, so a session in a
profile the user is not looking at still tells. That is the global rule
the profiles of `feat/profiles-a1` will need; no profiles code is built
here, and a later filter must not hide a session's attention from this
watch.

A session can be silenced: **Session › Silence Notifications**, the
session's context menu, or `kero do session.silence` turns its
notifications off until the user resumes them. The list is saved with the
settings (`silence-session`) and survives a restart of the app. A silenced
session tells nothing; once resumed, its next turn tells, inside the same
throttle window. A daemon of before the `Attention` field says none, so
`foldOf` reads the fold from the session's lead pane: nothing goes quiet
in that window.

What a notification says is the session's name and one line of what it
needs — the fold's `Msg`, which is the hook's or the screen's line, cut to
80 bytes by kerod — or the agent and the folder when the fold has none.
Nothing more of the conversation is read for it, and no credential goes in
it.

Code: [`attention_notify.go`](../attention_notify.go) (`attentionWatch`,
`foldOf`), [`app.go`](../app.go) (`notify`, `tell`, `toggleSilence`),
[`internal/settings`](../internal/settings/config.go) (`notify-throttle-minutes`,
`silence-session`).
Tests: `attention_notify_test.go`, `internal/settings/config_test.go`.

## Status indicators

A session row, and each count chip in the footer, draw the session's state
with one small mark, in `statusIconP` ([`view.go`](../view.go)):

- **Working** turns the loading ring, a faint track with an arc going
  round it, drawn as MyGo's `loading.Ring` draws it but in steps of 15 a
  second (`turningRing` in [`loading_ui.go`](../loading_ui.go)), and stands
  still where the desktop asks for less motion. A program that reports its
  own progress (OSC 7501) fills a determinate arc instead, from the top
  clockwise, on the same track.
- **Needs you** is an amber ring, heavier than the others, with a filled
  amber dot in its middle: it is the one state that should pull the eye.
- **Done** is a filled disc with a dark check until the session is read
  (unread, and its name is bolder), and a faint check once read.
- **Failed** — a program reported it failed — is a red disc with a bar,
  dimmer once read.
- **Exited** — the shell ended with an error; one that exits cleanly closes
  its pane — is a crossed ring, red until the session is read and faint
  after, beside the pane's "Exited" chip and its Restart button.
- **Inactive** — a session done and read whose status last changed 24 hours
  ago or more (`inactive`, `inactiveAfter`) — is a filled half moon, and
  its name is dimmed. A session working, waiting, failed, exited or unread
  is never inactive, so the calm moon never hides a failure.

Each mark has its accessible label — Working, Needs you, Done, Inactive,
Failed and Exited, from `locales/en.json` — read or unread, so a screen
reader says the state the icon draws. The turning ring is drawn by the mark
itself, with no unnamed progress element inside it. The marks are 15
points in a row and 12 in the footer chips.

Tests: `view_test.go` (`TestSessionStatusIcons`, `TestStatusIconProgress`),
`loading_ui_test.go` (`TestLoadingRingThrottled`).

## Sidebar project groups and title marks

**Project groups.** A project's header in the sidebar is
`projectHeaderH` (24) tall, `projectGap` (20) below the project above it and
with no margin under it, so it sits on its own sessions and reads as their
heading (`groupView` in [`view.go`](../view.go)). The first header has the
same gap under the Linear row; with the Linear row off it starts where the
rows always did. The header's name is 12 in, the left edge of its sessions'
title marks. Its parts keep one order and one place — name, count, chevron,
then "needs you" and the add button at the right — and the count and
"needs you" never shrink: the name gives way. The add button's 20-point
place is kept while it is hidden, so the header does not move when the
pointer comes over it.

**Emoji in a project title.** A project's title is user data, so an emoji
in it is allowed (the SF Symbols rule is about icons). It needed no change:
`project.rename` trims and stores the title as it is, `proto.Project.Name`
carries it, and the store saves it, so it survives a restart; the project's
id and directory never change. MyGo cuts a single line at grapheme
boundaries, so an ellipsis never falls inside an emoji, and its text
falls back to a font that has the glyph. `squash`, which matches a
project to a Linear project, drops the emoji. `kero projects` and `kero ls`
print it; `text/tabwriter` counts it as one column where the terminal draws
two, so that row's later columns are one off.

**Title marks.** A session's title can carry a mark at its head, an SF
Symbol that the app derives each frame from the state it already has and
never writes into the name (`titleMark` in
[`title_marks.go`](../title_marks.go)). The set is closed, in this order:

1. `hand.raised`, in the attention amber: the session is
   `waiting` on the user.
2. `ladybug`: the session's `TaskID` is an issue of the Linear workspace the
   app has (`State.Linear.Issues`) with a label named Bug. Linear has no issue
   type but its labels. A bug outranks working and done, since it is what the
   task is, but not needs you.
3. `arrow.triangle.2.circlepath`: the session is `working`.
4. `checkmark.circle`, faint: the session is idle and read, and its work
   ended — kerod's attention fold says `done`, a program left a message, or
   an agent's status changed since its pane began (an agent whose turn kerod
   saw on the screen leaves no message behind). A plain shell back at its
   prompt after a command keeps no mark.

The symbols are the Attention pane's for the same states (`attentionSymbol`
in [`attention_pane.go`](../attention_pane.go)), so a state reads the same
in the sidebar, the palette and the Attention pane, as Apple's guidance asks
of a symbol that means one thing.

A plain shell, a turn ended but unseen (its bold title and filled check say
so), a failure and an exit have no mark. The mark has its own 12-point slot
that never shrinks, empty for a session without one, so every title starts
at one edge, and the title is what truncates; the time and the status glyph
at the right keep their places. The command palette's session rows draw the
same mark. Each mark has an accessible label (Needs you, Bug work,
Working, Done).

Not done: a "stashed" mark needs a state to park a session in, which is a
protocol change, and is deferred (BRU-176). Detecting that a session runs
the tests is not cheap (the foreground program is only `go` or `npm`), so
only the task's label marks a bug. The session's pane header does not show
the mark.

Code: [`title_marks.go`](../title_marks.go) (`titleMark`, `turnEnded`,
`bugTask`, `titleMarkView`), [`view.go`](../view.go) (`groupView`,
`sessionRow`), [`palette.go`](../palette.go). Tests: `title_marks_test.go`,
`title_bar_test.go` (`TestSidebarTopSpacing`),
`internal/daemon/project_name_test.go`.

## Busy indicators

Where the app itself is busy — the connecting card before a session shows, a
pane attaching to a session, Files reading a directory, finding, or the file
viewer while a file loads, an issue being read — it draws the loading ring:
`working` in [`loading_ui.go`](../loading_ui.go), 16 points in the muted text
color, a progress indicator named for what it waits on ("Reading…" and
"Looking through the files…" in Files). It is loading.dev's Ring as MyGo's
loading plugin draws it — the same track, arc and 800 ms turn — but the
plugin asks for a frame at every display refresh, which paints the whole
window again for as long as it shows; `turningRing` asks for its next step
with `Painter.After` at 15 frames a second instead. It asks only while it
shows, and stands still, in the plugin's pose at rest, when the desktop
asks for less motion. The loading ring, on a working session or a busy
view, is the only continuous motion in the app, and it runs at that
throttled rate.

Code: [`loading_ui.go`](../loading_ui.go), and the call sites in
[`view.go`](../view.go), [`panes.go`](../panes.go),
[`files_pane.go`](../files_pane.go) and
[`linear_detail.go`](../linear_detail.go). Test: `loading_ui_test.go`
(the ring's steps, Reduce Motion, and no frames once it has gone).

## Account usage

Claude and Codex use existing CLI login credentials. OpenCode Go uses its
workspace key; DeepSeek shows direct API balance. These are provider-specific
allowances and balances, not a combined subscription budget. Reads run in
the background every two minutes, preserve the last successful observation
after errors, and respect rate-limit backoff. Explicit provider-key overrides
are atomically stored with mode `0600` in the personal data directory.

The sidebar's bottom bar is one ring per harness this Mac has installed:
Claude, Codex and OpenCode Go when their command or sign-in file is found
(OpenCode Go also by `OPENCODE_API_KEY` or OpenCode's own login), and
DeepSeek when its key is saved, set in `DEEPSEEK_API_KEY` or in OpenCode's
DeepSeek login; with none found it shows every provider, so a new user sees
what can be connected. The first allowance is the arc, amber once any of
the harness's windows reaches 90%. A harness with only a balance, as
DeepSeek, shows its currency's sign in the track instead; one not
connected, a dashed track; and an amber dot marks a ring whose last
refresh failed, the arc then being the last known usage. The names sit
beside the rings while they fit — in rows when the sidebar can't hold them
in one, and the rings alone at the narrowest sidebar, where the hover card
names them instead. The hovered harness's name turns the text's color and
heavier, and its ring's track darker. Hovering a ring shows the plan
(capitalized: the APIs send "max", "plus"), every allowance window ("5
hours", "Week") with its percentage and reset — the time today, the
weekday this week, the date from six days on — a balance, or why the
harness is not connected, and "Last known usage; refresh failed" over a
stale one; clicking it, or `Cmd+Shift+U`, opens the full in-window modal
with refresh, provider configuration and keys. The CPU and RAM meters stay
below the bar, unchanged (`resources.go`).

Detecting what is installed is `usage.Installed()`, which looks for each
command on `PATH` and then in the folders the installers use
(`installDirs`: `~/.local/bin`, `~/.claude/local`, `~/.opencode/bin`,
`~/.bun/bin`, `/opt/homebrew/bin`, `/usr/local/bin`), as an app opened from
the Dock or the Finder has only the system's `PATH`; and for the sign-in
files without asking the keychain for a secret, where Claude Code keeps
its sign-in. It runs off the main thread with each two-minute refresh, the
first at launch; until it returns, every provider shows. The bar's rows come from
`usageBarRows` in [`usage.go`](../usage.go); a ring is `usageRing`, the card
`usageHover`, its reset text `usageReset`. Tests: `usage_test.go`,
`internal/usage/usage_test.go` (`TestInstalledFromInstallerFolders`).

The modal shows allowance windows, reset times, refresh and provider
configuration, and its own error and connection states. `Cmd+Shift+U` opens
it; `Cmd+R` refreshes while open; Esc/Cmd+W closes it. There is no extra tab
or window.

Code: [`usage.go`](../usage.go), [`internal/usage/`](../internal/usage/).
Automation: `kero usage --json` and `kero do usage.open`.
Tests: `usage_test.go`, `internal/usage/usage_test.go`.

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

## Idle repaint floor

An idle window should not paint. Three timers that built and painted the
whole window, grain shader included, with nothing to show are cut back
(BRU-225, in part):

- **The CPU and RAM sampler** still reads every two seconds, off the main
  thread, but calls `a.update` only when `showResources` — the meters'
  labels, the tooltip's and the usage card's, the bars' fills and the
  error — differs from what it last sent. The bars fill in whole percents
  (`cpuPercent`, `memoryPercent`), so what is compared is what is drawn; a
  repeated error sends nothing either. At rest a Mac's CPU percentage still
  moves by a point now and then, so the saving depends on the machine.
- **Times ago** ("5m ago") read in whole minutes at the finest (`ago`). The
  sidebar asked for a rebuild every 20 seconds, shown or hidden; it now asks
  through `agoTick`, once a minute, and only while the sidebar shows. A time
  can read up to a minute late, so "now" can stay up to two minutes. The
  sidebar's tick also kept the Attention pane's rows and a working pane's
  chip moving; they now call `agoTick` themselves while they show times, and
  `c.After` from the same frame coalesces into one rebuild. The Linear
  views' times ("updated 5m ago") have no tick of their own: with the
  sidebar hidden they move on with the next rebuild for another reason.
- **The loading ring** needed no change: `turningRing` asks for its next
  step (`Painter.After`, a repaint without a rebuild) only while it is
  drawn on a working session with no progress, or on a busy view.
  `TestLoadingRingThrottled` covers it. A hidden sidebar is clipped to
  nothing, so MyGo's paint culling should not draw its rings; no test
  checks that.

Not done here: the cursor blink, which lives in MyGo's terminal plugin and
still paints the window about every 600 ms, and the issue's acceptance in
frames per minute. MyGo's `MYGO_FRAME_STATS=all` logs every frame, but no
before and after frame counts were recorded on this branch.

Code: [`resources.go`](../resources.go), `agoTick` in
[`view.go`](../view.go), [`attention_pane.go`](../attention_pane.go),
[`panes.go`](../panes.go). Tests: `idle_repaint_test.go` (what the sampler
compares; the window builds about twice in two idle minutes for the
sidebar, never with it hidden, and the Attention pane still ticks).

## Terminal responsiveness and attachments

The local build targets the host architecture: Apple Silicon is `arm64`;
Intel/AMD is `amd64`. The terminal's own status line no longer animates
constantly; the loading ring, on a working session or a busy view, is the
only continuous motion, and it turns at a throttled 15 frames a second
only while it shows. The daemon's
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

## Status detection from the last rows

kerod looks at every agent pane's screen on its 400 ms tick, with the
session's lock held, the lock its input takes too. The screen was the last
rows of the terminal's whole text: MyGo formatted all of the scrollback,
and kerod split it and joined its end, a copy of the scrollback each time.

Now `pane.tail` reads the text of the terminal's last rows alone, through
MyGo's `Terminal.TailText` (the fork's `plugins/terminal/terminal.go`).
That formats a selection of those rows (`vt.Terminal.TextFrom`) as `Text`
formats everything: soft-wrapped lines joined, trailing spaces trimmed. It
is the end of `Text`, but for its first line, which may be the rest of one
wrapped from above. `tail` reads twice as many rows as the screen has, and
twice as many again while they hold no more lines than it has rows, as
when blank rows end the screen or long lines wrap over many rows. It then
trims and takes the last lines as before (`lastLines`), so `look` and
`detect.Detect` see the same text, byte for byte. Once the rows reach the
top, `TailText` returns `Text` itself. `kero read` (`readText`) uses `tail`
too, unless it reads the whole scrollback.

`BenchmarkScreen` (Apple M4 Pro, a 96×28 pane with a full scrollback):

| Scrollback | Before | After |
| --- | --- | --- |
| 4 MB (default) | 0.75 ms, 387 KB, 16 allocs | 10 µs, 4.4 KB, 37 allocs |
| 64 MB | 12.6 ms, 6.2 MB, 18 allocs | 11 µs, 4.4 KB, 37 allocs |

The tests keep the old implementation as an oracle and compare the screen
and `kero read` with it, byte for byte: a full scrollback, blank rows at
the bottom, a cleared scrollback, soft-wrapped and wide lines, a text
shorter than the screen, the alternate screen and 400 random steps at
several sizes. Claude Code's recorded screens are detected as before.
Typing latency with a long scrollback was not measured by hand.

Code: [`internal/daemon/session.go`](../internal/daemon/session.go)
(`screen`, `tail`, `lastLines`, `readText`),
[`internal/daemon/screen_test.go`](../internal/daemon/screen_test.go).
## Scrollback budget

One setting, Settings › Shell › Scrollback (`scrollback`, 4 MB by
default), bounds both copies of a session's history: kerod's terminal and
the app's attached one. `Settings.ScrollbackBytes`
([`internal/settings/scrollback.go`](../internal/settings/scrollback.go))
turns it into what `terminal.Options.Scrollback` takes, -1 for none,
because a 0 there means MyGo's default of 10 MB. `newPane` in
[`internal/daemon/session.go`](../internal/daemon/session.go) and
`termOptions` in [`app.go`](../app.go) both use it. The setting counts the
emulator's memory, not text: a full 4 MB scrollback is about 350 KB of
history on disk, a full 16 MB one about 1.5 MB.

The app keeps at most `maxTerms` (8) terminals attached
([`term_budget.go`](../term_budget.go)). Every terminal that attaches is
counted (`noteTerm`), not only the pane in focus, which used to leave the
other panes of a session attached for good. `touch` drops those shown
longest ago (`trimTerms`), never a pane of the selected session, and never
while a frame builds. A dropped pane attaches again when it shows; kerod
kept it meanwhile.

kerod saves a pane's history (`terminal.History`) to `sessions/<id>.vt`
when it changed. The 10-second save writes only its tail: the last 256 KB,
cut right after a row's `\r\n` (`historyTail` in
[`internal/daemon/history.go`](../internal/daemon/history.go)). The whole
history goes only when kerod ends on a signal (`saveHistories(true)`). The
restore format did not change: a short file or an empty one restores, and
a crash restores the last screen with what fits above it. `History` still
serializes the whole scrollback before the cut; only the bytes written
shrink. Tests: `internal/daemon/history_test.go` (the tail restores the
same last screen as the whole history; short and empty files restore;
`BenchmarkSaveHistories`, `BenchmarkPaneHistory`) and
[`term_budget_test.go`](../term_budget_test.go).

## Daemon idle work

kerod's tick runs every 400 ms (`tickLoop` in
[`internal/daemon/daemon.go`](../internal/daemon/daemon.go)); what it does
for each pane is now bounded
([`internal/daemon/proc_cache.go`](../internal/daemon/proc_cache.go)):

- The foreground group (`TIOCGPGRP`) is still asked every tick. The
  program's name (`kern.proc.pid`) is read when the group changes, on every
  tick for its first 2 s, while env or a shim execs the real program, and
  then every 10 s. The arguments (`kern.procargs2`) are read only when the
  name changes and is not an agent's by itself: `node …/codex.js` and
  `bash …/claude` need them, `claude` does not. A minute of ticks on one
  program reads its name 10 times instead of 150, and its arguments at
  most once instead of 150.
- The shell's directory and the program's (`procCwd`) are read every
  1.5 s for the pane the user is in, the focus of the session the app
  shows (`focusedPaneLocked`), every 10 s for the others, and at once when
  the foreground group changes. A `cd` in a pane the user is not in shows
  in the sidebar up to 10 s later.
- The branch scan has no loop of its own
  ([`internal/daemon/branch.go`](../internal/daemon/branch.go)). Every
  fourth tick (1.6 s) `branchTick` stats the HEAD files that the last scan
  read; it reads the branches again when one changed, every 30 s, and when
  `branchWatch.due` is set: by the `focus` and `view` requests and by a
  pane whose agent, directory or program directory changed. git replaces
  HEAD with a rename, which changes its time, so a switch is seen within
  2 s. If the tick ever stops while idle, this check needs a file watch
  instead.

`state.json` is written 2 s after the first change of those that come
meanwhile (`saveLoopWith` in
[`internal/daemon/store.go`](../internal/daemon/store.go)), not 300 ms
after each: at most one write per 2 s under a storm of status changes. The
signal and `shutdown` paths still save at once.

The Linear workspace moved from `state.json` to `linear.json`
([`internal/daemon/linear_store.go`](../internal/daemon/linear_store.go)).
In memory and to the app it is as before. `saveState` writes
`linear.json` only when the workspace is a new copy (`linearSet` replaces
it and never changes it in place), and writes it before `state.json`, so a
crash between the two writes loses nothing. Each delta read moves
`fetchedAt`, which `linearEqual` counts as a change, so `linear.json` is
still written after every successful read (about every 30 s), as the
blob in `state.json` was; what changed is that a status flip no longer
rewrites it. Migration: the restore reads
`linear.json`, and takes the workspace from an old `state.json` when
`linear.json` has none or an older one (by `fetchedAt`). The next save
writes it to `linear.json` and leaves it out of `state.json`. The
`state.json` format is otherwise unchanged, and its `linear` field stays
readable. Rollback: an older kerod reads the new `state.json` with no
workspace, and loses only the cached copy until its first read from
Linear. Tests: `internal/daemon/linear_store_test.go` (a fixture saved by
the release before, `testdata/state-linear-v2.json`, loads whole, moves,
and reloads), `internal/daemon/idle_test.go` (read counts, focus, the
debounce under a storm, the HEAD check).

## Performance measurement

One command measures what the app and kerod cost with N panes against
the budgets of BRU-222, and two tests keep the allocations of the hottest
paths from growing (BRU-223). The budgets, how each number is taken and
the baseline are in [Performance](perf.md).

- `scripts/perf.sh N` builds the checkout, starts a kerod and the app of
  its own (a temporary `KERO_HOME`, `HOME`, socket and `TMPDIR`, every
  `KERO_*` variable cleared), feeds N panes 16 MB each, samples 60 s of
  idle and 10 s of a streaming pane, and prints the table. The app runs
  as a plain executable named `shibu-perf`, so its single-instance lock
  and identity are not the daily app's; `PERF_APP=0` leaves it out.
  [`scripts/perf_probe.py`](../scripts/perf_probe.py) reads footprint,
  RSS, CPU time and wakeups with `proc_pid_rusage` (no root), parses
  `MYGO_FRAME_STATS=all`, and prints the table; its tests are
  `scripts/test_perf_probe.py`.
- `KERO_PPROF=127.0.0.1:6060` has kerod serve `net/http/pprof` on a
  private mux there until it ends (`serveProfiles`, one deferred line in
  `Run`). A non-loopback address, one without a host, or a port in use is
  logged and refused, and kerod runs on. While it serves, `profiled`
  labels `pane.tick` (`tickLoop`), `pane.screen` (`screen`),
  `saveHistories` (the 10 s save) and `attachment.send` with
  `pprof.Do`. Unset, `profiled` only calls its function, and its closure
  stays on the stack: no allocation on the frame path.
- `TestScreenAllocsFlatWithScrollback` measures the bytes and objects one
  `screen()` allocates with 1 MB and 16 MB of full scrollback, and fails
  if they grow, or pass 16 KB. It measures the old whole-text read too,
  which must grow, so the test shows it can tell.
  `TestFrameAllocsFlatWithScrollback` does the same for a key's echo, in
  the shape of `terminal_perf_test.go`: kerod's side (its terminal and
  the mirror's frame) and the app's (its terminal and a frame of the
  terminal view), against today's cost and about half again.

The baseline, on c8ac9bc with this harness, is in
[Performance › Baseline](perf.md#baseline). Not measured: key press to
paint, which needs keys sent to the window, and the app with every pane
visited.

Code: [`internal/daemon/pprof.go`](../internal/daemon/pprof.go),
[`internal/daemon/perf_test.go`](../internal/daemon/perf_test.go),
[`frame_allocs_test.go`](../frame_allocs_test.go),
[`scripts/perf.sh`](../scripts/perf.sh),
[`scripts/perf_probe.py`](../scripts/perf_probe.py). Tests:
`internal/daemon/pprof_test.go` (loopback only, off when unset or
refused, served and stopped, free when off).

## Title bar and traffic lights

The title bar is macOS's compact title bar and toolbar, and the traffic
lights stay in sight in full screen, as the system's own buttons.
Measurements below are from real windows on macOS 27.0.1.

**Height.** MyGo's inset title bar (`TitleBarHiddenInset`, in
[`platform.go`](../platform.go)) gives the window an empty `NSToolbar` in
the automatic style, which AppKit made a 52-point bar here (66 to the
content while the window is key). MyGo does not set the toolbar's style,
so Shibu does, natively, through purego's Objective-C runtime:
`setCompactTitleBar` ([`title_bar_darwin.go`](../title_bar_darwin.go))
sets `NSWindowToolbarStyleUnifiedCompact`. AppKit then makes the bar 40
points, with the lights, 14 points and 23 apart, 12 from the left and
centered 20 from the top. Shibu does not move them (no
`TrafficLightPosition`). `topBar` is 40 and `barMid` 20
([`view.go`](../view.go)): the sidebar's title bar, and its buttons,
center on the lights. A card's header is its own 36 (`headerH`), for a
pane's title, 28, and its buttons, 26.

**Spacing.** The cards' column starts `chrome.topPad` down: 8 with the
sidebar shown, and 2 with it hidden, where the first header has the title
bar, so that the header, 36 tall, centers on the lights' line, 20, with
its title and the sidebar's buttons it holds (`headerButtons`). It moves
with the sidebar's animation, in whole points; on Windows and Linux,
whose window controls are the header's own, it stays 8. Before, the
header centered 6 points below the lights. In the sidebar, the sessions
list starts 10 below the bar and its rows 12 in (it was 0 and 10): the
first row starts 30 points below the lights' line, as it did under the
60-point bar (line 30, first row at 60), rather than 20, 7 points under
the buttons; the rows' left edge lines up with the close light's and the
footer's, 12.

**Full screen.** macOS moves the lights to a title bar in a window of its
own, which slides down over the top of the window with the menu bar: with
the automatic style, a 66-point strip with a second set of lights. MyGo
reports an empty title bar there. Shibu follows the window natively
(`followFullScreen`), for the main window only:

- Going full screen (`NSWindowWillEnterFullScreenNotification`), while the
  lights are still in the window, it reads where they are, then hides
  them and the title bar's container (`NSTitlebarContainerView`), whose
  background is the strip's, and has the strip's window ignore the mouse.
  Once there (`...DidEnterFullScreen...`) it does so again, as the
  container has moved to that window; a window that opens in full screen
  is caught there.
- In their place it puts the system's buttons, made as NSWindow makes its
  own (`+standardWindowButton:forStyleMask:`), in a view of their own
  (`ShibuTrafficLights`) over MyGo's content view, where the window's
  were, or where AppKit puts them in the compact bar if none were read.
  Close sends `performClose:` to the window, the green light
  `toggleFullScreen:`; minimize is disabled, and shown, as AppKit hides it
  in full screen. Their colors, and their greying with the window behind,
  are AppKit's.
- Hover glyphs: AppKit's buttons ask their superview `_mouseInGroup:`, as
  the title bar answers it, and look again on `mouseEnteredOrExited`. The
  group tracks the pointer (`NSTrackingArea`), answers `_mouseInGroup:`,
  and calls `mouseEnteredOrExited` on each button as the pointer comes
  and goes, if the button responds to it. Both are private AppKit
  methods; Electron answers `_mouseInGroup:` the same way. Should a later
  macOS drop them, the lights still work, without their glyphs.
- Should the group or the buttons not be made, Shibu leaves the window's
  own lights and the strip as they are: macOS's own full screen, with
  the lights in the bar that slides down with the menu bar, rather than
  no lights at all. The drawn lights of the earlier version, the other
  fallback, are gone; this case has not been seen.
- `titleBar` ([`title_bar.go`](../title_bar.go)) keeps the bar from the
  window, so the sidebar's buttons do not move. Back in the window
  (`...DidExitFullScreen...`) the group goes, and the system's lights and
  container come back.
- The window's style, `NSWindowStyleMaskFullScreen`, decides again as the
  window resizes or becomes key (`sync`): a change that went by unseen,
  as when the window is restored into or out of full screen, can no
  longer leave the window's lights hidden in the window, or the group up
  outside full screen.

A report that hiding the sidebar took the lights away, and showing it
brought them back, was not reproduced: in an isolated build of the app
(its own binary name, data and socket), in the window and in full screen,
with and without a session, and opened in full screen, the lights were
there with the sidebar shown and hidden, by AppKit's state and by
captures of the app's own window (`CGWindowListCreateImage`). The one
fault seen was a window restored in a state where its own lights stayed
hidden after a full-screen change went by unseen, which the `sync` above
now repairs; the drawn lights of the earlier version are gone too.

Checked with real windows in that build, driven by a probe rather than
by hand: in full screen the group is the content view's last subview,
over MyGo's surface, at 12, 13 from the top left, 60 by 14; close is
enabled, minimize disabled and in sight, and the system's own buttons
and container are hidden. Clicking the buttons with `performClick:`,
the green light left full screen and close closed the window. Calling
the group's hover handler showed the glyphs in a capture; the tracking
area firing under a real pointer was not watched. Back in the window
the system's lights are where they were, and going full screen again
reads them again. The slide-down strip itself, with the pointer at the
top of the screen, was not watched either: the probe could not move the
pointer. The spacing was checked in the tester's frames, not in a real
window.

Windows and Linux are unchanged (`ownControls`), but their
window controls, in the headers, are now 6 points below the sidebar's
buttons.

Earlier versions of this fix drew the lights with a 48-point bar
(`f777a8c`), followed macOS's own full screen with a 52-point bar
(`09fae71`), and drew Kero's own lights in full screen under the compact
bar (`09886fe`).

Tests: [`title_bar_test.go`](../title_bar_test.go) (the compact bar, the
sidebar's buttons on the lights' line and unmoved in and out of full
screen, with the sidebar shown and hidden, the first header's title on
the lights' line with the sidebar hidden, the sidebar's first row and its
left edge, and a window that opens in full screen). The native lights
are AppKit's and are not in MyGo's tester; they were checked in real
windows as above.

## Motion, accent and grain

The app's motion, its accent colors and the grain over its wash share a
section here because they are one taste: how the window looks as it moves.

**Motion tokens.** [`motion.go`](../motion.go) holds the app's durations
and curves as tokens: `press` 90 ms, `hover` 120, `row` 160, `focus` 160,
`slide` 260 over `CubicBezier(.25, 1, .5, 1)`, and the springs
`springDrag` 420 ms with a bounce of .18 and `springUI` 300 ms with .12.
Call sites use the token, not the number, so a change to how the app moves
is one edit. A few literals stay where no token fits, among them a
switch's track (150 ms) and its knob's spring (260 ms, .15) in
[`elevation.go`](../elevation.go), the Settings header's scroll edge
(160 ms) and the drop preview's fade-in (140 ms). The sidebar's own animation and the panes' layout motion were
the same 220–260 ms curve; both are `slide`. The chevron turned with a
500 ms spring; it turns with `row`. `Reduce Motion` is mygo's own.

**A project collapsing animates.** The rows of a collapsed project are
still not built ([`view.go`](../view.go) keeps the structure: collapse
expresses itself as a different tree, not hidden geometry). What changed
is that the rows carry `Transition`: a row that stops being built folds to
nothing along the list's direction while its parent still shows, and a row
coming back unfolds from there; the header carries a Position transition
and moves with them. The transition moves position only, besides the
fold, which mygo's Enter and Exit animate whatever the transition moves:
the row's background keeps its own `AnimateColor`, as easing the colors
twice would ease them twice, and a row's width follows the sidebar's
divider at once, as the headers' does, rather than lagging it.

**Accent presets.** [`accentSpecs`](../theme.go) holds six hand-tuned
accents — the blue the app has had, Plum, Rose, Sea, Moss and Graphite —
each a color in the light and in the dark, and a terminal selection in its
tint over the card. The ids and names live with the wash each comes with
(`settings.Accents`), and the three washes of their own (plum, rose, sea)
are in `washTable` with the five the window has had, so `background` stays
a free choice after a preset is picked. `accentPal` returns a palette from
a cache keyed by the accent and the appearance — built once, not per frame
— and `paletteFor` forwards to it; the base palettes carry the default, so
choosing nothing shows what the app has had. The terminals' themes are
functions now: `termTheme` styles a theme on the card's color with the
selection the accent has, and a change of accent attaches the terminals'
theme anew. A unit test holds every accent to its numbers: at least 3 to 1
on the card and the sidebar, in both appearances, and the white text of a
primary button at 4.5 to 1 on it.

**The grain.** [`grain.go`](../grain.go) holds one fragment shader of
package `ui/shader`: a hash of each two pixels of the window, half the
specks white and half black, from no image and no time. `backdrop` draws
it on the backdrop row after its background and before its children, so it
lies over the wash and under the sidebar and the cards, at 3.3% in the
light and 4.5% in the dark at a speck's strongest — an average of some
1.8% — and about 30% lighter where `Vibrancy()` has the desktop showing
through,
which has a texture of its own. `grain = false` turns it off.

**Focus, not dimming.** A pane that is not focused is the pane it is: its
card is `pal.card` and its text is untouched. [`panes.go`](../panes.go)
draws one absolute, pass-through edge over every card, inside its border:
1 point inside the dark's lit edge, at a radius of 13 to follow its inner
curve, and at the card's 14 in the light, which has no border. The focused
pane's edge is `pal.focusEdge` when the session shows several panes, and
not for a pane alone or zoomed, which has none to be told from; the
others are transparent, so every card has the same room and nothing moves
when the focus does. The color is the accent at 50% in the light and 62%
in the dark: the base palettes write the blue's so, and `accentPal`
recomputes it for a preset. The edge animates with
`focus`; clicking a pane, cycling panes and moving the focus all use the
same path.

**A pane opens and closes.** [`paneCard`](../panes.go) poses a new pane at
`Scale 0.985` and full fade on its first frame, then `Scale 1` and fade 0
over `focus` (160 ms); a closing pane goes the other way. The layout still
grows the pane from its edge (`growing`) and gives the room back
(`closing`) over `slide`, so the two motions meet rather than replace each
other. `focusNew` and `addPending` mark the panes that open.

**Hover that waits.** [`hoverIntent`](../view.go) keeps a project header's
hover chrome for 80 ms after the pointer leaves it, tracked per project in
`App.hoverSeen`/`hoverUntil` and woken by `Context.After`. The chevron and
the add button read that one answer, so they leave together.

**Empty states that teach.** `linearNothingAction`
([`linear_view.go`](../linear_view.go)) adds the one action to the Tasks
and Linear empty states, by the kind of state it is (`linearEmpty`). A
workspace not read (`linearUnread`) offers **Open Settings** for a missing
or refused key or the integration turned off, and **Refresh Linear**
otherwise; never **Clear Filters**, which from a Tasks pane, outside
`withLin`, would clear the Linear page's filters. A workspace read with no
issue to show (`linearNoIssues`) offers **Clear Filters** when a filter the
user set narrows it (`linearFiltered`: a Tasks pane's project is its
link's, forced every frame, and not one), **Show Done** in a Tasks pane
whose issues are all done and so hidden (`linearModel.doneHidden`), and
**Refresh Linear** otherwise, under a detail that no longer says a filter hides the issues
when none does. A workspace not read says why: how to set a key when
there is none, else its status (`linearUnreadDetail`), which now has a
line for the integration turned off. The same button is used from the board, the list and a
Tasks pane. The error copy around them says what went wrong and, where
there is one, the next step: the Linear key goes to Settings › Linear, the
browser asks for the address again, a file or folder that cannot be read
checks that it still exists and may be opened, settings that cannot be
saved check that the configuration folder is writable, and a failed CLI
install gives its error, on every platform.

**The first run.** [`intro.go`](../intro.go) holds the reveal, on a
first run: there was no `app.json` when the app opened. From the window's
first frame, before kerod has sent a state, `introStart` draws the accent
wash and the mark (`introState.shown`) and keeps the shell at opacity 0,
so no frame shows the shell before the overlay. When `App.loaded` turns
true, the shell's timeline starts (`started`, `since`) and the prefs are
written, so a crash does not replay it. The shell (`view`) fades and
slides 8 points in over 520 ms from 180 ms; the sidebar's first ten rows
stagger at 18 ms each; the mark keeps fading in from the first frame, not
again from the state; the overlay leaves from 600 ms and ends at 900 ms.
The wash's color is constant and the overlay's opacity fades it, once. A
state slower than `introWait` (2 s), a version mismatch or a failed
connection skips the reveal (`skipped`): the overlay goes and the shell
fades in over 150 ms, so it never covers kerod's error, and a state coming
later starts nothing. The overlay holds the keyboard with `Focus` every
frame, and `focusTerminal` does nothing while it shows, so the state's
selection does not take the keyboard from it. Its `HandleInput` takes
every event: a pointer press or a scroll ends it, and a key ends it as it
goes up, so the key and its text reach no pane; window shortcuts go first
and still act. A dialog opened during it (the palette, the chooser,
Settings, a confirmation, the usage) ends it, so the dialog's field keeps
the keyboard. `finishIntro` gives the keyboard back to the selected
session's pane once the frame the reveal ends in is built, unless a
dialog is open; a reveal ended while it waits counts as seen. A skipped
reveal's shell fades without moving. With Reduce Motion it is a 150 ms opacity
fade, with no translation, stagger or scale.

Code: [`motion.go`](../motion.go), [`theme.go`](../theme.go),
[`panes.go`](../panes.go), [`view.go`](../view.go), [`intro.go`](../intro.go),
[`grain.go`](../grain.go),
[`internal/settings/settings.go`](../internal/settings/settings.go),
[`internal/settings/config.go`](../internal/settings/config.go).

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

**A restart keeps each pane's status mark.** kerod saves, with each pane in
`state.json`, where its status stood: `status`, `since` (when it last
changed), `unread`, a failure's `message` and an exited shell's `exitCode`
(`savedPane` in [`internal/daemon/store.go`](../internal/daemon/store.go)).
A restore (`restoredStatus`) brings back the unread mark and the time, so the
ages and the inactive moon survive a restart. What ran in the pane is gone
and a new shell starts, so a pane that was working or waiting comes back
idle and unread, dated at the save, so that the user sees the work stopped
and a long job does not come back at rest since it began; a shell that
exited with an error unseen comes back failed, "Exited with code N", and
one already seen idle, both dated at the save; a failure stays failed until
the user's first key, as before. The migration is additive: the
fields are optional and left out when empty, the file's `version` stays 2, a
state file of before loads with its panes idle and read as of the restore,
as it always did, and an older kerod reads past the new fields. So the first
restart into this version still loses the marks once, from the old file;
kerod saves the fields with every change of status (`changed` queues a
save), and every restart after keeps them. Tests:
`internal/daemon/store_test.go`.

## Web links open in the browser

`openPath` (`platform.go`) calls `mygo.Shell.OpenPath`, which builds a
`file://` URL from its argument, so a web address handed to it became a
file's path and nothing opened. `openURL`, beside it, calls
`mygo.Shell.OpenExternal` and opens a link in the app that handles its
scheme. The Browser pane's **Open in Default Browser** button
(`browser_pane.go`) and a web link in a Files pane Markdown file
(`fileLink` in `files_pane.go`) use it. The other `openPath` callers hand it
real paths: a file's **Open** in the Files pane, a Command+clicked path in a
terminal (`openLink` in `app.go`, which leaves URLs to the system) and the
data folder in Settings. The same `openURL` lands with the plugin host on
`feat/plugins`, where it also opens plugin links.
