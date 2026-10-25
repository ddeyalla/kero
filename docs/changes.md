# Shibu Code changes and implementation

For release-by-release history see [Changelog](changelog.md). For daily use
and credential configuration see the [local edition guide](local-fork.md).

## Navigation placements, tabs and the usage popover

`navigation.go` owns the one saved placement. `prefs.NavPlacement` is
`left`, `right`, `top` or `bottom`; a missing or unknown value reads as
`left`. `setNavPlacement` saves the value and closes the usage popover only
when the placement actually changes; an unchanged request returns first. It
never changes the selected session, its panes, their terminals, the focused
pane or a zoom. The four actions `nav.placement.left`, `nav.placement.right`,
`nav.placement.top` and `nav.placement.bottom` are in `internal/actions`, so
the View menu (with its radio check), the command palette, Settings ›
Keyboard and `kero do` all reach the same setter. Settings › General draws one segmented
control through `segmentedOnChange`, because a preference saves as it
changes rather than with the settings page. `Cmd+B` keeps the
`sidebar.toggle` action and the saved `sidebarHidden` value; it now hides
the surface in use, tabs included.

`view.go` composes two shells around the unchanged pane tree.

- `verticalShell` draws the sidebar on the left or the right of the panes.
  `navChrome` keeps the left sidebar's existing geometry and mirrors the
  right one: the panes own the left edge and the traffic lights, so their
  header reserves the lights' lead, and the right divider drag subtracts its
  `dx` because its edge runs the other way.
- `horizontalShell` draws a tab row over the panes or under them and lets the
  panes take the window's width. Top tabs take the compact title bar's line
  and its traffic lights; bottom tabs leave that line to the panes. When
  navigation hides, the pane header brings the reveal and new-session buttons
  up after the lights, and its title pad grows so the title never draws over
  them.

The sidebar footer reserves room by width before it places controls.
Usage, Settings, the connection/restart icon and the attention count keep
their places; project open and search are eligible when the sidebar is at
least `sidebarFooterNarrow` (240 points), but the width budget still decides
whether they show, and they move into a labelled Sidebar Actions menu when
they do not. When the footer must overflow, it reserves the
required controls first and then places attention before the optional
project, search, working and done controls; widening across the 240-point
threshold cannot hide the attention chip. Status chips are compact
icon-and-count buttons; the footer places waiting, then project, search,
working and done while they fit, and the ones that do not fit move into the
same menu. `statusCounts` is the single count the layout and chips share;
`sidebarOverflow` builds the labelled menu from the controls that were
hidden. This keeps every required control usable at
the 200-point minimum, in either sidebar, including a stale service,
connecting and disconnected states. The connection and restart control is
the same icon-only `navConnection` the tab row uses.

`navigation_tabs.go` draws the shared top and bottom row. `groups()` stays the
source of truth for project order, pinned-session order and the synthetic
Other group. `navProjectGroup` puts each project header and its sessions in a
glass capsule with two points of padding, two-point gaps between its children
and six points between project capsules. `glassToolbar` supplies an opaque
control surface under Reduce Transparency; the capsule nesting does not
replace the project's or session's own controls.

Project header widths and the required utility cluster come out of the
available width first. `navSessionWidth` receives the current palette and
measures each session's full title at its actual font weight, status, reserved
close control, padding and gaps. It adds the two-point border only when
`navSession` draws it in dark appearance. For a pinned session it calls the
same cached `symbolImage("pin.fill", 10, pal.dark)` path as `symbolMark`,
reserving the pin's ten points and six-point gap only when that symbol exists;
a missing symbol leaves no blank allocation. `navPreferredSessionWidths`
measures the visible tabs once for both optional plugin-pin eligibility and
allocation. Pin visibility reserves the sum of `navSessionFloor`, the same
compact-title floors the
allocator protects, so an optional pin moves into the labeled plugin overflow
rather than causing otherwise avoidable scrolling. Visible pins do not also
appear in that menu.

`navSessionWidth` caps each preferred width at 220 DIPs. `navFitSessionWidths`
fits those capped values using an 80-DIP minimum. A compact tab whose
preferred width is at most 120 DIPs keeps that full width. When every
preferred width does not fit, only longer tabs shrink:
their flexible width above 80 is scaled by one shared proportion, while
compact tabs stay whole. If the compact widths and 80-DIP long-tab floors
exceed the room, `ui.ScrollHorizontal` scrolls the row. The overflow button
lists sessions from expanded project groups plus plugin shortcuts that did
not fit; explicitly folded projects stay represented by their headers and
their sessions stay out of the menu. No row wraps.

A project header folds its sessions on click, keeps the selected session's
panes in view and marks itself when it is folded over the active session.
Tabs reuse `sessionMenu`, the rename and drag helpers, and the pin and project
ordering operations. Session drag uses a vertical insertion rule; project
headers drag whole groups.

Selection by name goes through `selectRevealing` instead of `selectSession`.
The palette, overflow menu, number and next/previous shortcuts, attention
jumps, remote `kero focus`, a new session and a plugin notification's click
(`focusPluginPane`) expand a folded project and set `nav.reveal`; the
selected tab calls `ScrollIntoView` once. `selectRevealing` resolves the
session's displayed group with `knownProjects`, `groupOfProject` and
`displayedGroup`, the same rule `groups()` uses: an empty or unknown project
ID selects the synthetic Other group and expands Other's `""` collapse key,
while the session's own `Project` stays as it was. `requestReveal` is the
shared reveal request: it changes only `nav.reveal`, never the selection or
the page. An attention jump always calls it, even when the session is already
selected and its group is open, because the tab may be scrolled out of sight;
the jump then selects and expands only when the page, session or displayed
group needs it. Startup reconciliation and `toggleProject` still call
`selectSession` or nothing, so a fold survives a restart and never becomes a
selection change.

`nav_usage.go` hosts the usage popover on the shared plugin host.
`@nav/usage` is a transient surface id, not a daemon pane: `pluginPane.go`'s
`pluginSurfaceState`, `preparePluginSurface` and `syncPluginTheme` give it
the same open, resize, visibility, theme and tree rendering as a pane, while
`navUsageWidget` draws the Usage plugin's large list and the app's existing
CPU/RAM sampler adds the meters. The popover is a `ui.PopoverBase` anchored
to the usage icon: hover opens it, `hoverIntent`'s 80 ms grace lets the
pointer cross into it, a click pins it, and an outside click or Escape
closes it. An unpinned card the keyboard has entered stays open after the
pointer leaves: `hadFocus`, read from the panel's focus, holds it while the
keyboard is inside, and the pointer rule resumes when that focus leaves.
Escape is scoped to the popover's own focus so a passive hover never takes
the terminal's Escape; opening by keyboard focuses the card, and a keyboard
dismissal returns focus to the icon. `armed` and the cleared hover timers
keep a dismissal from reopening immediately.
The card sizes itself from the room its anchor leaves. In top and bottom tab
placements it subtracts the anchor gap and the toolkit's four-point window
margin before setting the panel height, so the footer keeps its full
30-point control and ten-point bottom padding; the plugin list gets the rest
and its own scroll. Left and right placements use the normal capped height,
`min(560, winH - 24)`.
`Open in Pane` calls the normal `openPluginPane`. Closing runs `host.Close`
for `@nav/usage` alone: a visible real Usage pane keeps its subscription,
and the usage plugin stops its loop only when its last visible pane is gone.
The app's old sidebar list, row and hover card were removed; the Usage modal
and its reader remain on `Cmd+Shift+U`, and the Usage pane remains a normal
plugin pane.

The navigation tests in `navigation_test.go` cover the four shells, saved
and invalid placements, unchanged pane identity, focus, zoom and terminal
attachment, folding the active project, content-measured widths and the
80/220 bounds, compact-title protection, pinned-title room, plugin-pin
eligibility against those floors without wide-window menu duplicates, overflow
selection and reveal, pins and Other order, drag without selection, close, rename and
menus, and capsule interactions in top and bottom, light and dark, with and
without Reduce Transparency. They also cover Cmd+B geometry, Settings and
palette actions, and the transient usage surface's hover, pin, outside click,
keyboard Escape, coexistence with a real Usage pane, missing plugin and
crash/restart.

## Glass control boundaries

`toolbarGroup` in `glass_ui.go`, the Browser address in `browser_pane.go`,
and `controlFace` in `elevation.go` add a half-point inset hairline. Glass highlights can become pure white on a white
pane, making the horizontal edges disappear while the rounded ends remain
visible. The inset outline preserves the complete control boundary without
changing layout, hit targets or glass interaction. It also applies to opaque
Reduce Transparency surfaces. Floating panels and plain sidebar/pane-header
buttons retain their existing styling. Settings and Attention search fields
keep their existing outline, without a second ring. `glass_capsule_edge_test.go` renders
actual plugin menus and toolbar material over white and checks all four edges.

## Increase Contrast

The desktop's Increase Contrast reaches Shibu's own drawing, not only
MyGo's glass. `paletteFor` asks `highContrastPal` for a cached variant of
the frame's accent palette; the accent, card fills and body text are
untouched. Muted and faint text move 35% and 55% toward the body text, and
the boundaries High Contrast owns — card and field edges, dividers, and
the `hairline`, `hairlineSoft` and `hairlineStrong` used by `controlFace`,
`litEdge`, Browser/Files toolbars and the shared field rim — take the text
color at a visible opacity. Light pane cards gain a real one-point boundary through
`raised`, whose ordinary edge is only a shadow; it is painted with an inset
rim, so content and terminal bounds do not move. The focused pane's
edge becomes fully opaque in the accent. The variant is derived from the
ordinary accent palette before the app's ambient blend — the palette the
accent cache returns, so each preset has its own — and is cached by that
palette; the per-app ambient color blends in afterwards. The ordinary
palette is never mutated, and turning the setting off returns it exactly.

`high_contrast_test.go` renders muted and faint text, a card edge and a
styled field in light and dark, toggles the setting in the same app, and
compares rendered contrast and edges, the accent and the ordinary frame
before and after. It also checks a selected and an ambient accent, the
per-preset cache, and an opaque `controlFace` under Reduce Transparency.
`SetPreferences` stands for the desktop setting; the native OS switch is
Codex's hand check.

## Delivery review corrections

A failed Spotify refresh leaves saved track and queue rows visible with a
warning and Retry. Retry is suppressed while that list's request is in
flight. Engine and desktop-monitor callbacks carry their source and generation,
checked atomically with playback application, so replacing a player also
retires already dispatched callbacks. `main.go` initializes focus and resource
pacing from the native window instead of the headless fixture defaults.

`actions.go` dispatches `pane.new.attention` separately from `pane.new.plugin`;
`pane_action_dispatch_test.go` drives the action through its daemon split
request. Spotify's `readNow` in `internal/plugin/builtin/spotify/spotify.go`
captures the engine generation and pushed-playback sequence before reading;
a response from an older engine or before a newer push is discarded. The
blocked-read regression in `playback_order_test.go` verifies that an old
response cannot replace the newer visible track. Account Usage titles and
accessibility labels share `usageAccountTitle` in the locale catalog.

## Performance measurements and idle scheduling

Daemon-only `PERF_APP=0` runs build the CLI without fetching or staging native
app libraries. `perf_probe.py` judges the visible/unfocused wakeup budget
only for an explicit `idle_window="unfocused"`; legacy measurements without
independent focus evidence report counters with no wakeup verdict.

`internal/daemon/branch.go` checks inexpensive HEAD metadata every 1.6 seconds
independently of the slower pane tick, so a checkout by another process no
longer waits ten seconds. `resources.go` refreshes focused meters every ten
seconds and unfocused meters every thirty seconds, with an immediate reading
on regained focus. `scripts/perf.sh` uses a temporary diagnostics overlay from
`scripts/perf_memory.py` to send 100 native AppKit key-down/up pairs only to its
own isolated window and follow each real PTY echo through painting. The
endpoint and listener never enter production builds. See `docs/perf.md` for
measured samples, distinct RSS and physical-footprint counters, native-input
scope and the remaining BRU-333 native-allocation ownership investigation.

Clip-mask idle memory lives below Shibu, in MyGo's
`internal/gpu/transform.go`: the builder caches transformed clip masks for the
current frame, then drops idle masks once their combined retained backing
capacity exceeds the 32 MiB budget. A mask that shrinks keeps its image's
backing array through `scene.Image.Resize`, so counting only
`len(m.image.Pix)` could leave more retained backing capacity than the budget
allowed. The pinned MyGo fork (`7fb5c349`,
`v0.5.6-0.20261025114427-7fb5c34991fd`) counts `cap(m.image.Pix)` instead; the
budget and the lifetime of masks used by the current frame are unchanged. Shibu
pins the fork commit through the existing `replace` directive, never through a
local path. This is accounting and lifetime correctness, not a claimed
25-session or total-footprint improvement.

C9 recorded a corrected 25-session attribution run that predates the
`7fb5c349` capacity fix; it measured `04895fe` plus the accepted reporting
diff `892838…`, with MyGo `439448` and the sealed C8 app/daemon binary
`adca389b…`. The private no-forced-GC run completed two
painted 25-pane sweeps and a 60.023 s quiet sample with 8 retained app clients
and 25 daemon VTs. Observed maxima were app RSS 290.516 MiB and footprint
378.642 MiB, daemon RSS 231.938 MiB and footprint 167.423 MiB; every sampled
app footprint stayed below the 750 MiB aggregate threshold. `NumForcedGC` was
0 in all 114 process snapshots. The stage table, separate RSS/footprint,
Go/VT/Metal counters and the 8-versus-25 retained distinction are in
[Performance](perf.md#c9-25-session-attribution). The historical 1.2 GB daily
observation, the earlier C8 partial 413 MiB result and earlier misses are not
erased; this synthetic run does not reproduce them. It does not claim
all-workload/daily acceptance, a disjoint footprint sum, mask-cap savings or
an app-RSS/25 verdict, and no GC or memory-limit policy changed. The unchanged
public `PERF_MEMORY` path still forces GC and does not reproduce C9.

## Release validation and CI

Release mode publishes the reviewed source on the pinned upstream base in
`shibu-upstream.json`. A newer upstream release is considered only by check
mode, keeping an unrelated upstream merge out of the final delivery step.
An explicit `--upstream-tag` cannot change the baseline in release mode;
review that upstream incorporation in check mode first. Every explicit
`macos` value must contain the permitted consent declaration: empty or
incorrectly typed values are rejected too. The guard approves metadata keys
and identity, while the reviewed source owns the explanatory consent copy.
Release preparation also stamps the feature tracker's release header and
Unreleased feature rows, retaining their existing hand-test status.

`scripts/shibu_release.py` permits the macOS `infoPlist` automation consent
description needed by Spotify. Its allowlist still rejects other plist keys,
signing settings and bundle identity overrides. `scripts/test_shibu_release.py`
checks the actual application configuration and invalid override cases.
`.github/workflows/test.yml` runs the same full Go, MyGo vet and Python checks
as `scripts/check.sh`, using the toolchain installed by `actions/setup-go`
rather than the local wrapper's checkout-specific `.toolchain`. The growing UI suite uses Go's
default ten-minute timeout instead of a separate two-minute CI timeout;
runtime performance budgets remain independently tested.
The complete native UI gate runs on ARM64 macOS, matching the release target,
where AppKit glass, native menu
shortcuts and the app's allocation budgets apply. A separate Linux job tests
the internal backend and plugin packages. Native UI budgets are retained,
not raised to accommodate Linux's different renderer and menu conventions.
Both jobs install the same pinned Gitleaks version as the security workflow,
because the complete Python suite exercises real Git hooks in disposable
repositories. Missing scanner dependencies cannot silently skip those tests.

## Session organization and update controls

`session_sidebar.go` owns project-local session pins, the stable partition
that puts pins first, context-menu actions, drop targets, and the conversion
from a visible position to the existing daemon `move` RPC's project index.
`app.go` applies that partition in `groups()`, so the sidebar, command palette,
number shortcuts and next/previous navigation agree. The pin indicator is the
native `pin.fill` SF Symbol. The Session menu, palette, configurable shortcuts,
and `kero do` expose `session.pin`, `session.unpin`, `session.moveUp` and
`session.moveDown`; commands accept `session=<id>` and otherwise use the
selected session.

Pins are stored separately from plugin pins in `app.json`'s `sessionPins` map.
Asynchronous preference snapshots clone that map. Session order uses the
existing daemon state and persistence, without a protocol or state-version
change. Drops and menu moves stay within the same project and pin section;
only a confirmed daemon state changes the displayed order. Failed writes use
the existing error toast. New sessions are unpinned and retain the existing insertion after the selected
session. Moving a session to
another project keeps its pin and appends it to that section. Unpinning returns
it to its underlying daemon-order position among unpinned sessions.

Pins are removed when a session observed in this daemon disappears. Unknown
pins are retained across connection and daemon changes: background restoration
sends partial session lists, so absence on connection is not proof of deletion.
A pin for a session deleted while this app was disconnected can therefore
remain dormant in the preferences; it is never displayed without that session.

The fourth footer icon is removed in `view.go`. `source_updates_settings.go`
puts its existing off-thread action in Settings > General > Updates, beside
published app update controls. While a source check runs, its button becomes a
busy label; Settings keeps the current progress and last result. Settings
search finds the action by upstream, source, GitHub, build and repair.
`updates.sync`, its palette entry, duplicate-run guard and repair behavior stay
with the existing source-update implementation.

Regression coverage: `session_sidebar_test.go` exercises the rendered menus,
drag requests over a private fixture socket, pin persistence, navigation,
section boundaries and partial restoration. `internal/daemon/session_order_test.go`
proves project-relative ordering survives daemon save/restore.
`source_updates_test.go` checks the footer and Settings idle/busy/result states.

`sidebar_drag.go` shares drag feedback between session rows and project
headers. A carried item uses tinted interactive glass with an opaque fallback
under Reduce Transparency. The placement rule is neutral paint drawn over the
row, so it changes neither the border nor the text layout. A session is chosen
on click, not press: dragging it leaves the active terminal alone. MyGo owns
the drag threshold, Escape cancellation, edge scrolling and reduced-motion
behavior. No new animation loop is introduced.

`project_sidebar.go` reuses `project.move` and its saved project order. The
whole project group is the drop target, so the marker appears outside its
sessions. Only its header is a drag source. Project menus, the Session menu,
palette, configurable shortcuts and `kero do project.moveUp|project.moveDown`
provide equivalent moves. Synthetic Other, missing ids, self drops and
unchanged placements are rejected. Group position transitions preserve the
existing collapse animation. Regression tests cover these gestures and the
existing daemon save/restore path.

## Linear plugin

The scope, setup and limits are recorded in
[Linear plugin and session links](linear-workspace.md). Secret guards and
their verification are documented in [Security checks](security.md). Home and
composer work remains separate. The installed app is not restarted by this
feature's test or build workflow.

Linear is a built-in plugin, a role of the app's binary
(`kero plugin-run linear`), with the widget `board`. It replaced the
built-in Linear screen and the Tasks pane, which were app code. The plugin
draws the issues in sections, **Current**, **Done**, **Todo** and **Backlog**,
as a list or a board, with filters and a full issue detail. It is a client of
the daemon, as the CLI is: it connects without starting a daemon, subscribes
to state, and reads the Linear workspace and the sessions from it. It
fingerprints what it shows (the workspace, the issues' update times, the
sessions' task and project) and redraws only when that changes and only while
its pane is visible. Its own saves of the pane target do not redraw it. It
writes through the daemon's RPCs (`linear.setState`, `linear.issue`,
`session.link`, a session started with a task) and opens links through the
`openURL` permission. It never reads or logs the key. The shared socket is a
privilege of built-in plugins for now; a third-party plugin would get a
host-mediated permission later.

**The pane follows the focused session.** The plugin has the permission
`focus`: the host sends `focus {session}` when the plugin starts and whenever
the selected session changes. Current puts the issue linked to that session
first.

**The sidebar row is a pin.** `prefs.Pinned` (the app's `app.json`) holds
`{plugin, widget}` pairs; the sidebar draws a row for each, with the plugin's
name and the widget's `icon`. A click focuses the plugin's pane in the
selected session or splits one in. Pin and unpin are in the pane header's
menu, Settings › Plugins, the command palette and `kero plugin pin|unpin`.
Once, the setting `linear-sidebar` (on by default) seeds the pin of the
Linear board (`migratePins`), and `prefs.PinsSeeded` records that. A saved
`show: "linear"` is ignored: the app opens the selected session.

**The Tasks view is the plugin.** The New Pane chooser keeps its **Tasks**
entry (`L`); it focuses the session's Linear pane, or splits one in. The
daemon accepts the view `tasks` in `create`, `split` and `pane.replace`, and
maps it where a pane starts (`startPane`) to a plugin pane with the Linear
target (`linearPluginTarget`), so older clients and scripts work. `kero split
--tasks` sends the same target. State version 3 (`migrateState`) turns saved
Tasks panes into the same panes once.

**Keys, menu and clipboard.** A plugin tree may set `Header` (fixed above
the scrolling page), `Keys` (up to 8 names from `KnownKeys`) and, on a
pressable part, a `ContextMenu` with one level of submenus. The host sends a
`key` event (`KindKey`) only while a pane of the plugin has the keyboard focus
and no field takes the typing, and a `context` event (`KindContext`) for a
menu choice. The Linear plugin asks for different keys in the list
(`listKeys`) and in the detail (`detailKeys`). Copy ID and Copy Link call
`Conn.Copy`, which sends `clipboard.write`; the host records `userAt` each
time it hands the plugin an event or command, and takes a write only within
`userActionWindow` (one second) of it, else refuses and logs. A move from the
Move To menu is optimistic: `withMoves` shows the snapshot with the pending
moves applied until the daemon's state agrees. `pruneFoldsLocked` drops the
fold keys of issues and groups that no longer exist (only on a real snapshot
with issues), so the pane's target does not grow without end. `fingerprint`
(`model.go`) is the hash the plugin compares to skip a redraw.

**Panes that start as Tasks.** `newPaneView` (`pane_chooser.go`) maps the
chooser's Tasks entry to the plugin pane with `linearPluginTarget`
(`pane_views.go`), so a new Tasks pane is a Linear plugin pane at once. A pane
still saved as Tasks, before the terminal service restarts and migrates it,
is shown as the Linear plugin as well. Links in plugin views must be plain
`http` or `https` without a username (`safeURL` in the plugin,
`linearSafeURL` in the app).

**What stayed in core.** The daemon's poller and its saved Linear read, the
`linear.*`, `session.link` and `project.linear` RPCs, branch auto-linking,
`internal/linear`, the key and the Linear page of Settings (key, refresh
interval), the project menu's Linear project (`projectLinearMenu`), and the
CLI.

The daemon protocol gained an issue's assignee, priority and parent, and the
workspace's viewer.

The workspace itself, unchanged: **a session and the task it works on are one
thing.** `kero link <session> <BRU-25>` binds a session to an issue; the link
is kept with the session across restarts. The daemon reads the workspace, so
the app, the plugin and the CLI see the same one. Linear's webhooks want a
public address, which a desktop app does not have, so the daemon polls: every
thirty seconds it asks what changed, and every five minutes it reads the
projects and the milestones whole. A request is bounded at two seconds, and a
workspace that cannot be read is a state shown, never a failure the daemon
dies of. The last good read is kept.

The key is a Linear personal API key, kept at mode `0600` in the personal
data directory, and read from `LINEAR_API_KEY` when that is set. It is read
per request, so replacing it takes effect at once, which is what rotating a
key is. Set it in Settings › Linear, or with `kero linear auth <key>`.

Code: [`internal/plugin/builtin/linear/`](../internal/plugin/builtin/linear/),
its manifest
[`internal/plugin/registry/manifests/linear.json`](../internal/plugin/registry/manifests/linear.json),
[`plugin_pins.go`](../plugin_pins.go),
[`internal/linear/`](../internal/linear/),
[`internal/daemon/linear.go`](../internal/daemon/linear.go), `migrateState`
in [`internal/daemon/store.go`](../internal/daemon/store.go).
Automation: `kero linear status|board|issue|set-state|comment`, `kero link`,
`kero plugin pin|unpin`, `kero split --tasks`.

**Removed:** `linear_view.go`, `linear_board.go`, `linear_detail.go`,
`linear_marks.go`, `tasks_pane.go` (and their tests), the sidebar row
`linearNav`, the `linear.open` action and `Cmd+Shift+L`, the `show: "linear"`
state of the card, `prefs.Linear`, and the settings rows for the sidebar row,
empty columns and card density. `linearSafeURL` moved to
[`safe_url.go`](../safe_url.go).

## Panes that show anything

A pane is a terminal or a view: **Tasks** (the Linear plugin's board; see
[Linear plugin](#linear-plugin)), **Files** (the folder as a tree, with files read beside it),
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
[`browser_pane.go`](../browser_pane.go),
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

A pane's own word comes from its marks. The program's own OSC 7501 report
is a record that stands until it is dismissed. The agent's hook reports and
the transcript scan are records too, but "soft" ones (`records`, `soft`):
the screen detection may overrule them when it shows the contrary (the
agent's prompt back after Esc, a permission granted), and the scan yields
to every other record on the pane. So the order is: the program's own
report, then the hooks, then the scan, each of which the screen may
overrule when it contradicts. (An earlier version of this note put the
screen last. The code never did, and keeps it so: a transcript can lag
behind the screen, which is right the moment it is read.) The fold reads
the resulting status, so only the most urgent pane's words show, and of two
as urgent, the first pane's.

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

Capability note: Codex has a permission-prompt hook
([`hook_events.go`](../internal/cli/hook_events.go)). The scan is the
fallback for Codex's approvals only when its hooks are not installed — 45
seconds after the call began, not the moment it asks. Claude Code reports
the prompt itself.

Codex transcript discovery uses `CODEX_HOME/sessions` when configured,
otherwise `~/.codex/sessions`, matching the hook installer and usage scanner.
It reads at most 256 directory entries or directory
opens for one pane in a pass (`transcript_discovery.go`). An unfinished
search continues on the next pass, so an older session beyond the first
slice remains discoverable. Only one directory descriptor stays open per
search; completing the search, closing the pane or changing the agent
session releases it. Known transcripts use only a stat and a bounded tail
read. A new agent conversation also drops the old scan-owned status and
cached verdict, so one conversation cannot inherit another's work.

The scan reads the file without the pane's lock (the lookup, the stat and
the tail), and takes it only to plan the read and to apply what it found,
checking that the pane did not change meanwhile. Passes never overlap, and
the tick loop runs one on its own goroutine. A turn's end is told once by
which end it is (the last completion's timestamp, optional event id and
absolute source byte offset), not by its words or by how many earlier ends
still fit in the tail. Two turns that end on "Done." remain distinct even
when more than 256 KB separates them. Sliding the bounded read window
does not replay an older completion. The scan marks an end unread only where another signal already
did (a transcript older than the daemon would otherwise mark every restored
pane), but it ends the work it began itself. A session's row and its
attention are folded from one look at each pane (`readPane`).

An end that comes while the app is away survives a restart: the pane's
summary is saved with its unread mark (`savedPane.Summary`,
[`internal/daemon/store.go`](../internal/daemon/store.go)) and a restored
pane shows it. Nothing else of the fold is saved; it is computed again from
the marks the daemon restored. The attention and summary fields are
optional; this completion changes neither the daemon protocol version nor
the saved-state version.

Code: [`internal/daemon/attention.go`](../internal/daemon/attention.go),
[`internal/daemon/transcript_discovery.go`](../internal/daemon/transcript_discovery.go),
[`internal/daemon/store.go`](../internal/daemon/store.go),
[`internal/daemon/osc.go`](../internal/daemon/osc.go) (`applyReport`),
[`internal/proto/proto.go`](../internal/proto/proto.go) (`Attention`).
Tests: `attention_test.go`, `store_test.go` (`TestRestoreKeepsSummary`).

## Attention navigation

A session that waits on the user can be gone to, and the next one can be
gone to after it. Moving between them is `attention.next-blocked`
(Session › **Next Waiting Session**, the command palette, or
`kero do attention.next-blocked` from an agent's shell). It has no default
shortcut: the menu, the palette and the CLI reach it, and no key is taken
from a pane's program for it.

**The cycle.** The action reads each session's fold (`foldOf`, shared with
the notifications), so it, the notifications and the attention pane see one
truth. The sessions it visits are those whose state
is `waiting-user`, ordered by `Attention.Since` ascending and then by the
session's id: the longest wait first, and the same order when two came at
the same millisecond, whatever order kerod sent them in. Nothing is saved
for the order; the app keeps the session it went to last in
`App.attentionLast` ([`attention_nav.go`](../attention_nav.go),
`nextBlocked`). A press goes after the session shown when it waits, else
after the session it went to last, else to the longest wait, and wraps. So
a press from a waiting session goes on to the next, and looking at a
session that does not wait does not start the cycle over. With no session
waiting it
is a no-op and says "Nothing needs you", as Jump to Session Needing You
does. A session that stops waiting leaves the cycle. If neither the
selected session nor the last visited session remains in the waiting set,
the next press resets to the longest remaining wait. A daemon that predates
this feature sends no attention state; `foldOf` then folds every terminal pane (waiting is
`Status == Waiting`), and such sessions have no `Since`, so they come first
and are ordered by id.

**Focus.** `focusAttention(session, pane)` is the one way to take the user
to what a session needs; the issue detail and the cycle use it. It opens the session's project in the sidebar, selects the
session, and moves both the app's focus and kerod's (`focus`) to the pane
the attention came from; the pane's terminal then takes the keyboard, as
the app's pane focus does (`setFocus`). A pane named that has closed since
the state came falls back to the pane the session was in last; that fallback
uses the same idempotent daemon focus request and local zoom update. A stale
linked session rejected by the daemon produces the Linear pane's existing
error notice. If it disappears after validation, the app shows a localized
session-not-found message.

A zoom follows the focus, exactly as it does when panes are cycled while
zoomed: when the session is zoomed on another pane, the zoom moves to the
named pane. That is what keeps the jump from landing on a pane the zoom
hides, and the session stays zoomed on the pane the user came to see
rather than dropping to the whole layout. The focus request also reaches kerod when the local pane already has
focus. The daemon moves its actual zoom with that focus, without a toggle
based on a possibly stale client snapshot. A row click explicitly clears
zoom with the existing empty-pane `zoom` request, then uses the same focus
path to reveal the full layout.

**Linear round trip.** A session linked to an issue shows in the issue's
detail, and the detail's **Open the session**, like a running
conversation's **Open**, now goes through `focusAttentionOf(session)`: the
session is selected, and when it waits on the user the pane its attention
came from is in front. A session at rest keeps its focus and zoom, since
the fold names its first pane then, which needs nobody. The other end is
the attention pane's **Open issue** button (`attentionIssueButton` in
`attention_pane.go`), shown on a row whose session has a `TaskID`: it
opens the issue in the Linear plugin (`openAttentionIssue`). An existing
Linear pane in the selected session keeps its filters; otherwise a new pane
is split with the issue in its saved target. The row's own click is skipped for that press. Nothing
new is stored: both ends read the link the daemon already keeps for the
task (`session.link`, the panes' `TaskID`, `savedPane.Task`), and the app
keeps no link of its own.

Code: [`attention_nav.go`](../attention_nav.go), `actions.go` (`run`),
[`internal/actions/actions.go`](../internal/actions/actions.go),
[`app.go`](../app.go) (`session.attention` action from the Linear plugin).
Tests: `attention_nav_test.go` (cycle order and wrap, no session waiting,
a zoomed session, a closed pane, the issue detail's jump, a session at
rest, a daemon with no fold); `attention_pane_test.go`
(`TestAttentionRowOpensIssue`).

## The Attention pane

The **Attention** pane shows what needs the user, of every session, in one
list. It reads `Session.Attention` as kerod sends it and holds no state of
its own beyond the list: it runs nothing in the daemon. When a supported
kerod omits the folded state, the pane derives it with `foldOf`, just as
navigation and notifications do. The pane view itself is new to the
protocol, though: `"attention"` joins `proto.PaneViews`, and a kerod
that predates it refuses the pane (`internal/daemon/sessions.go`), so the
terminal service needs a restart once, when agents are idle. Its own
state — the filter, the search, where it is scrolled to — is stored as
JSON in the existing pane Target (`attentionTarget`, `attention_pane.go`).
The app keeps one live view per pane and prunes it when the pane closes.
Changed state uses `pane.target`; one write is in flight at a time and its
completion sends only the newest state that arrived meanwhile. A rejected
target stays dirty without automatic per-frame retries; changed input or a
new daemon connection permits another attempt. The daemon coalesces its
saved-state writes. A restart restores the view; invalid
filters or scroll positions fall back to the default.

The pane is a view as Tasks, Files and
Browser are: kerod keeps it and its Target across a restart.

A row says the state's mark and name, the session's name and its project,
the agent, the line the state came with, and how long it has been so
(`ago`). Rows are the sessions of the sidebar's order, the most urgent
first — `waiting-user`, `running`, `unread`, `done`, the daemon's
order — and of two as urgent, the one whose state came last. A click
selects the session and puts the keyboard in the pane the state came from
(`Attention.Pane`), where the decision is; it does not zoom, and it ends a zoom
on another pane, so the panes beside it stay in sight.

The chips narrow the list to a state and count what each shows: **All**,
**Needs you**, **Working**, **Unread**, **Done**. The copy is the app's
own, as the sidebar and the footer say it: `waiting-user` is **Needs
you**. Idle sessions, at rest, need nothing and are not listed.
The search finds what a row says — its name, project, agent, message and
state — as plain text, and Escape clears it.

An approval shows the hook's own words. Codex supports a permission hook;
when it is not installed the transcript scan is its fallback. Every Codex
wait appends **Focus pane to approve**, including a daemon's generic
message, because the approval is answered in the pane.

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
`checkmark.circle.fill`, `checkmark.circle` and `circle`. Mapped control and status icons use native SF Symbols; provider logos remain
artwork. Tests: `symbols_test.go`.

## Notifications from attention

Notification throttling starts only after native delivery acceptance. Native
authorization stays off the UI thread, with one notice per session/state in
flight and only the newest eligible event queued. Retired-connection results
cannot charge the new watch. The app removes saved pins and silence only
after the daemon successfully acknowledges a session close, using the existing
serialized writers.

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
  Center. Only a notification that was shown starts the throttle: an ask
  the user saw in the focused session, or one a setting turned off, holds
  nothing back. A session that asks again less than 30 seconds after it
  left the state is a loop; one that asks again later (a real second ask
  after an approval) tells even inside the window;
- a clean exit of a plain program, and a view pane's unread mark, tell
  nothing; an exit with a non-zero code is a failure;
- after kerod restarts and the app reconnects, the watch starts anew:
  panes kerod brings back idle and unread are not news;
- an unseen end whose pane failed keeps the error's title and body, and
  `notify-on-error` decides it; `notify-on-waiting` and `notify-on-done`
  decide the rest, as they did. The sound, the Dock badge and the bounce
  stay as they were; the badge counts the sessions whose fold waits
  (`waiting-user`), not a pane's status.

Focus is the only filter: the session the user is in, in a window that is
in front and has the keyboard, tells nothing, and neither does a zoomed
session's pane in sight (another pane of it that asks does tell). A click
on a notification selects its session and moves into the pane it comes
from through `focusAttention`, so a zoom follows the asking pane.
The watch itself knows nothing of windows or
profiles — it is handed every session the daemon holds, so a session in a
profile the user is not looking at still tells. That is the global rule
the profiles of `feat/profiles-a1` will need; no profiles code is built
here, and a later filter must not hide a session's attention from this
watch.

A session can be silenced: **Session › Silence or Resume Notifications**,
the session's context menu, or `kero do session.silence [session=<id or
name>] [state=on|off]` turns its
notifications off until the user resumes them. The list is saved with the
settings (`silence-session`) and survives a restart of the app. A choice
is removed only when a session already observed in the current daemon
epoch disappears. Unknown or late-restored ids remain through partial
startup snapshots and daemon restarts; absence alone is not closure. Without
`session=` the action is about the selected session, and without `state=`
it toggles. A silenced
session tells nothing; once resumed, its next turn tells, inside the same
throttle window. A daemon of before the `Attention` field says none, so
`foldOf` folds all terminal panes, so a wait in a non-lead pane still
notifies. View panes and plain successful shell exits do not notify.

What a notification says is the session's name and, for a wait or a
failure, one line of what it needs: the fold's `Msg`, which is the hook's
or the screen's line (or the name of the tool call a wait hangs on), cut to
80 bytes by kerod. A session's end never uses the fold's line: kerod can
fold it from the agent's own transcript (the last message of a Codex turn),
and what an agent wrote is not for a notification, so an end says the agent
and the folder. The transcript scan is kerod's existing one; the
notification does not read more of it. A daemon of before the `Attention`
field is read pane by pane, and the most urgent pane wins.

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
[`files_pane.go`](../files_pane.go). Test: `loading_ui_test.go`
(the ring's steps, Reduce Motion, and no frames once it has gone).

## Account usage

Claude and Codex use existing CLI login credentials. OpenCode Go uses its
workspace key; DeepSeek shows direct API balance. These are provider-specific
allowances and balances, not a combined subscription budget. Reads run in
the background every two minutes, preserve the last successful observation
after errors, and respect rate-limit backoff. Explicit provider-key overrides
are atomically stored with mode `0600` in the personal data directory.

The navigation's usage icon opens the Usage plugin's large list in a
transient popover: Claude, Codex, OpenCode Go and DeepSeek, with every
allowance window, its percentage or the provider's balance, its reset time,
the plan, the cost ranges and the app's CPU and RAM. No circular meters
remain. Hover opens the card, a click pins it, the icon again or a click
outside dismisses it, and Escape closes it while it has the keyboard. The
pointer may cross the gap into the card, and the trigger takes the keyboard
back after a keyboard dismissal. The card flips and clamps at the window's
edges and scrolls in a short window; it always draws the large list rather
than the pane's compact form. `Open in Pane` opens a normal Usage pane.

`nav_usage.go` owns the popover and its transient plugin surface; the shared
host lifecycle is described under
[Navigation placements](#navigation-placements-tabs-and-the-usage-popover).
Closing the popover sends `close` for `@nav/usage` only, so a visible Usage
pane keeps its own refresh. The plugin's refresh button and
`kero do usage.refresh` reach its own loop. A failed reading keeps the last
successful observation, and the plugin's tree draws stale, disconnected and
missing-provider states; a plugin that stops shows its reason and Restart,
through the same view a pane uses. The transient surface never writes a
daemon pane target.

The sidebar's older list, row and hover card were removed with the popover's
arrival; `usage_test.go` now covers the modal and the app reader, while
`internal/plugin/builtin/usage/tree_test.go` covers the list's large and
compact forms, allowance states, costs and reset labels. `navigation_test.go`
covers the host transfer, pinning, outside click, Escape, missing plugin,
crash/restart, coexistence with a real Usage pane and the transient state
path.

The modal shows allowance windows, reset times, refresh and provider
configuration, and its own error and connection states. `Cmd+Shift+U` opens
it; `Cmd+R` refreshes while open; Esc/Cmd+W closes it. There is no extra tab
or window. The app's reader remains in `usage.go` for this modal; the popover
uses the plugin's reader instead of a second renderer.

Code: [`usage.go`](../usage.go), [`nav_usage.go`](../nav_usage.go),
[`internal/usage/`](../internal/usage/).
Automation: `kero usage --json` and `kero do usage.open`.
Tests: `usage_test.go`, `navigation_test.go`, `internal/usage/usage_test.go`.

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

macOS Mach calls sample every ten seconds while focused, in the background without shell
processes, and every 30 seconds while the window does not have the focus
(`resourceWait`); a window that gets the focus back reads at once
(`resourcePace`, BRU-336). CPU covers all cores; RAM includes active, wired and compressed
physical memory, excluding reclaimable cache. These are whole-Mac readings,
not Shibu process usage or memory pressure.

Code: [`resources.go`](../resources.go),
[`internal/resources/`](../internal/resources/).
Automation: `kero resources --json`.

## Idle repaint floor

An idle window should not paint. Three timers that built and painted the
whole window, grain shader included, with nothing to show are cut back
(BRU-225, in part):

- **The CPU and RAM sampler** reads every ten seconds (`resourcePeriod`),
  off the main thread, but calls `a.update` only when `showResources` — the meters'
  labels, the tooltip's and the usage card's, the bars' fills and the
  error — differs from what it last sent. The bars fill in whole percents
  (`cpuPercent`, `memoryPercent`), so what is compared is what is drawn; a
  repeated error sends nothing either. On a Mac at work the labels change
  with nearly every read (30 of 30 reads at 2 s on an M4 Pro running
  agents), so the period is what bounds the meters' rebuilds: 30 a minute
  at 2 s and 12 at 5 s previously, now at most six per minute at 10 s.
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

The cursor blink is handled by MyGo's terminal plugin, pinned to
`81afe923`. It paints the last frame again every 600 ms without rebuilding
the window, then stops 10 seconds after the last key or focus change and
rests shown. The final paint is scheduled at the timeout even when it
falls in the hidden half of the blink. A key or focus change starts it
again. The no-blink setting and the hollow unfocused cursor keep their
behavior. The implementation is in MyGo's `plugins/terminal/view.go`
(`blinkTimeout`, `blinking` and the cursor painter); `blink_test.go`
checks the phase, timeout, restart, and a minute without builds or paints
after the timeout. Whole-app frame and wakeup measurements, including
their visibility and focus limits, are in [Performance](perf.md).

On `03a94a6`, visible and unfocused, the combined idle cost was 790 and
1,083 wakeups/min at four panes, 1,196/min at one pane, and 1,494/min at
eight panes, within the
agreed 2,000/min budget. The earlier four-pane unfocused baseline was
3,068–3,577/min. Rebuilds were 3–4/min and streaming frame-build p99
1.0–1.8 ms. The app's footprint with one pane shown was still
121.8–254.5 MB, above its 30 MB budget. These samples do not establish
focused idle cost; no focused run kept its focus throughout sampling.


Code: [`resources.go`](../resources.go), `agoTick` in
[`view.go`](../view.go), [`attention_pane.go`](../attention_pane.go),
[`panes.go`](../panes.go). Tests: `idle_repaint_test.go` (what the sampler
compares; the window builds about twice in two idle minutes for the
sidebar, never with it hidden, and the Attention pane still ticks).

## Prefs and settings written off the main thread

A busy disk, as twenty-five agents building on one machine, made ordinary
actions wait: switching sessions, dragging the sidebar's edge or collapsing
a project wrote `app.json` on the main thread, and Settings wrote the
configuration there too. Both now write in the background (BRU-335).

`writeBack[T]` ([`write_back.go`](../write_back.go)) is one value's writer,
by type: `Set` keeps the newest value and returns, and one goroutine writes
it after `writeDelay`, 500 ms. A burst of changes, as a drag makes, costs
one write of the last value; a value that is what the disk holds is not
written at all. `Flush` writes a waiting value before it returns. It holds
no file of its own: a caller gives it a `save` func.

- **Prefs.** `(*App).savePrefs` copies the prefs with maps of their own
  (`snapshotPrefs`, `clonePrefs`), so nothing mutable leaves the main
  thread, and hands the copy to `a.prefsOut`, whose `writePrefs` marshals
  and writes as the file always was: `app.json.tmp`, then a rename. When
  `loadPrefs` read the file, it seeds the writer with those prefs (`Seed`),
  so a session switch that changes nothing writes nothing.
- **Settings.** `(*App).apply` hands a clone to `a.setOut`, which calls
  `settings.SaveAt` on its goroutine. The path is taken in `newApp`
  (`settings.Path()`), not by the writer, so a write does not depend on the
  environment at the time it runs. A failed save still shows the toast, from
  `a.update`. `ensureConfig`, which writes the file before the editor opens
  it, saves and flushes for the same reason.
- **A reload while a write waits.** `reloadConfig` reads the file and hands
  it to `applyConfigFile` on the main thread; while `a.setOut.pending()`, it
  keeps the app's own newer value, whose write fires the watch again when it
  lands, rather than flicker the window back to the file's older one each
  time a step of a setting is written.
- **Quitting.** `mygo.App.OnBeforeQuit` calls `(*App).beforeQuit`, which
  saves the prefs, then flushes the prefs and the settings: a change made in
  the last half second is on disk before the app goes.
- **Image peeks.** `imagePeek` resolved the path (`os.Stat`) and stat'ed it
  again on every frame a hover showed, so a resting pointer asked the disk
  every frame. `peekLook` ([`peek.go`](../peek.go)) keeps the hovered pane
  and link with the path and file it found, so one hover stats once.
  `resolvePath` keeps the stat for a click (`openLink`); `pathFor` is the
  name without it. One hover no longer notices a file that changed under
  the pointer: the link has to change, or the pointer leave and come back to
  it, for the preview to read the file again.

Measured on a warm cache with a probe of 200 calls (not committed), on the
calling thread: `savePrefs` 135 µs → 53 ns, `applySettings` 155 µs → 0.8–2.3
µs. Ten `savePrefs` in a row: 10 writes → 1. A save of unchanged prefs: 1
write → none. A hover read the file 2 stats a frame → 1 stat for the hover.

Code: [`app.go`](../app.go) (`savePrefs`, `snapshotPrefs`, `saveSettings`,
`beforeQuit`), [`write_back.go`](../write_back.go),
[`peek.go`](../peek.go), `SaveAt` in
[`internal/settings/settings.go`](../internal/settings/settings.go). Tests:
`prefs_test.go` (a burst writes once, holding the last value; unchanged
prefs write nothing; a quit writes what waits), `TestImagePeekStatsOncePerHover`
in [`view_test.go`](../view_test.go), and
`TestReloadDuringAPendingSaveKeepsTheNewer` in
[`settings_test.go`](../settings_test.go).

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

The images programs show are bounded the same way, without a setting.
`settings.ImageLimitBytes` ([`internal/settings/scrollback.go`](../internal/settings/scrollback.go))
is 32 MB, and both `termOptions` in [`app.go`](../app.go) and kerod's
`(*pane).termOptions` in [`internal/daemon/session.go`](../internal/daemon/session.go)
give it to `terminal.Options.ImageLimit`; a 0 there is MyGo's 320 MB for
each of a pane's two copies, which twenty-five panes would add up to. Tests:
`TestTermImageLimit` in [`term_budget_test.go`](../term_budget_test.go) and
`TestPaneTerminalImageLimit` in
[`internal/daemon/term_budget_test.go`](../internal/daemon/term_budget_test.go).

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

## State broadcast

kerod used to send the whole state, the Linear workspace included, to every
subscriber for every change, and the app rebuilt its window for each: a
spinner in a pane, which renames its title many times a second, made both
happen (BRU-332).

- **The workspace has an event of its own.** `proto.Message.Linear` carries
  it, on an event `linear`, and nil means the daemon has none, as
  `State.Linear` was nil then (`d.workspace()`;
  `linearState` still answers `no_token` for the `linear.state` call).
  `d.state()` ([`internal/daemon/sessions.go`](../internal/daemon/sessions.go))
  keeps the workspace for a client that asks once, with `kero task`,
  `kero hooks` and the CLI's own reading; `d.broadcastState()` leaves it out.
  `sendLinear` ([`internal/daemon/daemon.go`](../internal/daemon/daemon.go))
  compares the payload, its task links included, with the last one sent, and
  sends it only when it differs; a subscriber gets one as it subscribes
  ([`internal/daemon/rpc.go`](../internal/daemon/rpc.go)), and that records
  nothing, so a change that landed before it still reaches the others on the
  next broadcast.
- **Every subscriber puts the two back together.** The app keeps the
  workspace a `linear` event gave it, and a state keeps it (`setState` in
  [`app.go`](../app.go)); the Linear plugin does the same in
  `events.handle`, in
  [`internal/plugin/builtin/linear/daemon.go`](../internal/plugin/builtin/linear/daemon.go),
  where the pane reads `st.Linear` as it did when every state carried it.
- **A dropped workspace is sent again, and counted.** `(*controlConn).event`
  counts what a client too far behind cannot take, in `(*Daemon).dropped`
  (`dropEvent` says the count in the log at most once a minute; `scripts/perf.sh`
  has no query for it). A state lost loses nothing, since the next replaces
  it, but a `linear` event would be lost until the workspace changed: so it
  also clears `linearKnown` (`forgetLinear`, under `linearOutMu` while `d.mu`
  is held), and the next broadcast sends the workspace to all of them.
- **A title change broadcasts and does not save.** `(*Daemon).broadcast`
  signals `changeCh` alone, and `changed` is `broadcast` plus `save`; every
  other caller keeps `changed`. `(*pane).onTitle`
  ([`internal/daemon/session.go`](../internal/daemon/session.go)) uses
  `broadcast`: the saved state has no titles, and a spinner wrote
  `state.json` for each of its frames. No other caller was switched.
- **The app drops a state it already shows.** `stateSeen.stale`
  ([`app.go`](../app.go)) keeps the last state the client took, on the
  client's goroutine where the messages come, and an event equal to it
  never reaches the main thread, so `a.update` and the build it asks for do
  not happen. No other caller of `a.update` changed.

Measured with a probe of the daemon, 25 sessions of two panes and a
workspace of 120 issues, per subscriber: a broadcast state is 14,581 B
without the workspace and 33,986 B with it, the workspace alone 19,395 B.
Twenty-five broadcasts of a title storm cost 849,650 B before and 383,920 B
after (25 × 14,581 B, and the workspace once): 55% less, with the workspace
on one message of the 25 instead of all of them. A title change saved the
state before, and saves nothing after.

The protocol is `proto.Version` 4: an older app refuses a newer kerod and
offers to restart the terminal service, and a newer app refuses an older
kerod the same way. The service needs that one restart after updating.

Tests: `internal/daemon/broadcast_test.go` (a storm of 25 titles sends the
workspace once, and once more for a workspace or a task-link change; a
subscribe gets it once and hides nothing from the others; a state asked for
once carries it; no workspace is nil; a workspace dropped is sent again; a
title change saves nothing; a dropped event is counted),
[`state_event_test.go`](../state_event_test.go) (an identical state is
dropped before the window; the workspace arrives on its event, and a state
keeps it, or nil when there is none) and
[`internal/plugin/builtin/linear/daemon_test.go`](../internal/plugin/builtin/linear/daemon_test.go)
(the plugin is handed the workspace on its own event, and on the states
after it).

## Daemon idle work

kerod's tick runs every 400 ms while anything happens in a pane, and every
10 s once nothing has for 10 s (`tickLoop` in
[`internal/daemon/daemon.go`](../internal/daemon/daemon.go), see the pace
below); what it does for each pane is now bounded
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
  1.6 s of ticks (`branchCheck`), or each slow tick, `branchTick` stats the HEAD files that the last scan
  read; it reads the branches again when one changed, every 30 s, and when
  `branchWatch.due` is set: by the `focus` and `view` requests and by a
  pane whose agent, directory or program directory changed. git replaces
  HEAD with a rename, which changes its time, so a switch is seen within
  2 s while the panes are busy, and within 10 s once they are quiet. A
  switch made in a pane prints to it, which makes the tick fast again.

**The pace (BRU-336).** Each tick woke kerod a few times (the timer, the
Go runtime's sysmon, and a sysmon wake for each syscall of the tick), so
the fixed 400 ms tick alone cost about 1,000 wakeups a minute at rest. The
tick now waits `tickFast` (400 ms) while something happened within
`tickQuiet` (10 s), and `tickSlow` (10 s) after (`tickPace`):

- `stir` marks that something happened: `output`, every frame an attached
  client sends (a key, a resize, a theme), and every control request
  after it is handled. It wakes a slow loop at once through `tickWake`.
  While the loop is fast, a stir costs two atomic loads, so the output
  path stays cheap. The loop and `stir` set `stirred` and `tickSlow` in
  opposite orders, and each checks the other's flag, so a stir is never
  lost between a tick and its wait.
- A pane that waits out `idleHold` (its `idleFrom` is set) reports
  itself busy from `tick`. A kerod with no session and no client stays
  fast too. Both keep the fast pace, so the status hold and the 30 s
  idle exit keep their timing.
- The transcript scan (`scanEvery`, 4.8 s) and the HEAD check
  (`branchCheck`, 1.6 s) run by time, not by tick count, so a slow tick
  does both.
- `tickSlow` is no longer than `cwdEvery(false)` and `fgRecheck`, so a
  quiet pane that the user is not in is read as often as before. A quiet
  pane loses only what changes with no output and no request: a program
  started silently, a branch switched outside Shibu, a transcript written
  without screen output. It sees these within 10 s, not 0.4 to 4.8 s. A
  quiet focused pane reads its directory every 10 s, not every 1.5 s, but
  a `cd` prints a prompt, which makes the tick fast again.

The histories went to disk every 10 s, changed or not, which cost a timer
fire of its own. Now `historySoon` (from `output` and `clear`) starts a
10 s timer (`historyWait`), and `saveLoopWith` writes the histories when
it fires, and once 10 s after start, for the restored panes. `histArmed`
makes the first change ask, and the rest cost an atomic load until the
save, so a pane that streams output does not wake the save loop for each
read. A history is
still at most 10 s old after a crash, and shutdown still saves it whole.
Tests in `internal/daemon/idle_test.go`: `TestTickPace`,
`TestTickLoopStir` (a slow loop wakes on a stir and goes slow again; no
queued wakes) and `TestHistorySaveOnChange`.

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
  `saveHistories` (10 s after a change) and `attachment.send` with
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

The [empty app and daemon baseline](perf.md#empty-app-and-daemon-baseline)
records C17's production-mode, pre-lazy-grain fixture with zero sessions and
terminals. Four region snapshots were bracketed by kernel RSS/footprint
samples, with empty inventories and independently observed window state.
App RSS was 139.234375–139.296875 MiB and daemon RSS was
39.046875–39.281250 MiB. These absolute empty-process diagnostics have no
terminal denominator, so they cannot establish the per-pane 30/25 MiB
budget pass/fail. The existing budgets still apply actual 1/4/8/12-pane
workloads, and BRU-222 remains open. This separates empty-process overhead
from scrollback; it is not a claim that the remaining overhead is
unavoidable, a retained-terminal acceptance result or a measurement of the
lazy-grain candidate GUI. The private identity/metadata seam also prevents
byte-identical shipping-release claims.

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

**Focus follows the accent.** [`borderless`](../elevation.go) gives the
shared theme its accent and selection; it also sets `Theme.Focus`, the
color MyGo's `FocusRing` paints around generic controls and sliders. A
preset such as Plum or Sea therefore tints the keyboard focus ring, not
only the custom field outlines that already use `pal.accent`. The ring
takes the followed theme's alpha, so the desktop's usual 55% light / 60%
dark ring and Increase Contrast's full-opacity ring both survive; in the
dark the accent is lightened 20% as MyGo's own focus color is. The focus
regression in `focus_ring_test.go` focuses a shared slider with Tab and
reads the rendered ring in light and dark for Plum, Sea and the default
Blue, checks that Increase Contrast is fuller in both appearances, and
switches accents frame to frame to prove no theme or palette cache keeps
the old one.

**The grain.** [`grain.go`](../grain.go) holds one fragment shader of
package `ui/shader`: a hash of each two points in a repeating 300-point tile, half the
specks white and half black, from no image and no time. `backdrop` draws
it on the backdrop row after its background and before its children, so it
lies over the wash and under the sidebar and the cards, at 3.3% in the
light and 4% in the dark at a speck's strongest — an average of some
1.8% — and about 30% lighter where `Vibrancy()` has the desktop showing
through,
which has a texture of its own. `grain = false` turns it off.

The shader is held by `sync.OnceValue` in `grain.go`. `runApp` in `main.go`
prewarms it after role dispatch and the single-instance check, before the
first window. The CLI, `__kerod` and `plugin-run` return through their own
dispatch paths without compiling this UI backdrop or initializing its Metal
precompiler device. A software tester that draws a backdrop directly obtains
the same cached shader through `backdrop` in `theme.go`. The shader source,
strength and setting are unchanged; compilation stays out of an already
running GUI frame. This does not change MyGo's shader compiler or GPU policy.

The grain pixel regression in `grain_test.go` checks the tile and the
2–4% maximum strength. A light/dark backdrop regression also switches grain
off, on, off and on across frames, checking that both the wash and repeated
tile return unchanged. These are software-rendering checks, not an installed
GUI or native compositor verification. `sidebar_motion_test.go` verifies that a project of
100 sessions snaps to its collapsed or expanded state with Reduce Motion.
Its software-tester benchmark measures collapse operations; it is not a
measurement of the native GPU's frame rate.

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

**Empty states that teach.** Replaced: the built-in Linear screen and Tasks
pane, which had these empty states, left the app. The Linear plugin draws its
own empty states (not read, no issue in the scope, filtered to nothing), with
the actions Refresh Linear and Clear Filters. The error copy around
them says what went wrong and, where there is one, the next step: the Linear
key goes to Settings › Linear, the browser asks for the address again, a file
or folder that cannot be read checks that it still exists and may be opened,
settings that cannot be saved check that the configuration folder is writable,
and a failed CLI install gives its error, on every platform.

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

## Work profiles

A profile is the unit of switching between hats: it bundles what is on
screen, which panes and projects run, the look, and — later — an
environment bundle. This child is the saved-state ground only: the
switching itself (apply, RPC, switch UI, CLI verbs, env bundles and their
trust gate) is the work that follows.

**A profile is a thin bundle of references, not a copy.** `savedProfile` in
[`internal/daemon/store.go`](../internal/daemon/store.go) holds:

| Field | Meaning |
| --- | --- |
| `id`, `name` | the profile's identity, as the switcher shows it |
| `accent`, `background` | the appearance presets (Settings' `accent`/`background` ids) it puts on |
| `projects`, `sessions` | the ids of the live projects and sessions it brings forward |
| `envRef` | the name of its environment bundle, landed by the env child; the values are never saved here, so nothing secret is in the file |
| `createdAt`, `lastUsed` | Unix milliseconds, as the file's other times |

Membership points at live state and is resolved when the profile is
applied, at switch time; a reference that no longer resolves is skipped,
with a note, and never fails the restore. No session is ever duplicated:
the per-pane `View/Target/Dir/Command/Agent/Task` state already saved with
each session carries the layout.

kerod keeps the profiles in `state.json`'s `profiles`, and the id of the
one last active in `activeProfile` (empty when none); the daemon keeps them
in `d.profiles`/`d.activeProfile`. `restore` loads them even when session
restore is off, and `saveState` writes them in the same single
`writeAtomic`: the whole state is marshalled once and renamed over the
file, so a crash mid-save leaves the old file or the new one, never a
half-applied profile. The fields are optional and left out when empty, so
a state file of before loads as it always did. The saved state's version
stays at its current value, 3, and the protocol's stays 4: applying a profile (the next child)
adds new RPC methods, and new methods never bump a version — the target of
zero wire change in this child is met.
`TestProfileStateRoundTrip` in `internal/daemon/store_test.go` proves the
round trip, that stale references load, that the old file loads, and that
this addition does not change the current version.

**This is the seam a future plugin API reads.** Profiles are daemon data
with no wire type of their own: a plugin API can list, save and apply them
over the daemon's RPC surface by adding methods and optional fields, never
a version. Keep the profile additions co-located in `store.go` — the
`savedProfile` type and the two `savedState` fields — so other children
that also extend the saved state rebase trivially.

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

## Startup and restore

After a terminal service restart, the app connects before the saved sessions
finish restoring (BRU-229). This uses the existing pane and session model;
there is no protocol or saved-state format change.

- `Run` in `internal/daemon/daemon.go` reads a restore plan, starts the accept
  loop, then restores sessions in a background goroutine. The initial
  transcript scan runs only after that restore returns; `scanPass` in
  `internal/daemon/attention.go` takes a snapshot and applies results under the
  pane lock, while transcript disk reads run without it.
- `restore` in `internal/daemon/store.go` loads the state, workspace, projects
  and task runs. `restoreInBackground` uses up to four workers. History feeds
  and shell forks run outside `d.mu`; `restoreRun.publish` inserts a whole
  session and its panes under that lock, then announces the change.
- `restorePlan.place` supplies saved order. Insertion counts only sessions
  still present, so failed or closed sessions cannot cause an out-of-bounds
  insertion. Sessions created during restore stay after the restored ones, including
  requests to create one after a specific session.
- While `restoreDone` is open, `saveState` skips writes, including shutdown
  saves, and the idle timer cannot exit. History saves visit only published
  panes, leaving histories of panes still restoring untouched. The restore
  queues a final save after closing the gate.
- After workers finish, task cleanup uses their immutable results. It keeps
  resumed agents and restored session stand-ins, ends other unchanged runs
  from the saved state, and leaves conversations created or resumed by clients
  during restoration alone. With session restoration disabled, projects and
  workspace still load and the previous daemon's runs end.

Tests in `internal/daemon/restore_test.go` cover socket access, real daemon
startup and shutdown, saved order, client-created and closed sessions, partial
and total failures, save and idle gates, task cleanup, worker limits and scan
locking. Existing store and migration tests still wait for the full restore.

An isolated three-run probe with eight terminal panes, each with 4 MB of
history and `/bin/sh` (`CGO_ENABLED=0`), measured from restore start to the
first answered control `state`: median **107.58 ms** with serial restore
before accepting clients, versus **0.47 ms** with background restore. Ranges
were 104.30–180.38 ms and 0.38–11.12 ms respectively. This excludes process
launch and state-file preparation; the probe was removed after verification.

## Plugins

Shibu Code moves towards an app where every capability is a pane or a plugin.
The plugin platform is the first step: a plugin runs as its own process, and
the app draws its widget in a pane beside terminals and agents. The app itself
uses the same contract for its built-in plugins, so a built-in plugin can move
out of the binary later without a second API.

**A plugin is a manifest and a process.** The manifest is `kero-plugin.json`:
`id`, `name`, `version`, `description`, `run` (the argv that starts the
plugin), `widgets` (each with a name, a title and a least size), `commands`,
`permissions` (`secrets`, `notify`, `openURL`) and optional `settings` fields.
The id must match `^[a-z0-9][a-z0-9-]{1,40}$`; widget names and command ids are
unique. Built-in plugins are roles of the app's own binary: their `run` is the
app's executable with `plugin-run <id>`, and their manifests are embedded in
it. An installed plugin's code lives in `plugins/<id>/` under the Shibu data
folder; the app reads it but, for now, does not install one. Its settings and
secrets live apart, in `plugin-data/<id>/`, so that a later install that
replaces the code neither wipes them nor brings its own.

**A plugin pane is a normal pane.** Its view is `plugin`, and its
`proto.Pane.Target` is the JSON
`{"plugin":"<id>","widget":"<name>","state":<any JSON>}` (`EncodeTarget` and
`DecodeTarget` in the protocol package). A plugin's `state.save` updates that
target through the existing `TargetParams` call, so the pane comes back after a
restart with the same plugin, widget and state. No field was added to
`proto.Pane`; the only daemon change is the `plugin` entry in
`proto.PaneViews`, which an older terminal service answers with
`no view "plugin"`. The app catches that and shows one line asking the user to
restart the terminal service, never the raw error.

**The protocol is JSON-RPC 2.0, version 1, one message per line** on the
plugin's stdin and stdout, with a 4 MB line limit in both directions. The app
sends `hello {protocol, host}` first; the plugin answers `{protocol}` and the
app refuses any other version. The app then sends `open {pane, widget, state,
size}`, and later `close`, `resize`, `visible {bool}`, `theme {dark, accent}`,
`settings {values}`, `event {pane, node, kind, value}` and `command {id}`. The
plugin sends `render {pane, tree}`, `state.save {pane, state}`, `secret.get`,
`secret.set`, `notify` and `openURL`, and the app answers each one. Every
plugin-to-host call is checked against the manifest's permissions before the
app acts on it.

**The app hosts one process for each plugin.** It starts the process when the
first pane of that plugin opens and stops it five seconds after the last pane
closes. All of the host's I/O runs off the UI goroutine and results reach the
window with `win.Update`. A plugin that crashes shows an error and a
**Restart** button, with no restart loop; one that stops reading its input is
stopped after five seconds rather than blocking the app. The plugin's stderr
goes to `logs/plugins/<id>.log` under the Shibu data folder, rolled at 1 MB.

**The host draws the parts; the plugin sends a tree.** The tree is validated
before it is drawn: known node types, unique ids, known color roles and icon
names, a depth limit and a node limit. Version 1 has rows and columns, text
with a role (title, body, caption, mono) and a color role (text, muted,
accent), images (a URL or a `data:` URI) with a size and radius, buttons and
icon buttons, sliders, progress, lists of rows, a search field, an empty state,
spacers and dividers. A part has an `id`; an event returns that id. The host
keeps focus and scroll with the part id across renders, so a pane that redraws
does not lose the user's place. Images are fetched and cached off the UI
goroutine, a bounded number at a time. A progress part with a `rate` and an
`at` moves itself from that rate, without the plugin sending every frame. Two
parts go further: `text.marquee` scrolls only when the text overflows (pause,
scroll, pause, reset; a static ellipsis under Reduce Motion), and `image.tint`
samples the picture's dominant color once, draws a soft gradient behind the
card, crossfades it, and clamps the lightness for contrast in light and dark.

**Commands.** Each manifest command is a command palette item and the
`kero do plugin.<id>.<command>` action (`ParsePluginCommand` in
`internal/actions`; `kero do` and `kero actions` read the known plugins with
`registry.Known`). Plugin commands are not in `actions.All`, because they come
and go with the plugins. The app sends a command to a pane of its plugin: the
focused pane of the session shown, then another of its panes, then a pane of
another session (`pluginPaneOf`). A pane that is not open on the process yet
opens for it. With no pane, the command opens the plugin's first widget in the
session shown and waits in `pluginState.pending` until that pane's `open`,
which the host sends first. A crashed plugin is not started by a command. A
plugin's `notify` is a notification whose click focuses a pane of the plugin,
found at the click (`showPluginNotification`).

**Settings and secrets.** Settings › Plugins lists the built-in and installed
plugins with their version and their permissions, and draws each plugin's
settings fields from its manifest, with `help` under the label and an
**Open Link** button for an http or https `link`; no plugin is named in the
code. The fields are made only after the saved values are read off the UI
goroutine, and closing Settings drops them, so a field never shows empty over
a saved value. A field with `secret: true` is written to
`plugin-data/<id>/secrets.json` (file `0600`; the folder is made, or closed,
to `0700`); the other fields go to `settings.json` in the same folder. The app sends the non-secret values to the
plugin in the `settings` message on start and after every change, and never
sends a secret value: the plugin reads its own secrets with `secret.get`. Each
plugin can read only its own files.

Code: [`internal/plugin/protocol/`](../internal/plugin/protocol/) (the
contract: manifest, messages, line framing, target, tree validation),
[`internal/plugin/sdk/`](../internal/plugin/sdk/) (the plugin side: the
handshake, the run loop and the part builders), [`internal/plugin/host/`](../internal/plugin/host/)
(one process per plugin, permissions, logging, crash and restart),
[`internal/plugin/registry/`](../internal/plugin/registry/) (the built-in
manifests and their run functions), [`internal/actions/`](../internal/actions/)
(the action ids of plugin commands), [`plugin_pane.go`](../plugin_pane.go) and
[`plugin_view.go`](../plugin_view.go) (the pane and the renderer),
[`plugin_marquee.go`](../plugin_marquee.go), [`plugin_tint.go`](../plugin_tint.go),
[`pane_chooser.go`](../pane_chooser.go), [`actions.go`](../actions.go),
[`internal/cli/cli.go`](../internal/cli/cli.go) and `main.go` (`plugin-run`).

**Two built-in plugins ship with the app.** They are ordinary plugins in every
way that matters to the host: their manifests are embedded in the binary, their
`run` is the app's own executable with `plugin-run <id>`, and they speak to the
host only through `internal/plugin/sdk`. That keeps the same contract in front
of the app's own features and a plugin later installed from a folder.

The Spotify widget (`spotify`, widget `player`) plays in the app. Once the
saved sign-in has the `streaming` scope (`web.Client.CanStream`), the plugin
runs its engine (`…/spotify/engine/`): a loopback HTTP server on
`127.0.0.1:0` under a random path serves a page with Spotify's Web Playback
SDK, the page's access token, an event endpoint the page posts its state to,
and a server-sent event stream of commands. The host runs that page in a web
view with no window, through the plugin's `web` permission (`web.open`,
`web.close`, loopback `http` only, at most two per plugin, all closed with
the process; `plugin_web.go` and `internal/plugin/host`). The engine is the
plugin's `transport.Player`. It starts lazily: `engine_lazy.go` starts it on
the first play and closes it after 10 minutes with nothing playing
(`engineIdleTimeout`) or when the last Spotify pane closes, which saves about
130 MB while idle. `engine_sync.go` ends it at a sign-out or shutdown, and
`newEngine` sits in `backend.go` beside `newBackend`, so the end-to-end test's
overlay replaces both. Without a sign-in the pane draws a sign-in gate, never
the desktop app's states, and the palette's commands do nothing. Errors read
as plain text (Premium required, sign in again, or Spotify's own message) in a
notice under a player that stays, and play/pause flips at once before the
engine confirms. Shuffle and repeat hold their optimistic value until Spotify
confirms it, and the library refreshes when it is older than 60 seconds.

The engine runs the commands of the host one at a time, because parallel calls
break the SDK's playback (`enqueue` and `drain` in `engine/page.html`). A
waiting `seek` or `volume` gives way to a newer one only when it is the last
command in line. A lazy play passes its caller's context through `wake`: it
waits for a cold web view only until that context ends, and it checks the
context once more before admitting the command. A play that reaches the
deadline shows `Spotify is taking too long to start. Try again.`, not the
generic play failure. The engine's `send` refuses a
command whose context is already over, while a command accepted before a later
cancellation still runs, as the click it answers. `playEntry` and `playTrack` give each accepted play request a number in
`playReq`, separate from its guess number, and only that request may clear
its row marker, revert its guess, set a notice, clear a newer notice or read
after completion. The URI remains duplicate-click tracking only, so A, B,
then A again still lets the newest A own the result. A completion that a
newer press has superseded changes nothing; a current failure still reports
and reverts normally. A play carries that request number through the
engine's command queue, SSE payload, page retry and page error (the page's
`command` and `request` fields). `playbackErrorFor` checks it again under
`p.mu` at the callback, so a delayed error from an older request changes
nothing. A current play error reports its own cause and reverts only its own
guess; a non-play command error reports without taking back a newer play.
`beginPlay` makes the play's timeout context and keeps its cancel
(`playCancel`); a newer press cancels a request still on its way to the
engine, while a command the engine already queued keeps running.
`wake` checks cancellation after taking its gate and once more under the
lazy engine's lock, so a canceled play builds no web view.

The engine's double-click window keeps one accepted command. When an
identical play arrives inside it with a different request, `sendRequest`
returns `engine.CoalescedError` with the accepted request, and
`coalescePlay` makes that request the owner instead of leaving a newer click
owning a command that was never sent. If an accepted command fails while a
duplicate click is still waiting for that answer, `playPending` keeps the
failure for the click. The click has its own request identity, separate from
`playReq`, which coalescing can move to the older accepted request; a newer
press replaces the pending click, its finish or its binding clears it, and
only that owner can clear it. `coalescePlay` looks up the accepted request in
the click's failures and applies it after rechecking its source, so the sole
accepted command's failure is not lost, a retired engine's error is not
replayed, and an older completion or cache cannot suppress a later one. A
page failure clears the engine's
`lastPlay` window, so an immediate retry of the same row is sent; the page
does the same, and its own check compares the request, so a later accepted
play is not mistaken for the earlier click. The lazy engine wraps each inner
engine's errors with a `source` token. A callback can pass the lazy engine's
first generation check before a teardown and apply after the replacement;
`playbackErrorFor` checks the token under `p.mu` before changing a notice,
guess, mode or request, so a retired source changes nothing. Play and toggle
retry on a 403 or 404. The page wakes the audio after a play, a stall
watchdog fixes a track whose position stands still, and the player reconnects
with backoff of 2, 5 and 15 seconds. Fetches to the engine's own server time
out after 8 seconds. The
host's `slider` commits on `Dragged()` (`plugin_view.go`), not `Pressed()`,
which drops when the pointer leaves the thin track. Plugin web views open with
`PageOptions{AllowAutoplay: true, RunWhenHidden: true}` (`plugin_web.go`).
`AllowAutoplay` sets WebKit's `mediaTypesRequiringUserActionForPlayback` to
none, so the audio starts without a click; `RunWhenHidden` keeps WebKit from
throttling or suspending the hidden page. Only plugin web views set them; the
Browser pane keeps WebKit's default. Neither is in upstream MyGo: `go.mod`
replaces `github.com/egoist/mygo` with `github.com/ddeyalla/mygo` (the fork's
`shibu` branch). When a Kero update bumps the `egoist/mygo` require, the fork
is rebased onto that commit and the replace version is updated (see
`AGENTS.md`).

The layouts. `layoutOf` and `wideFrom` (440) in `view.go` pick the layout from
the pane's size. The player is two rows when the pane is 440 points wide or
more (56 point art, title and artist, shuffle, previous, play, next, repeat;
then elapsed, seek, duration, mute and an 80 point volume) and three rows
narrower. The mute button uses `speaker.slash.fill` (`volume-off`) when muted.
The player shows only once music plays in this session and stays while
paused; with nothing playing the pane shows just the library, and there is no
Sign In flash on open. A pane leaves "Connecting" as soon as the engine is
ready and gives up after 10 seconds. It then shows the track that plays on
another device, or the last played track (scope `user-read-recently-played`).
The library is a `grid` list of at most 48 tiles with a **Show More** tile,
using 300 point covers; a pressed result shows as playing at once and goes
back on a failure or after 5 seconds with no matching state. Search keeps the
last 20 queries for 2 minutes, and the library preloads after sign-in as one
request shared by all panes.

Browse. In the large layout a playlist or album tile opens its track list
(`drill.go`, `web/tracks.go`): back, the name, **Play** and numbered rows, with
the playing row highlighted (`Item.Active`). A row press plays in context
(`PlayFrom`; the desktop transport does the same through AppleScript). The
drill-in is saved in the pane's target. A 403 shows "Spotify doesn't share
this playlist's tracks", and **Sign In Again** when the token lacks the new
scope `playlist-read-collaborative` (`web.ScopeCollaborative`), so an older
sign-in must sign in once more. The **Up next** tab reads `/me/player/queue`.
The medium and small layouts still play on a tile press.

Sign Out. The manifest's `sign-out` command and its setting of the new kind
`action` (`protocol.SettingAction`, whose key must be a declared command)
draw a **Sign Out** button in Settings › Plugins › Spotify (`settings_view.go`)
and a palette item. The button runs `Host.Act` (`internal/plugin/host`), which
sends the command to the plugin. The plugin forgets the sign-in, keeps the
Client ID and stops the engine.

A smooth pane. The SDK (`internal/plugin/sdk`) skips sending a tree identical
to the last one for the same pane. A slider or a text part may carry `Rate`
and `At`, so the host moves the seek bar and the elapsed clock itself
(`sdk.Clock`, `Slider.Playing`), with no one-second tick in the plugin. A drag
renders only when it ends. The picture cache is keyed by URL, and the host
downscales a decoded picture to at most 512 pixels in `pluginFetch`. SF
Symbols are drawn through AppKit as templates and tinted at draw time
(`plugin_symbol.go`, `plugin_symbol_darwin.go`), so no fallback shows first.
The app's own views draw control and status SF Symbols through
`symbols_darwin.go`. Its white templates are keyed by name, size and
appearance, with tint applied when painted, just as plugin symbols are.
The medium and large layouts may
return `ScrollingTree()` (`Tree.Scroll`, `internal/plugin/protocol/tree.go`).

Ambient colour. A plugin with the `ambient` permission lends its pane tint.
`plugin_pane.go` calls `lendAmbient` after a render (`ambient.go`), and the
app mixes the lent colour, capped to 3:1 contrast, into the accent, the focus
edge, the terminals' cursor and selection, and a soft wash of the card
(`theme.go`, `ambientPal`). The setting `ambient-color`
(`internal/settings/config.go`, on by default) is the toggle in Settings ›
Appearance › **Colour from music**. The Spotify pane lends its tint only while
a track plays, so the colour fades on pause. The tint itself comes from
`plugin_tint.go`, darkened into a band where white text keeps 4.5 to 1
(`pluginTintClamp`).

Protocol additions are optional (`omitempty`): `Node.Active`, `Item.Active`,
`Item.Leading`, `Rate` and `At` on sliders and text, the icons `volume-off`,
`shuffle`, `repeat` and `volume`, the permissions `web` and `ambient`, and the
SDK's `(*sdk.Node).Label` and `.Active`. A horizontal slider with a `Width`
keeps it; one without grows.

The desktop transport below remains in the code. Its library calls (search, playlists, albums) go through the Web API
client with PKCE sign-in, a loopback redirect at
`http://127.0.0.1:43017/callback` (`web.DefaultRedirectPort`; the widget keeps
the client's default, the Set Up state names `RedirectURI()`, and the
manifest's Client ID help names the same URI, which a test checks), the Client
ID set in Settings › Plugins and the tokens kept in the plugin's
`secrets.json`. The plugin process calls `transport.RunLoopUntil(stop)` on its
main goroutine after starting `sdk.Run`, because the distributed-notification
listener needs the main run loop. When the SDK loop ends (the host closed
stdin, or the app crashed), the plugin closes the monitor and its ticker and
closes `stop`, so the run loop returns within a turn and the process exits
instead of polling osascript as an orphan; a test closes the stdin of the real
`plugin-run spotify` binary and waits for it to end. Playback states (not
running, not installed, the Automation refusal, playing, idle) and library
states (no Client ID, signed out, token refused, Premium lapsed, offline, rate
limited, ready) are typed, not strings, so each draws its own empty state. The
library state before a call comes from the client's `Current()`, so a refused
refresh keeps **Sign In Again** until the next sign-in, and a tab switch asks
the client again, so a tab whose load failed loads on the next pick. Offline
and rate-limited calls keep the last results with a note above them, and a
search shows kept results only under the query they are for
(`SearchResult.Query`). **Privacy › Automation** opens
`x-apple.systempreferences:com.apple.preference.security?Privacy_Automation`,
the one non-web link the host's `openURL` check allows, and the app opens
plugin links as URLs (`mygo.Shell.OpenExternal`), not as file paths. A failed
command's notice shows in every layout and clears after the next command that
works. The compact bar sends the snapshot's fraction at its read time with
`rate` and `at`, so the host counts the time since the read once. The search
field gets the saved query through the `searchField` part's `text`, so it is
filled again after a restart. The large layout keeps the
Search, Playlists, Albums and Up next tabs under the player. The title uses `marquee` and the album art
uses `tint`.

The Usage widget (`usage`, widget `usage`) reads `internal/usage` directly and
has no network code and no permissions. It no longer shows the machine's CPU
and RAM. A Cost card shows a Today, Yesterday and 30 Days `segmented` control,
a full total and a vertical list of each provider's name and amount; "≈" and
a line "Some amounts are estimates" mark estimated prices, and the line's tooltip lists those models.
Each provider has a section with its mark, name and plan. Each window (Session,
Weekly, Monthly) is a `progress` bar with a `marker` for the pace, "% left"
and "Resets in …"; when the pace would reach the limit before the reset, a red
`flame` and "Limit in …" show. A chevron opens Today and Last 30 Days as
dollars and tokens. Small panes get a compact form. The range and the open
providers are saved in the pane's target. A percent whose refresh failed is
muted and says it is the last known one. It starts
its read loop when a pane becomes visible and cancels it when none is, so a
hidden widget does no work. A pane is hidden whenever a frame does not draw it,
so the loop keeps the time of the last account read: a pane shown again within
two minutes waits out the rest of them, and only the refresh button reads at
once. The account read runs on a goroutine of its own, one at a time, so a
slow provider does not hold up the CPU and RAM bars, and each show starts the
CPU sampler afresh, so its first reading does not average over the hidden
span.

Cost comes from `internal/usage/cost`, which reads the logs of Claude Code
(`~/.claude/projects`, or `CLAUDE_CONFIG_DIR`), Codex (`~/.codex/sessions`, or
`CODEX_HOME`) and OpenCode (`opencode.db` through the system `sqlite3`, read
only, with OpenCode's own recorded cost). `price.go` holds the embedded price
table; Claude prices from Anthropic's list and the other prices are estimates.
Reads are incremental, and a compact binary cache at
`<KERO_HOME>/plugin-data/usage/cost-cache.bin` is saved at most every 30
minutes and on close. The plugin reads only while a pane is visible: every 5
minutes, at midnight and on refresh. Nothing reads or stores message text.
The generic parts that the card needs (`segmented`, `surface`,
`marker`, `icon` and `mark` on text, the `strong` role, more colour roles and
icons) live in `internal/plugin/protocol/tree.go`, `internal/plugin/sdk/parts.go`,
[`plugin_view.go`](../plugin_view.go) and [`plugin_symbol.go`](../plugin_symbol.go).

Code: [`internal/plugin/builtin/spotify/`](../internal/plugin/builtin/spotify/)
(the plugin, its layouts, engine, browse and the end-to-end test), [`…/spotify/engine/`](../internal/plugin/builtin/spotify/engine/) (the Web Playback SDK page and its server), [`plugin_web.go`](../plugin_web.go), [`plugin_symbol.go`](../plugin_symbol.go), [`ambient.go`](../ambient.go) and `controls.go`,
[`…/spotify/transport/`](../internal/plugin/builtin/spotify/transport/) (AppleScript and
the distributed notification) and [`…/spotify/web/`](../internal/plugin/builtin/spotify/web/)
(PKCE sign-in and the Web API client),
[`internal/plugin/builtin/usage/`](../internal/plugin/builtin/usage/) (the Usage
plugin and its source seam), [`internal/usage/cost/`](../internal/usage/cost/)
(the cost scan, prices and cache), and
[`internal/plugin/registry/manifests/`](../internal/plugin/registry/manifests/)
(the two manifests).

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

## Floating panels on Liquid Glass

The panels that float over the window for a moment are of Liquid Glass,
the material of macOS 26 and later: the command palette and the
confirmation dialog ([`palette.go`](../palette.go)), the New Pane chooser
([`pane_chooser.go`](../pane_chooser.go)), Settings
([`settings_view.go`](../settings_view.go)) and the chip a pane's title
becomes while it is dragged ([`panes.go`](../panes.go)). This is the
second step of BRU-212. Apple keeps the glass for the layer of controls
and navigation that floats over content (Human Interface Guidelines,
Materials); the panes, rows and cards are content and stay opaque.

[`glass_ui.go`](../glass_ui.go) holds the helpers, so that each panel
swaps one call:

- `glassPanel` fills a panel with the regular glass of MyGo's
  `plugins/glass` (`glass.Glass{}`), the variant Apple uses for alerts,
  popovers and sheets, which frosts what is under it and lightens it, or
  darkens it in the dark, so that text on it reads over a terminal full of
  colored text. In the dark, `panelGlass` tints it 60% toward the panel's
  own color: untinted, white terminal text under it brought the muted text
  to 3.8 to 1; tinted, it keeps 4.5 to 1. The glass draws its own lit
  hairline rim, so the panel has
  no border; `glassShadow` adds the soft shadow that the window AppKit
  shows a popover or a sheet in casts, as the glass casts none.
- `glassChip` is the same for the drag chip, with interactive glass, which
  swells a little while it is held.
- `glassScrim` is what a panel's backdrop lays over the window: nothing
  over glass, as macOS dims no window under a sheet, an alert or
  Spotlight, and the glass needs the panes under it as they are. The
  confirmation dialog (`confirmView` in `palette.go`) dims the window
  anyway: it asks before something that cannot be undone.
- `concentric` gives a panel the radius of its content's plus the inset:
  Settings is 22, its page's 14 plus the 8 around it, and the chooser's
  rows are 8, its 14 less its padding of 6.
- `glassShows` is the one switch for Reduce Transparency: it returns
  `!c.Preferences().ReduceTransparency`, which MyGo reads again as the
  setting changes. Under it every panel falls back to the opaque
  `floating` look over the dimmed window, and Settings to its wash
  (`TestPaletteOpaqueUnderReduceTransparency`).

Settings keeps its page as an opaque card; the sidebar beside it is now
the sheet's glass rather than the window's wash. The image peek stays a
borderless flyout: `FlyoutOptions.Popover` would put it in an NSPopover,
which takes the keyboard while it shows and animates in and out as it
moves, while the peek follows the pointer with Command held over a
terminal that must keep the keyboard. The Account usage panel
([`usage.go`](../usage.go)) is left to its own redesign.

`TestGlassPanelsOverTerminal` opens each panel over two terminals full of
text in the light and the dark, and writes shots of them to
`$KERO_GLASS_SHOTS`, a confirmation over Settings among them;
`TestPaletteIsGlass` checks that the palette shows the terminal through it
where an opaque panel would be one shade, and `TestGlassTextContrast` that
the palette's muted text keeps 4.5 to 1 on the glass, light and dark.

## Attention integration on current Shibu

The B1–B4 fixes share the existing adaptive tick scheduler: transcript scans
run off the tick goroutine and the atomic scan guard prevents overlapping
passes. `scanTranscript` owns the pane lock; `scanPass` does not take it
again. The branch watcher and elapsed-time scan cadence remain intact.
Screen overrides and the 30-second re-ask loop rule are intentional owner
choices (BRU-261 and BRU-269).

Linear links use the built-in Linear plugin, rather than the removed Linear
screen. Its `session.attention session=<id>` action passes through `focusAttentionOf`, and
notification clicks through `focusAttention`, so the pane that needs the user
stays visible under zoom. An Attention row also saves a project fold it opens.

Attention search, filters and scroll survive restart through the pane
Target. Missing Codex transcripts are discovered in bounded, continuing
slices. The transcript dedupe retains what it told after a screen override
to avoid reclaiming the same working state on every scan.

## Hidden terminal redraws (BRU-331)

`App.view` clears terminal entries' `shown` markers before composing panes.
`paneTerminal` marks entries actually drawn; the completed frame applies that
visibility through MyGo's `Terminal.SetVisible`. New attachments start hidden,
so an attachment that finishes after its session was left cannot redraw it.
The host still streams output and preserves titles, bells and notifications.
Revealing a terminal requests one fresh frame; unchanged visibility requests
none. Settings leaves the terminals composed beneath its overlay visible.

The implementation lives in `app.go`, `view.go`, `panes.go` and the MyGo
terminal plugin. `terminal_visibility_test.go` covers session switches, zoom,
late attachment, built-in views and Settings. MyGo's visibility tests cover
hidden output and callbacks, reveal, timer quiescence and concurrent updates.
No daemon protocol, saved state or pane API changes.

In the tester, four hidden terminals producing 20 output cycles caused 20
window builds before the change and none afterward. An isolated native run
with five visited sessions recorded 3,732 builds per minute while four hidden
terminals streamed before the change, and 10 afterward. The latter sample
lost focus partway through, so it is supporting evidence rather than a
focused idle-budget comparison. Visible output still produced frames and
revealing a terminal showed its current text. The installed app was not tested.

## Visited-terminal memory diagnostics (BRU-333)

`PERF_MEMORY=1 PERF_KEEP=1 sh scripts/perf.sh N` uses `scripts/perf_memory.py`
and a temporary build overlay to capture an empty-app baseline, two complete
sweeps of every terminal, all-fixture output and settled memory. `PERF_LAYOUT=split`
keeps N panes in one selected session; real pane actions visit each one zoomed
before expanding the layout. A visit needs a fresh pane paint and the fixture's
initial-feed `PERF-READY` marker in its bounded text tail. Normal app builds gain no diagnostic
listener or hook. `scripts/test_perf_memory.py` covers routing isolation,
accounting, paint readiness, traversal bounds and overlay seams.

The 25-session and selected 25-pane synthetic workloads met the 750 MiB
aggregate target, retaining eight and 25 terminals respectively. The largest
primary stage sample was 431.7 MiB in the split workload; its settled live Go
heap was 38.3 MiB. Baselines and native snapshots varied, so these results do
not establish a memory reduction or full allocator ownership. The historical
1.22 GB daily-app observation and the ordinary small-pane budget remain open.
No production memory policy changed. See [Performance](perf.md) for the full
matrix, profiling effects and remaining limits.

## Controls and toolbars on Liquid Glass (BRU-212)

The sidebar and pane layout stay as they are. This pass changes the navigation
and control layer: Browser and Files toolbar groups use compact glass capsules,
fields and segmented controls have concentric rounded ends, and a held slider
or switch has a real glass lens. Primary action buttons keep their opaque
accent fill so their text contrast does not depend on content underneath.

Sidebar collapse/add controls, the footer's folder/search/Settings buttons and
pane-header add/split/zoom/close controls keep their previous plain style:
individual compact buttons with a subtle rounded hover fill, without a glass
capsule or group padding. This follows the owner's native-app hand-test feedback.

`glass_ui.go` owns `glassToolbar`, `toolbarGroup` and `scrollUnderBar`. Sidebar,
Files and Attention lists, and plugin lists with a sticky header, use
pass-through soft edges only where more content continues beyond the viewport.
The plugin seam gives the Linear/Tasks toolbar the same treatment. Settings uses its existing overlaid header,
with a soft edge. Browser navigation and its address field are grouped above
the native web view; the MyGo compositor does not refract the web page.

`controls.go` and `elevation.go` retain the base controls' keyboard, pointer and
accessibility behavior while styling their materials. `glassShows` and MyGo's
glass materials follow Reduce Transparency and Increase Contrast. Opaque
fallbacks stay fully opaque even when a hover tint has partial alpha. Motion
continues to follow the system's Reduce Motion preference.

`icons.go` maps existing embedded control handles to native SF Symbols through
`controlIcon` and `symbols_darwin.go`; provider logos remain artwork. Glyph
handles are cached by embedded asset name under a mutex, so Settings rebuilds
cannot grow the native-symbol registry every frame. Non-macOS window controls
keep their existing platform rendering.

Native symbol bitmaps cache the white shape, not each tint. Hover, theme
and ambient-color transitions reuse that shape and apply color with the
painter's image tint. The regression advances 32 control tints and verifies
that they retain at most one new bitmap instead of one per color.

Offscreen symbol drawing pins its goroutine to one OS thread while AppKit's
per-thread graphics stack and autorelease pool are live. Graphics state is
restored before releasing the bitmap, draining the pool and unlocking the
thread. This fixes a sporadic native restore trap exposed by the full UI suite;
the uncached worker regression repeats native calls under scheduler and GC
pressure, rather than exercising only warmed bitmap-cache entries.

`controls_glass_test.go`, `toolbar_glass_test.go` and `symbols_test.go` cover
control interaction, opaque fallbacks, scroll-edge hit testing, symbol tint,
native symbol availability and bounded concurrent glyph reuse. MyGo tester
shots cover light/dark appearances and reduced transparency. A floating inset
sidebar, window-radius redesign and glass morphing container are outside this
pass, following the owner's current-layout decision.
